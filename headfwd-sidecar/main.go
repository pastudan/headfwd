package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
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
	PublicKey        string `json:"publicKey"`        // Curve25519 key for fingerprint
	Ed25519PublicKey string `json:"ed25519PublicKey"` // Ed25519 key for signature verification
}

type RegistrationInitResponse struct {
	Fingerprint string `json:"fingerprint"`
	Nonce       string `json:"nonce"`
	ExpiresAt   int64  `json:"expiresAt"`
}

type RegistrationVerifyRequest struct {
	Fingerprint string `json:"fingerprint"`
	Signature   string `json:"signature"`
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

// registerWithProxy performs challenge-response registration with the proxy
func registerWithProxy(proxyURL, headscaleURL, noiseKeyPath string) (string, error) {
	// 1. Get Headscale's Noise public key (Curve25519)
	resp, err := http.Get(headscaleURL + "/key?v=96")
	if err != nil {
		return "", fmt.Errorf("failed to get Headscale key: %w", err)
	}
	defer resp.Body.Close()

	var keyData HeadscaleKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&keyData); err != nil {
		return "", fmt.Errorf("failed to decode key: %w", err)
	}

	log.Printf("Headscale Curve25519 public key: %s", keyData.PublicKey)

	// 2. Convert Curve25519 private key to Ed25519 and get Ed25519 public key
	ed25519PublicKey, err := getEd25519PublicKey(noiseKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to get Ed25519 public key: %w", err)
	}
	
	log.Printf("Ed25519 public key for signing: %x", ed25519PublicKey)

	// 3. Initialize registration with Curve25519 key (for fingerprint) and Ed25519 key (for verification)
	// We send both: Curve25519 for fingerprint computation, Ed25519 for signature verification
	initReq := RegistrationInitRequest{
		PublicKey: keyData.PublicKey, // Curve25519 key for fingerprint
		Ed25519PublicKey: hex.EncodeToString(ed25519PublicKey), // Ed25519 key for verification
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

	// 3. Sign the nonce with Noise private key
	signature, err := signWithNoiseKey(noiseKeyPath, initResp.Nonce)
	if err != nil {
		return "", fmt.Errorf("failed to sign nonce: %w", err)
	}

	// 4. Verify registration
	verifyReq := RegistrationVerifyRequest{
		Fingerprint: initResp.Fingerprint,
		Signature:   signature,
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

// signWithNoiseKey signs data using the Noise private key (Curve25519)
// 
// Headscale's Noise protocol uses Curve25519 keys for ECDH key exchange.
// We convert the Curve25519 private key to Ed25519 for signing.
//
// Conversion: SHA-512 hash of the 32-byte Curve25519 seed, use first 32 bytes as Ed25519 seed.
// This is a standard transformation used by many cryptographic libraries.
func signWithNoiseKey(keyPath, nonce string) (string, error) {
	// Auto-detect key path if not specified
	if keyPath == "" {
		// Try common locations
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
			return "", fmt.Errorf("could not find Noise private key. Specify with --noise-key")
		}
	}

	// Read Curve25519 private key from Headscale format
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return "", fmt.Errorf("failed to read Noise private key from %s: %w", keyPath, err)
	}

	// Parse Headscale's key format: "privkey:hex_encoded_32_bytes"
	keyStr := string(keyData)
	if len(keyStr) < 8 || keyStr[:8] != "privkey:" {
		return "", fmt.Errorf("invalid Noise key format: expected 'privkey:' prefix")
	}
	
	keyHex := keyStr[8:] // Remove "privkey:" prefix
	
	// Remove trailing whitespace (newlines, etc.)
	for len(keyHex) > 0 && (keyHex[len(keyHex)-1] == '\n' || keyHex[len(keyHex)-1] == '\r' || keyHex[len(keyHex)-1] == ' ') {
		keyHex = keyHex[:len(keyHex)-1]
	}
	
	// Decode hex to bytes (should be 32 bytes for Curve25519)
	curve25519Key, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", fmt.Errorf("failed to decode Noise key hex: %w", err)
	}
	
	if len(curve25519Key) != 32 {
		return "", fmt.Errorf("invalid Curve25519 key: expected 32 bytes, got %d", len(curve25519Key))
	}

	// Convert Curve25519 private key to Ed25519 private key
	// Method: Hash the Curve25519 key with SHA-512, use first 32 bytes as Ed25519 seed
	hash := sha512.Sum512(curve25519Key)
	ed25519Seed := hash[:32]
	
	// Generate Ed25519 keypair from the seed
	ed25519PrivateKey := ed25519.NewKeyFromSeed(ed25519Seed)

	// Sign the nonce
	nonceBytes := []byte(nonce)
	signature := ed25519.Sign(ed25519PrivateKey, nonceBytes)

	return hex.EncodeToString(signature), nil
}

// getEd25519PublicKey converts Curve25519 private key to Ed25519 public key
func getEd25519PublicKey(keyPath string) ([]byte, error) {
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
			return nil, fmt.Errorf("could not find Noise private key")
		}
	}

	// Read and parse Curve25519 private key
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Noise private key: %w", err)
	}

	keyStr := string(keyData)
	if len(keyStr) < 8 || keyStr[:8] != "privkey:" {
		return nil, fmt.Errorf("invalid Noise key format")
	}
	
	keyHex := keyStr[8:]
	for len(keyHex) > 0 && (keyHex[len(keyHex)-1] == '\n' || keyHex[len(keyHex)-1] == '\r' || keyHex[len(keyHex)-1] == ' ') {
		keyHex = keyHex[:len(keyHex)-1]
	}
	
	curve25519Key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key: %w", err)
	}
	
	if len(curve25519Key) != 32 {
		return nil, fmt.Errorf("invalid key length: %d", len(curve25519Key))
	}

	// Convert to Ed25519
	hash := sha512.Sum512(curve25519Key)
	ed25519Seed := hash[:32]
	ed25519PrivateKey := ed25519.NewKeyFromSeed(ed25519Seed)
	ed25519PublicKey := ed25519PrivateKey.Public().(ed25519.PublicKey)

	return ed25519PublicKey, nil
}

