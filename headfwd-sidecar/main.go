package main

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/headfwd/sidecar/portal"
	"github.com/headfwd/sidecar/portal/headscale"
	"tailscale.com/tsnet"
)

// Configuration
var (
	tunnelURL          = flag.String("tunnel", "", "Tunnel WebSocket URL (wss://abc123.headfwd.net/tunnel?auth=secret)")
	headscaleURL       = flag.String("headscale", "http://headscale:8080", "Local Headscale URL")
	proxyURL           = flag.String("proxy", "https://headfwd.net", "Proxy URL for registration")
	noiseKeyPath       = flag.String("noise-key", "", "Path to Noise private key (default: auto-detect from Headscale)")
	autoRegister       = flag.Bool("register", true, "Auto-register with proxy using challenge-response")
	configPath         = flag.String("config", "/config/config.yaml", "Headscale config path for server_url updates")
	updateConfig       = flag.Bool("update-config", true, "Update headscale server_url when auto-registering")
	forceUpdate        = flag.Bool("force-update", false, "Force update headscale server_url even if already set")
	restartHeadscale   = flag.Bool("restart-headscale", false, "Restart headscale container after updating server_url")
	headscaleContainer = flag.String("headscale-container", "headscale", "Docker container name for headscale")
	reconnectDelay     = flag.Duration("reconnect", 5*time.Second, "Reconnect delay")
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
	IsBinary bool             `json:"isBinary,omitempty"`
}

type TunnelResponse struct {
	ID      string            `json:"id"`
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    *string           `json:"body,omitempty"`
	IsBinary bool             `json:"isBinary,omitempty"`
}

type WsOpen struct {
	ID      string            `json:"id"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    *string           `json:"body,omitempty"`
	IsBinary bool             `json:"isBinary,omitempty"`
}

type WsData struct {
	ID       string `json:"id"`
	Data     string `json:"data"`
	IsBinary bool   `json:"isBinary"`
}

type WsClose struct {
	ID     string `json:"id"`
	Code   int    `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
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

type RegistrationResult struct {
	Fingerprint string
	TunnelURL   string
	PublicURL   string
}

func main() {
	flag.Parse()

	// Allow env vars to override defaults when flags are not explicitly set.
	if *tunnelURL == "" {
		if v, ok := os.LookupEnv("TUNNEL_URL"); ok && v != "" {
			tunnelURL = &v
		}
	}
	if *proxyURL == "https://headfwd.net" {
		if v, ok := os.LookupEnv("PROXY_URL"); ok && v != "" {
			proxyURL = &v
		}
	}
	if *headscaleURL == "http://headscale:8080" {
		if v, ok := os.LookupEnv("HEADSCALE_URL"); ok && v != "" {
			headscaleURL = &v
		}
	}
	if *configPath == "/config/config.yaml" {
		if v, ok := os.LookupEnv("HEADSCALE_CONFIG_PATH"); ok && v != "" {
			configPath = &v
		}
	}
	if *updateConfig {
		if v, ok := os.LookupEnv("UPDATE_SERVER_URL"); ok && v != "" {
			if strings.ToLower(v) == "false" || v == "0" {
				updateConfig = func() *bool { b := false; return &b }()
			}
		}
	}
	if !*restartHeadscale {
		if v, ok := os.LookupEnv("RESTART_HEADSCALE"); ok && v != "" && strings.ToLower(v) != "false" && v != "0" {
			restartHeadscale = func() *bool { b := true; return &b }()
		}
	}
	if *headscaleContainer == "headscale" {
		if v, ok := os.LookupEnv("HEADSCALE_CONTAINER_NAME"); ok && v != "" {
			headscaleContainer = &v
		}
	}
	if !*forceUpdate {
		if v, ok := os.LookupEnv("FORCE_UPDATE_SERVER_URL"); ok && v != "" && strings.ToLower(v) != "false" && v != "0" {
			forceUpdate = func() *bool { b := true; return &b }()
		}
	}
	if !*autoRegister {
		if v, ok := os.LookupEnv("AUTO_REGISTER"); ok && v != "" && strings.ToLower(v) != "false" && v != "0" {
			autoRegister = func() *bool { b := true; return &b }()
		}
	}

	log.Printf("HeadFwd Sidecar starting...")
	log.Printf("Headscale: %s", *headscaleURL)

	devMode := os.Getenv("DEV") == "1"

	// Collect portal config; portal is started after registration so it has the public URL.
	var portalAPIKey string
	if portalEnabled() {
		portalAPIKey = os.Getenv("HEADSCALE_API_KEY")
		if portalAPIKey == "" {
			if key, err := bootstrapAPIKey(*headscaleContainer); err != nil {
				log.Printf("Portal: API key bootstrap failed: %v", err)
				log.Printf("Portal: user/key management will not work (set HEADSCALE_API_KEY manually)")
			} else {
				portalAPIKey = key
			}
		}
	}

	// Auto-register if requested (must happen before portal start to obtain public URL)
	var portalPublicURL string
	if *autoRegister {
		log.Printf("Auto-registering with proxy...")
		result, err := registerWithProxy(*proxyURL, *headscaleURL, *noiseKeyPath)
		if err != nil {
			if devMode {
				log.Printf("Registration failed (non-fatal in dev mode): %v", err)
			} else {
				log.Fatalf("Registration failed: %v", err)
			}
		} else {
			tunnelURL = &result.TunnelURL
			portalPublicURL = publicURLFromFingerprint(result.Fingerprint)
			log.Printf("✓ Registration successful")

			if *updateConfig {
				if updated, err := updateServerURL(*configPath, portalPublicURL, *forceUpdate); err != nil {
					log.Printf("Config update skipped: %v", err)
				} else if updated {
					log.Printf("✓ Updated headscale server_url in %s", *configPath)
					if *restartHeadscale {
						if err := restartHeadscaleContainer(*headscaleContainer); err != nil {
							log.Printf("Failed to restart headscale: %v", err)
							log.Printf("Please restart headscale for the change to take effect.")
						} else {
							log.Printf("✓ Restarted headscale container: %s", *headscaleContainer)
						}
					} else {
						log.Printf("Please restart headscale for the change to take effect.")
					}
				}
			}
		}
	}

	// Fall back to PUBLIC_URL env var (useful in dev or when not auto-registering)
	if portalPublicURL == "" {
		portalPublicURL = os.Getenv("PUBLIC_URL")
	}

	// tailnetServerAddr holds the sidecar's tailnet portal URL once tsnet connects.
	// It is set from a background goroutine and read at QR-code-generation time.
	var tailnetServerAddr atomic.Pointer[string]
	tailnetServerFunc := func() string {
		if v := tailnetServerAddr.Load(); v != nil {
			return *v
		}
		return ""
	}

	// Compute the state directory once — used by both portal instances and tsnet.
	portalStateDir := envOr("TSNET_DIR", defaultTsnetDir())

	upstreamURL := os.Getenv("UPSTREAM_URL")

	// Start portal web UI immediately so it is available while tsnet is connecting.
	if portalEnabled() {
		go func() {
			err := portal.ListenAndServe(portal.Config{
				Port:              envOr("PORTAL_PORT", "3001"),
				HeadscaleURL:      *headscaleURL,
				APIKey:            portalAPIKey,
				DevMode:           devMode,
				PublicURL:         portalPublicURL,
				TailnetServerFunc: tailnetServerFunc,
				StateDir:          portalStateDir,
				UpstreamURL:       upstreamURL,
			})
			if err != nil {
				log.Printf("Portal error: %v", err)
			}
		}()
	}

	// Join the headscale network as "headfwd-server" in the background.
	// Once connected, tailnetServerAddr is updated and new QR codes will include
	// the tailnet portal URL. The portal also starts listening on the tsnet interface.
	if portalEnabled() && portalAPIKey != "" {
		hsClient := headscale.NewClient(*headscaleURL, portalAPIKey)
		go func() {
			ip, tsLn, err := startTsnetNode(hsClient, *headscaleURL, portalStateDir)
			if err != nil {
				log.Printf("Warning: tsnet self-registration failed: %v", err)
				log.Printf("Warning: iOS /api/hello will not show tailnet IP until this is resolved")
				return
			}
		addr := "http://" + ip + ":3001"
		tailnetServerAddr.Store(&addr)
		log.Printf("✓ Tailnet portal ready: %s", addr)
			// Serve portal on the tsnet listener so peers reach us at our 100.64 IP.
			// In dev mode the embedded frontend isn't built, so proxy static requests
			// to the local Vite server instead so the iOS WebView shows the live UI.
			viteProxy := ""
			if devMode {
				viteProxy = "http://localhost:5173"
			}
			portal.ServeOn(portal.Config{
				HeadscaleURL:      *headscaleURL,
				APIKey:            portalAPIKey,
				DevMode:           devMode,
				ViteProxyURL:      viteProxy,
				PublicURL:         portalPublicURL,
				TailnetServerFunc: tailnetServerFunc,
				StateDir:          portalStateDir,
				UpstreamURL:       upstreamURL,
			}, tsLn)
		}()
	}

	if *tunnelURL == "" {
		if devMode {
			log.Printf("No tunnel URL -- running portal only (dev mode)")
			select {} // block forever, portal runs in goroutine
		}
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
			wsMu.Lock()
			err := conn.WriteJSON(msg)
			wsMu.Unlock()
			if err != nil {
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
		case "ws_open":
			go handleWsOpen(conn, msg.Data)
		case "ws_data":
			handleWsData(msg.Data)
		case "ws_close":
			handleWsClose(msg.Data)
		case "pong":
			// Keepalive response
		default:
			log.Printf("Unknown message type: %s", msg.Type)
		}
	}
}

func publicURLFromFingerprint(fingerprint string) string {
	if fingerprint == "" {
		return ""
	}
	return "https://" + fingerprint + ".headfwd.net"
}

func updateServerURL(path, publicURL string, force bool) (bool, error) {
	if publicURL == "" {
		return false, fmt.Errorf("missing public URL")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("failed to read config: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	found := false
	updated := false
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "server_url:") {
			found = true
			current := strings.TrimSpace(strings.TrimPrefix(trim, "server_url:"))
			if !force && !isDefaultServerURL(current) {
				if !shouldUpdateHeadfwdURL(current, publicURL) {
					return false, fmt.Errorf("server_url already set (%s)", current)
				}
			}
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[i] = fmt.Sprintf("%sserver_url: %s", indent, publicURL)
			updated = true
			break
		}
	}

	if !found {
		lines = append(lines, fmt.Sprintf("server_url: %s", publicURL))
		updated = true
	}

	if !updated {
		return false, nil
	}

	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		return false, fmt.Errorf("failed to write config: %w", err)
	}
	return true, nil
}

func isDefaultServerURL(value string) bool {
	switch strings.ToLower(value) {
	case "", "http://127.0.0.1:8080", "http://localhost:8080", "http://headscale:8080":
		return true
	default:
		return false
	}
}

func shouldUpdateHeadfwdURL(current, desired string) bool {
	if !strings.HasSuffix(strings.ToLower(current), ".headfwd.net") {
		return false
	}
	return strings.ToLower(current) != strings.ToLower(desired)
}

func computeFingerprint(pubkey string) string {
	sum := sha256.Sum256([]byte(pubkey))
	full := hex.EncodeToString(sum[:])
	if len(full) < 32 {
		return full
	}
	return full[:32]
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

	wsMu.Lock()
	err = conn.WriteJSON(msg)
	wsMu.Unlock()
	if err != nil {
		log.Printf("Error writing response: %v", err)
	}
}

type streamConn struct {
	conn net.Conn
	mu   sync.Mutex
}

func (s *streamConn) write(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.conn.Write(data)
	return err
}

var streamMu  = &sync.Mutex{}
var streamConns = map[string]*streamConn{}
// wsMu serializes all writes to the tunnel websocket connection.
// gorilla/websocket requires that only one writer is active at a time.
var wsMu = &sync.Mutex{}

func handleWsOpen(conn *websocket.Conn, data json.RawMessage) {
	var open WsOpen
	if err := json.Unmarshal(data, &open); err != nil {
		log.Printf("Error unmarshaling ws_open: %v", err)
		return
	}
	if open.Method == "" {
		open.Method = http.MethodGet
	}

	targetAddr, err := headscaleDialAddress()
	if err != nil {
		log.Printf("Error resolving headscale address: %v", err)
		return
	}

	rawConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		log.Printf("Error dialing headscale: %v", err)
		return
	}

	bodyBytes := []byte{}
	if open.Body != nil {
		if open.IsBinary {
			decoded, err := base64.StdEncoding.DecodeString(*open.Body)
			if err != nil {
				log.Printf("Error decoding stream body: %v", err)
				_ = rawConn.Close()
				return
			}
			bodyBytes = decoded
		} else {
			bodyBytes = []byte(*open.Body)
		}
	}

	rawRequest := buildRawRequest(open.Method, open.URL, open.Headers, bodyBytes, targetAddr)
	if _, err := rawConn.Write(rawRequest); err != nil {
		log.Printf("Error writing raw request: %v", err)
		_ = rawConn.Close()
		return
	}

	streamMu.Lock()
	streamConns[open.ID] = &streamConn{conn: rawConn}
	streamMu.Unlock()

	go func(id string, c net.Conn) {
		defer c.Close()
		buf := make([]byte, 32*1024)
		for {
			n, err := c.Read(buf)
			if err != nil {
				sendWsClose(conn, id, 1000, "closed")
				streamMu.Lock()
				delete(streamConns, id)
				streamMu.Unlock()
				return
			}
			payload := buf[:n]
			sendWsData(conn, id, base64.StdEncoding.EncodeToString(payload), true)
		}
	}(open.ID, rawConn)
}

func handleWsData(data json.RawMessage) {
	var msg WsData
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("Error unmarshaling ws_data: %v", err)
		return
	}

	streamMu.Lock()
	stream := streamConns[msg.ID]
	streamMu.Unlock()
	if stream == nil {
		return
	}

	payload := []byte(msg.Data)
	if msg.IsBinary {
		decoded, err := base64.StdEncoding.DecodeString(msg.Data)
		if err != nil {
			log.Printf("Error decoding stream data: %v", err)
			return
		}
		payload = decoded
	}
	if err := stream.write(payload); err != nil {
		log.Printf("Error writing stream data: %v", err)
	}
}

func handleWsClose(data json.RawMessage) {
	var msg WsClose
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("Error unmarshaling ws_close: %v", err)
		return
	}
	streamMu.Lock()
	stream := streamConns[msg.ID]
	delete(streamConns, msg.ID)
	streamMu.Unlock()
	if stream != nil {
		stream.conn.Close()
	}
}

func sendWsData(conn *websocket.Conn, id, data string, isBinary bool) {
	msg := TunnelMessage{
		Type: "ws_data",
		Data: mustMarshal(WsData{
			ID:       id,
			Data:     data,
			IsBinary: isBinary,
		}),
	}
	wsMu.Lock()
	err := conn.WriteJSON(msg)
	wsMu.Unlock()
	if err != nil {
		log.Printf("Error writing ws_data: %v", err)
	}
}

func sendWsClose(conn *websocket.Conn, id string, code int, reason string) {
	msg := TunnelMessage{
		Type: "ws_close",
		Data: mustMarshal(WsClose{
			ID:     id,
			Code:   code,
			Reason: reason,
		}),
	}
	wsMu.Lock()
	err := conn.WriteJSON(msg)
	wsMu.Unlock()
	if err != nil {
		log.Printf("Error writing ws_close: %v", err)
	}
}

func forwardToHeadscale(req TunnelRequest) (*TunnelResponse, error) {
	// Extract path from full URL
	reqURL := req.URL
	if len(reqURL) > 0 && reqURL[0] != '/' {
		u, err := url.Parse(reqURL)
		if err == nil {
			reqURL = u.Path
			if u.RawQuery != "" {
				reqURL += "?" + u.RawQuery
			}
		}
	}

	// All tunnel traffic goes to Headscale. The portal is NOT reachable via the
	// public proxy — it is only accessible on the local tailnet (100.64.x.x).
	targetBase := *headscaleURL
	
	var body io.Reader
	if req.Body != nil {
		if req.IsBinary {
			payload, err := base64.StdEncoding.DecodeString(*req.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to decode binary body: %w", err)
			}
			if strings.Contains(reqURL, "/ts2021") {
				preview := payload
				if len(preview) > 16 {
					preview = preview[:16]
				}
				log.Printf("TS2021 HTTP forward (binary): len=%d preview=%x", len(payload), preview)
			}
			body = bytes.NewReader(payload)
		} else {
			body = stringReader(*req.Body)
		}
	}

	httpReq, err := http.NewRequest(req.Method, targetBase+reqURL, body)
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

	if strings.Contains(reqURL, "/ts2021") {
		log.Printf("TS2021 response: %d %s", httpResp.StatusCode, httpResp.Header.Get("Content-Type"))
	}

	contentType := httpResp.Header.Get("Content-Type")
	isText := strings.HasPrefix(contentType, "text/") ||
		strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "xml") ||
		strings.Contains(contentType, "x-www-form-urlencoded")

	bodyStr := ""
	isBinary := false
	if len(respBody) > 0 {
		if isText {
			bodyStr = string(respBody)
		} else {
			bodyStr = base64.StdEncoding.EncodeToString(respBody)
			isBinary = true
		}
	}

	return &TunnelResponse{
		ID:       req.ID,
		Status:   httpResp.StatusCode,
		Headers:  headers,
		Body:     stringPtr(bodyStr),
		IsBinary: isBinary,
	}, nil
}

func buildHeadscaleURL(requestURL string, ws bool) (string, error) {
	base, err := url.Parse(*headscaleURL)
	if err != nil {
		return "", err
	}

	reqURL := requestURL
	if len(reqURL) > 0 && reqURL[0] != '/' {
		u, err := url.Parse(reqURL)
		if err == nil {
			base.Path = u.Path
			base.RawQuery = u.RawQuery
		} else {
			base.Path = reqURL
		}
	} else {
		base.Path = reqURL
	}

	if ws {
		base.Scheme = strings.Replace(base.Scheme, "http", "ws", 1)
	}
	return base.String(), nil
}

func headscaleDialAddress() (string, error) {
	u, err := url.Parse(*headscaleURL)
	if err != nil {
		return "", err
	}
	host := u.Host
	if host == "" {
		host = strings.TrimPrefix(*headscaleURL, "http://")
		host = strings.TrimPrefix(host, "https://")
	}
	if !strings.Contains(host, ":") {
		if u.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	return host, nil
}

func buildRawRequest(method, requestURL string, headers map[string]string, body []byte, host string) []byte {
	u, _ := url.Parse(requestURL)
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s %s HTTP/1.1\r\n", method, path)
	hasContentLength := false
	for k, v := range headers {
		if strings.EqualFold(k, "Host") {
			continue
		}
		if strings.EqualFold(k, "Content-Length") {
			hasContentLength = true
		}
		fmt.Fprintf(&buf, "%s: %s\r\n", k, v)
	}
	fmt.Fprintf(&buf, "Host: %s\r\n", host)
	if len(body) > 0 && !hasContentLength {
		fmt.Fprintf(&buf, "Content-Length: %d\r\n", len(body))
	}
	fmt.Fprintf(&buf, "\r\n")
	if len(body) > 0 {
		buf.Write(body)
	}
	return buf.Bytes()
}

func dockerClient() (*http.Client, error) {
	socketPath := "/var/run/docker.sock"
	if _, err := os.Stat(socketPath); err != nil {
		return nil, fmt.Errorf("docker socket not available: %w", err)
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
		Timeout: 30 * time.Second,
	}, nil
}

func restartHeadscaleContainer(container string) error {
	client, err := dockerClient()
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, "http://docker/containers/"+container+"/restart", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("docker restart failed: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

// dockerExec runs a command inside a container via the Docker API and returns stdout.
func dockerExec(container string, cmd []string) (string, error) {
	client, err := dockerClient()
	if err != nil {
		return "", err
	}

	// Step 1: Create exec instance
	createBody, _ := json.Marshal(map[string]interface{}{
		"AttachStdout": true,
		"AttachStderr": true,
		"Cmd":          cmd,
	})
	createReq, _ := http.NewRequest(http.MethodPost,
		"http://docker/containers/"+container+"/exec",
		bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")

	createResp, err := client.Do(createReq)
	if err != nil {
		return "", fmt.Errorf("exec create: %w", err)
	}
	defer createResp.Body.Close()

	if createResp.StatusCode != 201 {
		body, _ := io.ReadAll(createResp.Body)
		return "", fmt.Errorf("exec create failed (%d): %s", createResp.StatusCode, string(body))
	}

	var execCreate struct {
		Id string `json:"Id"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&execCreate); err != nil {
		return "", fmt.Errorf("exec create decode: %w", err)
	}

	// Step 2: Start exec and capture output
	startBody, _ := json.Marshal(map[string]interface{}{"Detach": false, "Tty": false})
	startReq, _ := http.NewRequest(http.MethodPost,
		"http://docker/exec/"+execCreate.Id+"/start",
		bytes.NewReader(startBody))
	startReq.Header.Set("Content-Type", "application/json")

	startResp, err := client.Do(startReq)
	if err != nil {
		return "", fmt.Errorf("exec start: %w", err)
	}
	defer startResp.Body.Close()

	if startResp.StatusCode != 200 {
		body, _ := io.ReadAll(startResp.Body)
		return "", fmt.Errorf("exec start failed (%d): %s", startResp.StatusCode, string(body))
	}

	// Docker multiplexed stream: 8-byte header per frame [type(1) padding(3) size(4)] + payload
	var output strings.Builder
	header := make([]byte, 8)
	for {
		_, err := io.ReadFull(startResp.Body, header)
		if err != nil {
			break
		}
		size := int(header[4])<<24 | int(header[5])<<16 | int(header[6])<<8 | int(header[7])
		if size <= 0 {
			continue
		}
		frame := make([]byte, size)
		if _, err := io.ReadFull(startResp.Body, frame); err != nil {
			break
		}
		// header[0]: 1=stdout, 2=stderr; only capture stdout
		if header[0] == 1 {
			output.Write(frame)
		}
	}

	// Step 3: Check exit code
	inspectReq, _ := http.NewRequest(http.MethodGet, "http://docker/exec/"+execCreate.Id+"/json", nil)
	inspectResp, err := client.Do(inspectReq)
	if err == nil {
		defer inspectResp.Body.Close()
		var inspect struct {
			ExitCode int `json:"ExitCode"`
		}
		if json.NewDecoder(inspectResp.Body).Decode(&inspect) == nil && inspect.ExitCode != 0 {
			return "", fmt.Errorf("command exited with code %d", inspect.ExitCode)
		}
	}

	return strings.TrimSpace(output.String()), nil
}

// bootstrapAPIKey creates a Headscale API key via docker exec if none is configured.
func bootstrapAPIKey(container string) (string, error) {
	log.Printf("Bootstrapping Headscale API key via docker exec...")
	key, err := dockerExec(container, []string{
		"headscale", "apikeys", "create", "--expiration", "8760h",
	})
	if err != nil {
		return "", fmt.Errorf("failed to create API key: %w", err)
	}
	if key == "" {
		return "", fmt.Errorf("headscale returned empty API key")
	}
	log.Printf("✓ API key bootstrapped")
	return key, nil
}

func stringPtr(s string) *string { return &s }
func stringReader(s string) io.Reader {
	return bytes.NewBufferString(s)
}
func mustMarshal(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func portalEnabled() bool {
	v := os.Getenv("PORTAL_ENABLED")
	return v == "1" || strings.EqualFold(v, "true")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// registerWithProxy performs X25519 ECDH + HMAC-SHA256 challenge-response registration
func registerWithProxy(proxyURL, headscaleURL, noiseKeyPath string) (RegistrationResult, error) {
	// 1. Load X25519 private key from Headscale
	x25519Key, err := loadNoisePrivateKey(noiseKeyPath)
	if err != nil {
		return RegistrationResult{}, fmt.Errorf("failed to load noise key: %w", err)
	}

	// 2. Get Headscale's Noise public key (X25519)
	resp, err := http.Get(headscaleURL + "/key?v=96")
	if err != nil {
		return RegistrationResult{}, fmt.Errorf("failed to get Headscale key: %w", err)
	}
	defer resp.Body.Close()

	var keyData HeadscaleKeyResponse
	if err := json.NewDecoder(resp.Body).Decode(&keyData); err != nil {
		return RegistrationResult{}, fmt.Errorf("failed to decode key: %w", err)
	}

	log.Printf("Headscale X25519 public key: %s", keyData.PublicKey)
	localFingerprint := computeFingerprint(keyData.PublicKey)

	// 3. Phase 1: Request challenge
	initReq := RegistrationInitRequest{
		PublicKey: keyData.PublicKey,
	}
	initBody, _ := json.Marshal(initReq)

	resp, err = http.Post(proxyURL+"/api/register/init", "application/json", bytes.NewReader(initBody))
	if err != nil {
		return RegistrationResult{}, fmt.Errorf("failed to init registration: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return RegistrationResult{}, fmt.Errorf("registration init failed: %s", string(body))
	}

	var initResp RegistrationInitResponse
	if err := json.NewDecoder(resp.Body).Decode(&initResp); err != nil {
		return RegistrationResult{}, fmt.Errorf("failed to decode init response: %w", err)
	}

	log.Printf("Challenge received: fingerprint=%s", initResp.Fingerprint)
	if initResp.Fingerprint != localFingerprint {
		return RegistrationResult{}, fmt.Errorf("proxy fingerprint mismatch: local=%s proxy=%s", localFingerprint, initResp.Fingerprint)
	}

	// 4. Decode proxy's ephemeral X25519 public key
	proxyPublicKeyBytes, err := hex.DecodeString(initResp.ProxyPublicKey)
	if err != nil {
		return RegistrationResult{}, fmt.Errorf("failed to decode proxy public key: %w", err)
	}

	// 5. Compute shared secret via X25519 ECDH
	curve := ecdh.X25519()

	sidecarPrivateKey, err := curve.NewPrivateKey(x25519Key)
	if err != nil {
		return RegistrationResult{}, fmt.Errorf("failed to create X25519 private key: %w", err)
	}

	proxyPublicKey, err := curve.NewPublicKey(proxyPublicKeyBytes)
	if err != nil {
		return RegistrationResult{}, fmt.Errorf("failed to create proxy public key: %w", err)
	}

	sharedSecret, err := sidecarPrivateKey.ECDH(proxyPublicKey)
	if err != nil {
		return RegistrationResult{}, fmt.Errorf("ECDH failed: %w", err)
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
		return RegistrationResult{}, fmt.Errorf("failed to verify registration: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return RegistrationResult{}, fmt.Errorf("registration verification failed: %s", string(body))
	}

	var verifyResp RegistrationVerifyResponse
	if err := json.NewDecoder(resp.Body).Decode(&verifyResp); err != nil {
		return RegistrationResult{}, fmt.Errorf("failed to decode verify response: %w", err)
	}

	log.Printf("✓ Registered at: %s", verifyResp.PublicURL)
	if verifyResp.Fingerprint != localFingerprint {
		return RegistrationResult{}, fmt.Errorf("proxy verify fingerprint mismatch: local=%s proxy=%s", localFingerprint, verifyResp.Fingerprint)
	}
	if verifyResp.PublicURL != publicURLFromFingerprint(localFingerprint) {
		return RegistrationResult{}, fmt.Errorf("proxy public URL mismatch: expected=%s got=%s", publicURLFromFingerprint(localFingerprint), verifyResp.PublicURL)
	}
	if !strings.HasPrefix(verifyResp.TunnelURL, "wss://"+localFingerprint+".headfwd.net/") {
		return RegistrationResult{}, fmt.Errorf("proxy tunnel URL mismatch: got=%s", verifyResp.TunnelURL)
	}
	return RegistrationResult{
		Fingerprint: localFingerprint,
		TunnelURL:   verifyResp.TunnelURL,
		PublicURL:   verifyResp.PublicURL,
	}, nil
}

// loadNoisePrivateKey reads Headscale's X25519 Noise private key from disk
func loadNoisePrivateKey(keyPath string) ([]byte, error) {
	// Auto-detect key path if not specified
	if keyPath == "" {
		paths := []string{
			"/var/lib/headscale/noise_private.key",
			"/keys/noise_private.key",
			"./headscale/data/noise_private.key",
			"../headscale/data/noise_private.key", // running from headfwd-sidecar/ in dev
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

// ── Tsnet self-registration ────────────────────────────────────────────────

// defaultTsnetDir returns a writable directory for tsnet's WireGuard state.
// Prefers /var/lib/headfwd-ts (Docker / production), falls back to the OS
// user cache dir (~/.cache/headfwd-ts on Linux, ~/Library/Caches/headfwd-ts
// on macOS) so `make dev` works without root.
func defaultTsnetDir() string {
	if err := os.MkdirAll("/var/lib/headfwd-ts", 0700); err == nil {
		return "/var/lib/headfwd-ts"
	}
	if cacheDir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cacheDir, "headfwd-ts")
	}
	return filepath.Join(os.TempDir(), "headfwd-ts")
}

// startTsnetNode joins the local headscale network as "headfwd-server" using tsnet
// (userspace WireGuard — no kernel TUN device or CAP_NET_ADMIN required).
// It returns the node's tailnet IPv4 address and a net.Listener on port 3001 of
// that tailnet interface. Portal traffic arriving on that listener has a genuine
// 100.64.x.x remote address for the connecting peer.
func startTsnetNode(hsClient *headscale.Client, headscaleURL, stateDir string) (ip4 string, ln net.Listener, err error) {
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return "", nil, fmt.Errorf("create tsnet state dir: %w", err)
	}

	// Only create a new preauth key on first start (no existing state file).
	stateFile := filepath.Join(stateDir, "tailscaled.state")
	var authKey string
	if _, statErr := os.Stat(stateFile); os.IsNotExist(statErr) {
		authKey, err = provisionServerNode(hsClient)
		if err != nil {
			return "", nil, fmt.Errorf("provision server node: %w", err)
		}
	}

	srv := &tsnet.Server{
		Hostname:   "headfwd-server",
		AuthKey:    authKey,
		ControlURL: headscaleURL,
		Dir:        stateDir,
		Ephemeral:  false,
		// Suppress tsnet's chatty internal logs.
		Logf: func(format string, args ...any) {},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if _, err = srv.Up(ctx); err != nil {
		srv.Close()
		return "", nil, fmt.Errorf("tsnet up: %w", err)
	}

	lc, err := srv.LocalClient()
	if err != nil {
		srv.Close()
		return "", nil, fmt.Errorf("tsnet local client: %w", err)
	}

	status, err := lc.Status(ctx)
	if err != nil {
		srv.Close()
		return "", nil, fmt.Errorf("tsnet status: %w", err)
	}

	for _, ip := range status.TailscaleIPs {
		if ip.Is4() {
			ip4 = ip.String()
			break
		}
	}
	if ip4 == "" {
		srv.Close()
		return "", nil, fmt.Errorf("tsnet: no IPv4 tailnet address assigned")
	}

	ln, err = srv.Listen("tcp", ":3001")
	if err != nil {
		srv.Close()
		return "", nil, fmt.Errorf("tsnet listen :3001: %w", err)
	}

	log.Printf("✓ Joined tailnet as headfwd-server (%s)", ip4)
	return ip4, ln, nil
}

// provisionServerNode ensures the "headfwd-server" headscale user exists and
// creates a single-use preauth key for it (used only on the first tsnet start).
func provisionServerNode(hsClient *headscale.Client) (string, error) {
	const serverUser = "headfwd-server"

	user, err := hsClient.GetUserByName(serverUser)
	if err != nil {
		user, err = hsClient.CreateUser(serverUser)
		if err != nil {
			return "", fmt.Errorf("create headscale user %q: %w", serverUser, err)
		}
		log.Printf("Created headscale user: %s", serverUser)
	}

	userID, err := user.NumericID()
	if err != nil {
		return "", fmt.Errorf("invalid user ID for %q: %w", serverUser, err)
	}

	expiration := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)
	key, err := hsClient.CreatePreauthKey(headscale.CreateKeyRequest{
		User:       userID,
		Reusable:   false,
		Ephemeral:  false,
		Expiration: expiration,
	})
	if err != nil {
		return "", fmt.Errorf("create server preauth key: %w", err)
	}
	return key.Key, nil
}
