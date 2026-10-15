package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/headfwd/sidecar/portal/headscale"
	"github.com/headfwd/sidecar/portal/identity"
)

type HelloResponse struct {
	Message   string `json:"message"`
	YourIP    string `json:"your_ip"`
	IsTailnet bool   `json:"is_tailnet"`
	Timestamp string `json:"timestamp"`
	NodeName  string `json:"node_name,omitempty"`
	UserName  string `json:"user_name,omitempty"`
}

type HelloHandlers struct {
	HS       *headscale.Client
	Listener identity.Listener
}

func (h *HelloHandlers) Hello(w http.ResponseWriter, r *http.Request) {
	caller := h.Listener.Classify(r)

	resp := HelloResponse{
		Message:   "Hello from HeadFwd!",
		YourIP:    caller.IP.String(),
		IsTailnet: caller.Tailnet,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// If on the tailnet, try to resolve the node/user from the IP
	if caller.Tailnet && h.HS != nil {
		if nodes, err := h.HS.ListNodes(); err == nil {
			if node := identity.FindNode(nodes, caller.IP); node != nil {
				resp.NodeName = node.GivenName
				resp.UserName = node.User.Name
			}
		}
	}

	deviceInfo := ""
	if resp.NodeName != "" {
		deviceInfo = " (" + resp.NodeName + ")"
	}
	log.Printf("[hello] %s%s tailnet=%v", resp.YourIP, deviceInfo, caller.Tailnet)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
