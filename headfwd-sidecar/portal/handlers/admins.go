package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/headfwd/sidecar/portal/admin"
	"github.com/headfwd/sidecar/portal/headscale"
)

// AdminHandlers handles the /api/admins endpoints.
type AdminHandlers struct {
	Store *admin.Store
	HS    *headscale.Client
}

// List handles GET /api/admins — returns the list of admin usernames.
func (h *AdminHandlers) List(w http.ResponseWriter, r *http.Request) {
	admins := []string{}
	if h.Store != nil {
		admins = h.Store.List()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string][]string{"admins": admins})
}

// Grant handles PUT /api/admins/{name} — promotes name to admin.
func (h *AdminHandlers) Grant(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		jsonErr(w, "admin store not configured", http.StatusInternalServerError)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		jsonErr(w, "missing name", http.StatusBadRequest)
		return
	}
	if err := h.Store.Add(name); err != nil {
		jsonErr(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Revoke handles DELETE /api/admins/{name} — revokes admin from name.
// Blocks self-revoke and last-admin revoke.
func (h *AdminHandlers) Revoke(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		jsonErr(w, "admin store not configured", http.StatusInternalServerError)
		return
	}
	name := r.PathValue("name")
	if name == "" {
		jsonErr(w, "missing name", http.StatusBadRequest)
		return
	}
	callerName, _ := r.Context().Value(admin.ContextKeyUserName).(string)
	if callerName != "" && callerName != "local" && callerName == name {
		jsonErr(w, "cannot remove your own admin access", http.StatusBadRequest)
		return
	}
	if err := h.Store.Remove(name); err != nil {
		jsonErr(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
