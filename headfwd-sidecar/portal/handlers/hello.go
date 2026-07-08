package handlers

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/headfwd/sidecar/portal/headscale"
)

type HelloResponse struct {
	Message   string  `json:"message"`
	YourIP    string  `json:"your_ip"`
	IsTailnet bool    `json:"is_tailnet"`
	Timestamp string  `json:"timestamp"`
	NodeName  string  `json:"node_name,omitempty"`
	UserName  string  `json:"user_name,omitempty"`
}

type HelloHandlers struct {
	HS *headscale.Client
}

func (h *HelloHandlers) Hello(w http.ResponseWriter, r *http.Request) {
	remoteIP := extractIP(r)
	isTailnet := strings.HasPrefix(remoteIP, "100.") || strings.HasPrefix(remoteIP, "fd7a:")

	resp := HelloResponse{
		Message:   "Hello from HeadFwd!",
		YourIP:    remoteIP,
		IsTailnet: isTailnet,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// If on the tailnet, try to resolve the node/user from the IP
	if isTailnet && h.HS != nil {
		if nodes, err := h.HS.ListNodes(); err == nil {
			for _, node := range nodes {
				for _, ip := range node.IPAddresses {
					if ip == remoteIP {
						resp.NodeName = node.GivenName
						resp.UserName = node.User.Name
						break
					}
				}
				if resp.NodeName != "" {
					break
				}
			}
		}
	}

	deviceInfo := ""
	if resp.NodeName != "" {
		deviceInfo = " (" + resp.NodeName + ")"
	}
	log.Printf("[hello] %s%s tailnet=%v", remoteIP, deviceInfo, isTailnet)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
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

// isLocalIP reports whether ip is a loopback or RFC-1918 address.
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
