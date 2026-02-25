package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/headfwd/sidecar/portal/headscale"
)

type KeyHandlers struct {
	HS                *headscale.Client
	PublicURL         string     // public headscale URL used in QR payload (e.g. https://<fingerprint>.headfwd.net)
	TailnetServerFunc func() string // returns current tailnet portal URL; called at request time so it updates once tsnet connects
}

type CreateKeyResponse struct {
	Key       string `json:"key"`
	User      string `json:"user"`
	QRData    string `json:"qr_data"`
	ExpiresAt string `json:"expires_at"`
}

func (h *KeyHandlers) Create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		User      string `json:"user"`
		Reusable  bool   `json:"reusable"`
		Ephemeral bool   `json:"ephemeral"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.User == "" {
		http.Error(w, `{"error":"user is required"}`, http.StatusBadRequest)
		return
	}

	user, err := h.HS.GetUserByName(req.User)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"user lookup failed: %s"}`, err), http.StatusBadRequest)
		return
	}
	userID, err := user.NumericID()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid user ID: %s"}`, err), http.StatusInternalServerError)
		return
	}

	expiration := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)

	key, err := h.HS.CreatePreauthKey(headscale.CreateKeyRequest{
		User:       userID,
		Reusable:   req.Reusable,
		Ephemeral:  req.Ephemeral,
		Expiration: expiration,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	// JSON payload: starts with '{' so the stock iOS camera app does not interpret
	// it as a tappable URL and accidentally GET the preauth key via the fly.io proxy.
	// The embedding app reads this directly via DataScannerViewController and uses
	// the key exclusively over the Noise channel to headscale.
	//
	// tailnet_server: the portal's tailnet (100.64.x.x) address. When present, the
	// iOS app calls /api/hello via its tsnet SOCKS5 session so the server sees the
	// device's real tailnet IP instead of a public internet address.
	type qrPayload struct {
		Server        string `json:"server"`
		Key           string `json:"key"`
		TailnetServer string `json:"tailnet_server,omitempty"`
		Version       int    `json:"v"`
	}
	tailnetServer := ""
	if h.TailnetServerFunc != nil {
		tailnetServer = h.TailnetServerFunc()
	}
	qrBytes, _ := json.Marshal(qrPayload{
		Server:        h.PublicURL,
		Key:           key.Key,
		TailnetServer: tailnetServer,
		Version:       1,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(CreateKeyResponse{
		Key:       key.Key,
		User:      req.User,
		QRData:    string(qrBytes),
		ExpiresAt: expiration,
	})
}
