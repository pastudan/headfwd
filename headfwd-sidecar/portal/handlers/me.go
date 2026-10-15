package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/headfwd/sidecar/portal/admin"
	"github.com/headfwd/sidecar/portal/headscale"
	"github.com/headfwd/sidecar/portal/identity"
)

// MeResponse is the response body for GET /api/me.
type MeResponse struct {
	UserName      string `json:"user_name"`
	IsAdmin       bool   `json:"is_admin"`
	IsLocalAccess bool   `json:"is_local_access"`
}

// MeHandlers handles the /api/me endpoint.
type MeHandlers struct {
	HS       *headscale.Client
	Store    *admin.Store
	Listener identity.Listener
}

// Me returns the caller's identity and admin status.
// This endpoint is unprotected — any tailnet peer (or local browser) can call it.
// The answer must agree with middleware.AdminOnly, so it uses the same rules.
func (h *MeHandlers) Me(w http.ResponseWriter, r *http.Request) {
	caller := h.Listener.Classify(r)

	var userName string
	if caller.Tailnet && h.HS != nil {
		if nodes, err := h.HS.ListNodes(); err == nil {
			if node := identity.FindNode(nodes, caller.IP); node != nil {
				userName = node.User.Name
			}
		}
	}

	isAdmin := caller.Local
	if !caller.Local && h.Store != nil && userName != "" {
		isAdmin = h.Store.IsAdmin(userName)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MeResponse{
		UserName:      userName,
		IsAdmin:       isAdmin,
		IsLocalAccess: caller.Local,
	})
}
