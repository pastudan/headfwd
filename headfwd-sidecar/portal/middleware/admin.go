package middleware

import (
	"context"
	"log"
	"net/http"

	"github.com/headfwd/sidecar/portal/admin"
	"github.com/headfwd/sidecar/portal/headscale"
	"github.com/headfwd/sidecar/portal/identity"
)

// AdminOnly returns an HTTP middleware that enforces admin-only access.
//
// Identity comes only from the TCP peer address (see package identity);
// forwarding headers are ignored.
//
//   - On the LAN listener, loopback/RFC 1918 callers are local admins.
//   - On the tailnet listener, the caller's headscale user must be in the admin
//     store. Private/loopback addresses get no special treatment there.
//   - If store is nil every request is allowed (fallback for unconfigured installs).
//
// The resolved username (or "local") is stored in the request context under
// admin.ContextKeyUserName so downstream handlers can read it without an extra
// headscale round-trip.
func AdminOnly(store *admin.Store, hs *headscale.Client, ln identity.Listener) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if store == nil {
				next.ServeHTTP(w, r)
				return
			}
			caller := ln.Classify(r)
			if caller.Local {
				ctx := context.WithValue(r.Context(), admin.ContextKeyUserName, "local")
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			var userName string
			if caller.Tailnet {
				userName = resolveUser(caller, hs)
			}
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

func resolveUser(caller identity.Caller, hs *headscale.Client) string {
	if hs == nil {
		return ""
	}
	nodes, err := hs.ListNodes()
	if err != nil {
		log.Printf("[admin middleware] list nodes: %v", err)
		return ""
	}
	if node := identity.FindNode(nodes, caller.IP); node != nil {
		return node.User.Name
	}
	return ""
}
