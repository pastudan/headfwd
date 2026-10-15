package portal

import (
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"

	"github.com/headfwd/sidecar/portal/admin"
	"github.com/headfwd/sidecar/portal/handlers"
	"github.com/headfwd/sidecar/portal/headscale"
	"github.com/headfwd/sidecar/portal/identity"
	portalmiddleware "github.com/headfwd/sidecar/portal/middleware"
)

type Config struct {
	Port         string
	HeadscaleURL string
	APIKey       string
	// DevMode skips the embedded frontend when true (Vite dev server serves the UI on its own port).
	DevMode bool
	// ViteProxyURL, when non-empty together with DevMode, signals that this server
	// instance is the tsnet listener running in dev mode. A dev-mode message page
	// is served at "/" instead of the embedded frontend.
	// Typical value: "http://localhost:5173" (used as a presence flag only).
	ViteProxyURL string
	// UpstreamURL, when set, reverse-proxies all non-/api/* requests to this
	// backend URL and injects verified headscale identity headers.
	// Examples: "http://localhost:5173" (Vite dev), "http://immich:2283" (prod).
	// When set, the embedded frontend is not served.
	UpstreamURL string
	// PublicURL is the public headscale URL (e.g. https://<fingerprint>.headfwd.net); used in QR codes.
	PublicURL string
	// TailnetServerFunc returns the current tailnet portal URL (e.g. http://100.64.0.1:3001).
	// It is called at key-generation time so the value can be populated asynchronously
	// after tsnet self-registers. Returns "" until the node has joined the tailnet.
	TailnetServerFunc func() string
	// StateDir is the directory used to persist admin state (admins.json).
	// When empty, admin enforcement is skipped (all requests are allowed).
	StateDir string
}

// NewServer builds the portal handler for one listener. ln decides how a
// caller's peer address is interpreted (see package identity): only the LAN
// listener grants loopback/RFC 1918 callers local admin, and only the tailnet
// listener maps callers to headscale users.
func NewServer(cfg Config, ln identity.Listener) http.Handler {
	hsClient := headscale.NewClient(cfg.HeadscaleURL, cfg.APIKey)

	var adminStore *admin.Store
	if cfg.StateDir != "" {
		s, err := admin.NewStore(cfg.StateDir)
		if err != nil {
			log.Printf("Warning: admin store init failed: %v", err)
		} else {
			adminStore = s
			go func() {
				if err := s.Bootstrap(hsClient); err != nil {
					log.Printf("[admin] bootstrap: %v", err)
				}
			}()
		}
	}

	adminMW := portalmiddleware.AdminOnly(adminStore, hsClient, ln)

	helloHandlers := &handlers.HelloHandlers{HS: hsClient, Listener: ln}
	userHandlers := &handlers.UserHandlers{HS: hsClient}
	keyHandlers := &handlers.KeyHandlers{HS: hsClient, PublicURL: cfg.PublicURL, TailnetServerFunc: cfg.TailnetServerFunc}
	nodeHandlers := &handlers.NodeHandlers{HS: hsClient}
	meHandlers := &handlers.MeHandlers{HS: hsClient, Store: adminStore, Listener: ln}
	adminHandlers := &handlers.AdminHandlers{Store: adminStore, HS: hsClient}

	mux := http.NewServeMux()

	// Unprotected — any peer can identify themselves.
	mux.HandleFunc("GET /api/hello", helloHandlers.Hello)
	mux.HandleFunc("GET /api/me", meHandlers.Me)

	// Admin-only management routes.
	mux.Handle("GET /api/users", adminMW(http.HandlerFunc(userHandlers.List)))
	mux.Handle("POST /api/users", adminMW(http.HandlerFunc(userHandlers.Create)))
	mux.Handle("DELETE /api/users/{name}", adminMW(http.HandlerFunc(userHandlers.Delete)))
	mux.Handle("POST /api/keys", adminMW(http.HandlerFunc(keyHandlers.Create)))
	mux.Handle("GET /api/nodes", adminMW(http.HandlerFunc(nodeHandlers.List)))
	mux.Handle("DELETE /api/nodes/{id}", adminMW(http.HandlerFunc(nodeHandlers.Delete)))

	// Admin CRUD.
	mux.Handle("GET /api/admins", adminMW(http.HandlerFunc(adminHandlers.List)))
	mux.Handle("PUT /api/admins/{name}", adminMW(http.HandlerFunc(adminHandlers.Grant)))
	mux.Handle("DELETE /api/admins/{name}", adminMW(http.HandlerFunc(adminHandlers.Revoke)))

	if cfg.UpstreamURL != "" {
		upstream, err := handlers.NewUpstreamHandler(cfg.UpstreamURL, hsClient, ln)
		if err != nil {
			log.Printf("Warning: upstream proxy setup failed (%s): %v", cfg.UpstreamURL, err)
		} else {
			log.Printf("Upstream proxy → %s", cfg.UpstreamURL)
			mux.Handle("/", upstream)
		}
	} else if cfg.DevMode && cfg.ViteProxyURL != "" {
		// tsnet listener in dev mode: WKWebView can't load Vite's module scripts
		// through the custom tsnet:// scheme. Serve a clear message instead so
		// the iOS WebView shows something useful rather than a blank page.
		// Run `make build-frontend` once, then restart without DEV=1 to use the
		// portal dashboard on iOS.
		serveDevMessage(mux)
	} else if !cfg.DevMode {
		serveFrontend(mux)
	}

	// No CORS headers: the portal UI is same-origin (embedded, or reached via the
	// Vite dev proxy / iOS 127.0.0.1 proxy), so browsers need none. The API's
	// authentication is the caller's address, so letting other origins read
	// responses would hand them admin.
	return apiGuard(mux, allowedHostsFromEnv())
}

// ListenAndServe starts the portal HTTP server on its configured LAN port. Blocks until error.
// Loopback/RFC 1918 callers on this listener are treated as local admins.
func ListenAndServe(cfg Config) error {
	addr := ":" + cfg.Port
	log.Printf("Portal starting on %s (headscale: %s)", addr, cfg.HeadscaleURL)
	return http.ListenAndServe(addr, NewServer(cfg, identity.LAN))
}

// ServeOn starts serving the portal on an already-accepted tsnet listener.
// Callers are identified only by their tailnet node; nobody is a local admin.
// It does not block; errors are logged. Call this before ListenAndServe.
func ServeOn(cfg Config, ln net.Listener) {
	handler := NewServer(cfg, identity.Tailnet)
	srv := &http.Server{Handler: handler}
	go func() {
		log.Printf("Portal also serving on tailnet listener %s", ln.Addr())
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("Portal tailnet listener error: %v", err)
		}
	}()
}

// serveDevMessage registers a "/" fallback that returns a plain HTML page
// explaining that the portal dashboard requires a production frontend build.
// WKWebView cannot load Vite's ES-module scripts through the custom tsnet://
// scheme, so proxying to the Vite dev server produces a blank page.
// Fix: run `make build-frontend` in headfwd-sidecar/, then restart without DEV=1.
func serveDevMessage(mux *http.ServeMux) {
	const page = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Portal — dev mode</title>
<style>
  body{font-family:system-ui,sans-serif;max-width:480px;margin:80px auto;padding:0 24px;color:#1a1a1a}
  h2{font-size:1.1rem;font-weight:600;margin-bottom:.5rem}
  p{color:#555;font-size:.9rem;line-height:1.5;margin:.5rem 0}
  code{background:#f4f4f4;border:1px solid #e0e0e0;border-radius:4px;padding:2px 6px;font-size:.85rem}
</style>
</head>
<body>
  <h2>Portal not available in dev mode</h2>
  <p>The portal dashboard requires a compiled frontend to run inside the iOS WebView.</p>
  <p>In <strong>headfwd-sidecar/</strong>, run:</p>
  <p><code>make build-frontend</code></p>
  <p>Then restart the sidecar without <code>DEV=1</code>. The browser portal at
     <code>localhost:5173</code> continues to work with hot-reload as usual.</p>
</body>
</html>`
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(page))
	})
}

func serveFrontend(mux *http.ServeMux) {
	dist, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		log.Printf("warning: no embedded frontend found (run frontend build first): %v", err)
		return
	}

	fileServer := http.FileServer(http.FS(dist))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			fileServer.ServeHTTP(w, r)
			return
		}

		f, err := dist.Open(strings.TrimPrefix(path, "/"))
		if err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

// apiGuard protects /api/* from browser-borne attacks. The portal authenticates
// by network address, so any web page a LAN or tailnet user visits could
// otherwise act with that user's privileges:
//
//   - Cross-site requests: http.CrossOriginProtection rejects non-GET/HEAD
//     requests a browser marks as coming from another origin (Sec-Fetch-Site,
//     falling back to Origin vs Host). Non-browser clients are unaffected.
//   - DNS rebinding: an attacker's domain re-pointed at the portal's IP makes
//     the browser treat requests as same-origin, defeating the check above. So
//     the Host header must be a name an attacker cannot re-point: an IP
//     literal, localhost, a single-label name, a .local mDNS name, or a host
//     listed in PORTAL_ALLOWED_HOSTS.
//
// Non-/api paths (the embedded UI or the UPSTREAM_URL app) pass through; the
// upstream app is responsible for its own CSRF protection.
func apiGuard(next http.Handler, allowedHosts map[string]bool) http.Handler {
	cop := http.NewCrossOriginProtection()
	protected := cop.Handler(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api" && !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if !hostAllowed(r.Host, allowedHosts) {
			log.Printf("[portal] rejected %s %s: Host %q not allowed (set PORTAL_ALLOWED_HOSTS to permit it)", r.Method, r.URL.Path, r.Host)
			http.Error(w, `{"error":"host not allowed"}`, http.StatusMisdirectedRequest)
			return
		}
		protected.ServeHTTP(w, r)
	})
}

// hostAllowed reports whether a Host header value is safe from DNS rebinding.
func hostAllowed(hostport string, extra map[string]bool) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(strings.Trim(host, "[]"), "."))
	if host == "" {
		return false
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return true
	}
	if host == "localhost" || !strings.Contains(host, ".") {
		return true // single-label LAN name; not resolvable via public DNS
	}
	for _, suffix := range privateDNSSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return extra[host]
}

// privateDNSSuffixes are names only a local resolver answers for, so an
// attacker cannot point them at the portal from the public internet.
var privateDNSSuffixes = []string{
	".localhost", ".local", ".lan", ".home", ".home.arpa", ".internal", ".localdomain",
}

// allowedHostsFromEnv parses PORTAL_ALLOWED_HOSTS: comma-separated hostnames
// (no port) that may be used to reach the portal API, e.g. a MagicDNS name.
func allowedHostsFromEnv() map[string]bool {
	m := map[string]bool{}
	for _, h := range strings.Split(os.Getenv("PORTAL_ALLOWED_HOSTS"), ",") {
		if h = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), ".")); h != "" {
			m[h] = true
		}
	}
	return m
}
