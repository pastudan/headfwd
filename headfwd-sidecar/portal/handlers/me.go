package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/headfwd/sidecar/portal/admin"
	"github.com/headfwd/sidecar/portal/headscale"
)

// MeResponse is the response body for GET /api/me.
type MeResponse struct {
	UserName      string `json:"user_name"`
	IsAdmin       bool   `json:"is_admin"`
	IsLocalAccess bool   `json:"is_local_access"`
}

// MeHandlers handles the /api/me endpoint.
type MeHandlers struct {
	HS    *headscale.Client
	Store *admin.Store
}

// Me returns the caller's identity and admin status.
// This endpoint is unprotected — any tailnet peer (or local browser) can call it.
func (h *MeHandlers) Me(w http.ResponseWriter, r *http.Request) {
	ip := extractIP(r)
	isLocal := isLocalIP(ip)
	isTailnet := strings.HasPrefix(ip, "100.") || strings.HasPrefix(ip, "fd7a:")

	var userName string
	if isTailnet && h.HS != nil {
		if nodes, err := h.HS.ListNodes(); err == nil {
			outer:
			for _, node := range nodes {
				for _, nodeIP := range node.IPAddresses {
					if nodeIP == ip {
						userName = node.User.Name
						break outer
					}
				}
			}
		}
	}

	isAdmin := isLocal
	if !isLocal && h.Store != nil && userName != "" {
		isAdmin = h.Store.IsAdmin(userName)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(MeResponse{
		UserName:      userName,
		IsAdmin:       isAdmin,
		IsLocalAccess: isLocal,
	})
}
