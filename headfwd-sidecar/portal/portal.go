package portal

import (
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/headfwd/sidecar/portal/handlers"
	"github.com/headfwd/sidecar/portal/headscale"
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
	// PublicURL is the public headscale URL (e.g. https://<fingerprint>.headfwd.net); used in QR codes.
	PublicURL string
	// TailnetServerFunc returns the current tailnet portal URL (e.g. http://100.64.0.1:3001).
	// It is called at key-generation time so the value can be populated asynchronously
	// after tsnet self-registers. Returns "" until the node has joined the tailnet.
	TailnetServerFunc func() string
}

func NewServer(cfg Config) http.Handler {
	hsClient := headscale.NewClient(cfg.HeadscaleURL, cfg.APIKey)

	helloHandlers := &handlers.HelloHandlers{HS: hsClient}
	userHandlers := &handlers.UserHandlers{HS: hsClient}
	keyHandlers := &handlers.KeyHandlers{HS: hsClient, PublicURL: cfg.PublicURL, TailnetServerFunc: cfg.TailnetServerFunc}
	nodeHandlers := &handlers.NodeHandlers{HS: hsClient}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/hello", helloHandlers.Hello)
	mux.HandleFunc("GET /api/users", userHandlers.List)
	mux.HandleFunc("POST /api/users", userHandlers.Create)
	mux.HandleFunc("DELETE /api/users/{name}", userHandlers.Delete)
	mux.HandleFunc("POST /api/keys", keyHandlers.Create)
	mux.HandleFunc("GET /api/nodes", nodeHandlers.List)
	mux.HandleFunc("DELETE /api/nodes/{id}", nodeHandlers.Delete)

	if cfg.DevMode && cfg.ViteProxyURL != "" {
		// tsnet listener in dev mode: WKWebView can't load Vite's module scripts
		// through the custom tsnet:// scheme. Serve a clear message instead so
		// the iOS WebView shows something useful rather than a blank page.
		// Run `make build-frontend` once, then restart without DEV=1 to use the
		// portal dashboard on iOS.
		serveDevMessage(mux)
	} else if !cfg.DevMode {
		serveFrontend(mux)
	}

	return corsMiddleware(mux)
}

// ListenAndServe starts the portal HTTP server on its configured LAN port. Blocks until error.
func ListenAndServe(cfg Config) error {
	addr := ":" + cfg.Port
	log.Printf("Portal starting on %s (headscale: %s)", addr, cfg.HeadscaleURL)
	return http.ListenAndServe(addr, NewServer(cfg))
}

// ServeOn starts serving the portal on an already-accepted listener (e.g. a tsnet listener).
// It does not block; errors are logged. Call this before ListenAndServe.
func ServeOn(cfg Config, ln net.Listener) {
	handler := NewServer(cfg)
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
		w.WriteHeader(http.StatusServiceUnavailable)
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

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
