package middleware

import (
	"context"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/headfwd/sidecar/portal/admin"
	"github.com/headfwd/sidecar/portal/headscale"
)

// AdminOnly returns an HTTP middleware that enforces admin-only access.
//
//   - Requests from local/RFC-1918 IPs are always allowed (local admin).
//   - Tailnet requests are allowed only when the resolved headscale username is
//     in the admin store.
//   - If store is nil every request is allowed (fallback for unconfigured installs).
//
// The resolved username (or "local") is stored in the request context under
// admin.ContextKeyUserName so downstream handlers can read it without an extra
// headscale round-trip.
func AdminOnly(store *admin.Store, hs *headscale.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if store == nil {
				next.ServeHTTP(w, r)
				return
			}
			ip := extractIP(r)
			if isLocalIP(ip) {
				ctx := context.WithValue(r.Context(), admin.ContextKeyUserName, "local")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			userName := resolveUser(ip, hs)
			if userName == "" || !store.IsAdmin(userName) {
				w.Header().Set("Content-Type", "application/json")
				http.Error(w,
					`{"error":"forbidden","hint":"ask an admin to grant you access"}`,
					http.StatusForbidden,
				)
				return
			}
			ctx := context.WithValue(r.Context(), admin.ContextKeyUserName, userName)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func resolveUser(ip string, hs *headscale.Client) string {
	if hs == nil {
		return ""
	}
	nodes, err := hs.ListNodes()
	if err != nil {
		log.Printf("[admin middleware] list nodes: %v", err)
		return ""
	}
	for _, node := range nodes {
		for _, nodeIP := range node.IPAddresses {
			if nodeIP == ip {
				return node.User.Name
			}
		}
	}
	return ""
}

func isLocalIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed != nil && parsed.IsLoopback() {
		return true
	}
	switch {
	case strings.HasPrefix(ip, "10."):
		return true
	case strings.HasPrefix(ip, "192.168."):
		return true
	case strings.HasPrefix(ip, "172."):
		parts := strings.SplitN(ip, ".", 4)
		if len(parts) >= 2 {
			n := 0
			for _, c := range parts[1] {
				if c < '0' || c > '9' {
					return false
				}
				n = n*10 + int(c-'0')
			}
			return n >= 16 && n <= 31
		}
	}
	return false
}

func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
