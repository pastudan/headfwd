package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/headfwd/sidecar/portal/headscale"
)

type NodeHandlers struct {
	HS *headscale.Client
}

func (h *NodeHandlers) List(w http.ResponseWriter, r *http.Request) {
	nodes, err := h.HS.ListNodes()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"nodes": nodes})
}

func (h *NodeHandlers) Delete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, `{"error":"id is required"}`, http.StatusBadRequest)
		return
	}
	if err := h.HS.DeleteNode(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
