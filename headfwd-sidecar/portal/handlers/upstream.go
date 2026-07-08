package handlers

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/headfwd/sidecar/portal/headscale"
)

// Identity headers injected by the proxy. These are stripped from incoming
// client requests first so they cannot be spoofed.
var identityHeaders = []string{
	"X-Tailscale-User",
	"X-Tailscale-Login",
	"X-Tailscale-Device",
	"X-Tailscale-IP",
}

// UpstreamHandler reverse-proxies all requests to a configured backend URL,
// enriching each request with verified headscale identity headers derived
// from the client's tailnet IP address.
//
// Header semantics:
//
//	X-Tailscale-User   — headscale username (e.g. "dan")
//	X-Tailscale-Login  — same value; alias for apps expecting this name
//	X-Tailscale-Device — unique MagicDNS device name (e.g. "headfwd-ios-mrsyme5m")
//	X-Tailscale-IP     — client tailnet IP (e.g. "100.64.0.10")
//
// Non-tailnet clients (local dev, internet) receive no identity headers.
type UpstreamHandler struct {
	proxy *httputil.ReverseProxy
	cache nodeCache
}

// NewUpstreamHandler creates a reverse proxy that forwards to targetURL.
func NewUpstreamHandler(targetURL string, hs *headscale.Client) (*UpstreamHandler, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	h := &UpstreamHandler{}

	rp := httputil.NewSingleHostReverseProxy(target)

	// Wrap the default director to inject verified identity headers.
	base := rp.Director
	rp.Director = func(req *http.Request) {
		base(req)

		// Prevent clients from spoofing identity headers.
		for _, hdr := range identityHeaders {
			req.Header.Del(hdr)
		}

		remoteIP := extractIP(req)
		if !strings.HasPrefix(remoteIP, "100.") && !strings.HasPrefix(remoteIP, "fd7a:") {
			return // non-tailnet client — no identity headers
		}

		nodes, err := h.cache.get(hs)
		if err != nil {
			return // best-effort; log noise suppressed intentionally
		}
		for _, node := range nodes {
			for _, ip := range node.IPAddresses {
				if ip == remoteIP {
					req.Header.Set("X-Tailscale-User", node.User.Name)
					req.Header.Set("X-Tailscale-Login", node.User.Name)
					req.Header.Set("X-Tailscale-Device", node.GivenName)
					req.Header.Set("X-Tailscale-IP", remoteIP)
					return
				}
			}
		}
	}

	rp.ErrorLog = log.New(log.Writer(), "[upstream] ", log.LstdFlags)
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("[upstream] %s %s → %v", r.Method, r.URL.Path, err)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}

	h.proxy = rp
	return h, nil
}

func (h *UpstreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.proxy.ServeHTTP(w, r)
}

// nodeCache holds a short-lived snapshot of headscale nodes so that
// high-throughput backends (Immich, Frigate) do not generate a headscale
// API call on every proxied request.
type nodeCache struct {
	mu      sync.Mutex
	nodes   []headscale.Node
	expires time.Time
}

const nodeCacheTTL = 30 * time.Second

func (c *nodeCache) get(hs *headscale.Client) ([]headscale.Node, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.expires) {
		return c.nodes, nil
	}
	nodes, err := hs.ListNodes()
	if err != nil {
		if len(c.nodes) > 0 {
			// Return stale data on transient headscale errors rather than
			// dropping identity headers for all in-flight requests.
			return c.nodes, nil
		}
		return nil, err
	}
	c.nodes = nodes
	c.expires = time.Now().Add(nodeCacheTTL)
	return nodes, nil
}
