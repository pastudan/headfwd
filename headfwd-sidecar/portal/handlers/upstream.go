package handlers

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/headfwd/sidecar/portal/headscale"
	"github.com/headfwd/sidecar/portal/identity"
)

// Identity headers injected by the proxy. These are stripped from incoming
// client requests first so they cannot be spoofed.
var identityHeaders = []string{
	"X-Tailscale-User",
	"X-Tailscale-Login",
	"X-Tailscale-Device",
	"X-Tailscale-IP",
}

// Client-supplied address headers that must never reach the upstream app.
// (httputil.ReverseProxy with Rewrite already drops X-Forwarded-For/Host/Proto
// from the outbound request; these are the remaining common variants.)
var clientAddressHeaders = []string{
	"X-Real-Ip",
	"Forwarded",
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
//	X-Forwarded-For    — the TCP peer address only (any client value is replaced)
//
// Identity headers are only ever set for requests that arrived on the tailnet
// listener from a tailnet address. Requests on the LAN listener, and anything
// else, reach the upstream with no identity headers.
type UpstreamHandler struct {
	proxy *httputil.ReverseProxy
	cache nodeCache
}

// NewUpstreamHandler creates a reverse proxy that forwards to targetURL.
// ln is the listener this handler serves; it decides whether a caller's peer
// address may be mapped to a tailnet identity.
func NewUpstreamHandler(targetURL string, hs *headscale.Client, ln identity.Listener) (*UpstreamHandler, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	h := &UpstreamHandler{}

	h.proxy = &httputil.ReverseProxy{
		// Rewrite (unlike Director) strips hop-by-hop headers before this
		// runs and removes the client's X-Forwarded-* headers from Out.
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = pr.In.Host // keep NewSingleHostReverseProxy's Host behaviour
			pr.SetXForwarded()       // X-Forwarded-For = TCP peer address, nothing else

			// Prevent clients from spoofing identity or address headers.
			for _, hdr := range identityHeaders {
				pr.Out.Header.Del(hdr)
			}
			for _, hdr := range clientAddressHeaders {
				pr.Out.Header.Del(hdr)
			}

			caller := ln.Classify(pr.In)
			if !caller.Tailnet || hs == nil {
				return // not a tailnet peer on the tailnet listener — no identity
			}
			nodes, err := h.cache.get(hs)
			if err != nil {
				return // best-effort; log noise suppressed intentionally
			}
			if node := identity.FindNode(nodes, caller.IP); node != nil {
				pr.Out.Header.Set("X-Tailscale-User", node.User.Name)
				pr.Out.Header.Set("X-Tailscale-Login", node.User.Name)
				pr.Out.Header.Set("X-Tailscale-Device", node.GivenName)
				pr.Out.Header.Set("X-Tailscale-IP", caller.IP.String())
			}
		},
		ErrorLog: log.New(log.Writer(), "[upstream] ", log.LstdFlags),
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("[upstream] %s %s → %v", r.Method, r.URL.Path, err)
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
		},
	}

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
