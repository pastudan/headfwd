package main

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Configuration
var (
	tunnelURL      = flag.String("tunnel", "", "Tunnel WebSocket URL (wss://abc123.headfwd.net/tunnel?auth=secret)")
	headscaleURL   = flag.String("headscale", "http://localhost:8080", "Local Headscale URL")
	proxyURL       = flag.String("proxy", "http://localhost:8787", "Proxy URL for registration")
	noiseKeyPath   = flag.String("noise-key", "", "Path to Noise private key (default: auto-detect from Headscale)")
	autoRegister   = flag.Bool("register", false, "Auto-register with proxy using challenge-response")
	reconnectDelay = flag.Duration("reconnect", 5*time.Second, "Reconnect delay")
)

// TunnelMessage matches proxy protocol
type TunnelMessage struct {
	Type string          `json:"type"` // "request", "response", "ping", "pong"
	Data json.RawMessage `json:"data"`
}

type TunnelRequest struct {
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    *string           `json:"body,omitempty"`
}

type TunnelResponse struct {
	ID      string            `json:"id"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    *string           `json:"body,omitempty"`
}

// Registration types
type HeadscaleKeyResponse struct {
	PublicKey string `json:"publicKey"`
}

type RegistrationInitRequest struct {
	PublicKey string `json:"publicKey"` // X25519 Noise public key
}

type RegistrationInitResponse struct {
	Fingerprint    string `json:"fingerprint"`
	Nonce          string `json:"nonce"`
	ProxyPublicKey string `json:"proxyPublicKey"` // Proxy's ephemeral X25519 public key
	ExpiresAt      int64  `json:"expiresAt"`
}

type RegistrationVerifyRequest struct {
	Fingerprint string `json:"fingerprint"`
	Proof       string `json:"proof"` // HMAC-SHA256(sharedSecret, nonce)
}

type RegistrationVerifyResponse struct {
	Fingerprint string `json:"fingerprint"`
	TunnelURL   string `json:"tunnelUrl"`
	PublicURL   string `json:"publicUrl"`
}

func main() {
	flag.Parse()

	log.Printf("HeadFwd Sidecar starting...")
	log.Printf("Headscale: %s", *headscaleURL)

	// Auto-register if requested
	if *autoRegister {
		log.Printf("Auto-registering with proxy...")
		tunnel, err := registerWithProxy(*proxyURL, *headscaleURL, *noiseKeyPath)
		if err != nil {
			log.Fatalf("Registration failed: %v", err)
		}
		tunnelURL = &tunnel
		log.Printf("✓ Registration successful")
	}

	if *tunnelURL == "" {
		log.Fatal("--tunnel is required (or use --register for auto-registration)")
	}

	log.Printf("Tunnel: %s", *tunnelURL)

	for {
		if err := runTunnel(); err != nil {
			log.Printf("Tunnel error: %v", err)
			log.Printf("Reconnecting in %v...", *reconnectDelay)
			time.Sleep(*reconnectDelay)
		}
	}
}

func runTunnel() error {
	// Connect to proxy
	log.Printf("Connecting to tunnel...")
	conn, _, err := websocket.DefaultDialer.Dial(*tunnelURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	log.Printf("✓ Tunnel connected")

	// Keepalive
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			msg := TunnelMessage{Type: "ping"}
			if err := conn.WriteJSON(msg); err != nil {
				return
			}
		}
	}()

	// Handle messages
	for {
		var msg TunnelMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return fmt.Errorf("read: %w", err)
		}

		switch msg.Type {
		case "request":
			go handleRequest(conn, msg.Data)
		case "pong":
			// Keepalive response
		default:
			log.Printf("Unknown message type: %s", msg.Type)
		}
	}
}

func handleRequest(conn *websocket.Conn, data json.RawMessage) {
	var req TunnelRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Printf("Error unmarshaling request: %v", err)
		return
	}

	log.Printf("%s %s", req.Method, req.URL)

	// Forward to local Headscale
	resp, err := forwardToHeadscale(req)
	if err != nil {
		log.Printf("Error forwarding: %v", err)
		// Send error response
		resp = &TunnelResponse{
			ID:     req.ID,
			Status: 502,
			Headers: map[string]string{
				"Content-Type": "text/plain",
			},
			Body: stringPtr("Bad Gateway"),
		}
	}

	// Send response back through tunnel
	msg := TunnelMessage{
		Type: "response",
		Data: mustMarshal(resp),
	}

	if err := conn.WriteJSON(msg); err != nil {
		log.Printf("Error writing response: %v", err)
	}
}

func forwardToHeadscale(req TunnelRequest) (*TunnelResponse, error) {
	// Build HTTP request to local Headscale
	// Extract path from full URL
	reqURL := req.URL
	if len(reqURL) > 0 && reqURL[0] != '/' {
		// Parse full URL to get path
		u, err := url.Parse(reqURL)
		if err == nil {
			reqURL = u.Path
			if u.RawQuery != "" {
				reqURL += "?" + u.RawQuery
			}
		}
	}
	
	var body io.Reader
	if req.Body != nil {
		body = stringReader(*req.Body)
	}

	httpReq, err := http.NewRequest(req.Method, *headscaleURL+reqURL, body)
	if err != nil {
		return nil, err
	}

	// Copy headers
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	// Execute request
	client := &http.Client{Timeout: 30 * time.Second}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	// Build response
	headers := make(map[string]string)
	for k, v := range httpResp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	return &TunnelResponse{
		ID:      req.ID,
		Status:  httpResp.StatusCode,
		Headers: headers,
		Body:    stringPtr(string(respBody)),
	}, nil
}

func stringPtr(s string) *string { return &s }
func stringReader(s string) io.Reader {
	return bytes.NewBufferString(s)
}
func mustMarshal(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// registerWithProxy performs X25519 ECDH + HMAC-SHA256 challenge-response registration
func registerWithProxy(proxyURL, headscaleURL, noiseKeyPath string) (string, error) {
	// 1. Load X25519 private key from Headscale
	x25519Key, err := loadNoisePrivateKey(noiseKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to load noise key: %w", err)
	}

	// 2. Get Headscale's Noise public key (X25519)
	resp, err := http.Get(headscaleURL + "/key?v=96")
	if err != nil {
		return "", fmt.Errorf("failed to get Headscale key: %w", err)
	}
	defer resp.Body.Close()

	var keyData HeadscaleKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&keyData); err != nil {
		return "", fmt.Errorf("failed to decode key: %w", err)
	}

	log.Printf("Headscale X25519 public key: %s", keyData.PublicKey)

	// 3. Phase 1: Request challenge
	initReq := RegistrationInitRequest{
		PublicKey: keyData.PublicKey,
	}
	initBody, _ := json.Marshal(initReq)

	resp, err = http.Post(proxyURL+"/api/register/init", "application/json", bytes.NewReader(initBody))
	if err != nil {
		return "", fmt.Errorf("failed to init registration: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("registration init failed: %s", string(body))
	}

	var initResp RegistrationInitResponse
	if err := json.NewDecoder(resp.Body).Decode(&initResp); err != nil {
		return "", fmt.Errorf("failed to decode init response: %w", err)
	}

	log.Printf("Challenge received: fingerprint=%s", initResp.Fingerprint)

	// 4. Decode proxy's ephemeral X25519 public key
	proxyPublicKeyBytes, err := hex.DecodeString(initResp.ProxyPublicKey)
	if err != nil {
		return "", fmt.Errorf("failed to decode proxy public key: %w", err)
	}

	// 5. Compute shared secret via X25519 ECDH
	curve := ecdh.X25519()

	sidecarPrivateKey, err := curve.NewPrivateKey(x25519Key)
	if err != nil {
		return "", fmt.Errorf("failed to create X25519 private key: %w", err)
	}

	proxyPublicKey, err := curve.NewPublicKey(proxyPublicKeyBytes)
	if err != nil {
		return "", fmt.Errorf("failed to create proxy public key: %w", err)
	}

	sharedSecret, err := sidecarPrivateKey.ECDH(proxyPublicKey)
	if err != nil {
		return "", fmt.Errorf("ECDH failed: %w", err)
	}

	// 6. Compute HMAC-SHA256 proof
	h := hmac.New(sha256.New, sharedSecret)
	h.Write([]byte(initResp.Nonce))
	proof := hex.EncodeToString(h.Sum(nil))

	log.Printf("Computed HMAC proof")

	// 7. Phase 2: Submit proof for verification
	verifyReq := RegistrationVerifyRequest{
		Fingerprint: initResp.Fingerprint,
		Proof:       proof,
	}
	verifyBody, _ := json.Marshal(verifyReq)

	resp, err = http.Post(proxyURL+"/api/register/verify", "application/json", bytes.NewReader(verifyBody))
	if err != nil {
		return "", fmt.Errorf("failed to verify registration: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("registration verification failed: %s", string(body))
	}

	var verifyResp RegistrationVerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&verifyResp); err != nil {
		return "", fmt.Errorf("failed to decode verify response: %w", err)
	}

	log.Printf("✓ Registered at: %s", verifyResp.PublicURL)
	return verifyResp.TunnelURL, nil
}

// loadNoisePrivateKey reads Headscale's X25519 Noise private key from disk
func loadNoisePrivateKey(keyPath string) ([]byte, error) {
	// Auto-detect key path if not specified
	if keyPath == "" {
		paths := []string{
			"/var/lib/headscale/noise_private.key",
			"/keys/noise_private.key",
			"./headscale/data/noise_private.key",
		}
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				keyPath = p
				break
			}
		}
		if keyPath == "" {
			return nil, fmt.Errorf("could not find Noise private key. Specify with --noise-key")
		}
	}

	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Noise private key from %s: %w", keyPath, err)
	}

	// Parse Headscale's key format: "privkey:hex_encoded_32_bytes"
	keyStr := strings.TrimSpace(string(keyData))
	if !strings.HasPrefix(keyStr, "privkey:") {
		return nil, fmt.Errorf("invalid Noise key format: expected 'privkey:' prefix")
	}

	keyHex := strings.TrimPrefix(keyStr, "privkey:")
	privateKey, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode Noise key hex: %w", err)
	}

	if len(privateKey) != 32 {
		return nil, fmt.Errorf("invalid X25519 key: expected 32 bytes, got %d", len(privateKey))
	}

	return privateKey, nil
}

