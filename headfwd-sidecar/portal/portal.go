package portal

import (
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	// ViteProxyURL, when set, registers a reverse-proxy fallback on "/" so that static
	// frontend requests (e.g. from the iOS WebView via the tsnet listener) are forwarded
	// to the Vite dev server. Only used when DevMode is true.
	// Typical value: "http://localhost:5173"
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
		serveViteProxy(mux, cfg.ViteProxyURL)
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

// serveViteProxy registers a "/" fallback that reverse-proxies static requests
// to the Vite dev server. API routes registered before this handler take priority.
// Used for the tsnet listener in dev mode so the iOS WebView can load the live UI.
func serveViteProxy(mux *http.ServeMux, viteURL string) {
	target, err := url.Parse(viteURL)
	if err != nil {
		log.Printf("warning: invalid ViteProxyURL %q: %v", viteURL, err)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	log.Printf("Portal tsnet listener: proxying frontend to %s", viteURL)
	mux.HandleFunc("/", proxy.ServeHTTP)
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
