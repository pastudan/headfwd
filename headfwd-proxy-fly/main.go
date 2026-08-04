package main

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type TunnelMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type TunnelRequest struct {
	ID       string            `json:"id"`
	Method   string            `json:"method"`
	URL      string            `json:"url"`
	Headers  map[string]string `json:"headers"`
	Body     *string           `json:"body,omitempty"`
	IsBinary bool              `json:"isBinary,omitempty"`
}

type TunnelResponse struct {
	ID       string            `json:"id"`
	Status   int               `json:"status"`
	Headers  map[string]string `json:"headers"`
	Body     *string           `json:"body,omitempty"`
	IsBinary bool              `json:"isBinary,omitempty"`
}

type WsOpen struct {
	ID       string            `json:"id"`
	Method   string            `json:"method"`
	URL      string            `json:"url"`
	Headers  map[string]string `json:"headers"`
	Body     *string           `json:"body,omitempty"`
	IsBinary bool              `json:"isBinary,omitempty"`
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

type HeadscaleKeyResponse struct {
	PublicKey string `json:"publicKey"`
}

type RegistrationInitRequest struct {
	PublicKey string `json:"publicKey"`
}

type RegistrationInitResponse struct {
	Fingerprint    string `json:"fingerprint"`
	Nonce          string `json:"nonce"`
	ProxyPublicKey string `json:"proxyPublicKey"`
	ExpiresAt      int64  `json:"expiresAt"`
	// ProtocolVersion lets a sidecar detect a relay that predates v2. Absent
	// (zero) means a v1 relay, which a v2 sidecar must refuse rather than
	// silently downgrade to the weaker proof.
	ProtocolVersion int `json:"protocolVersion"`
}

type RegistrationVerifyRequest struct {
	Fingerprint string `json:"fingerprint"`
	Proof       string `json:"proof"`
}

type RegistrationVerifyResponse struct {
	Fingerprint string `json:"fingerprint"`
	TunnelURL   string `json:"tunnelUrl"`
	PublicURL   string `json:"publicUrl"`
}

type Challenge struct {
	PublicKey     string
	Nonce         string
	EphemeralPriv []byte
	// EphemeralPubHex is retained because v2 binds it into the proof transcript,
	// so verify must MAC exactly the value that was sent at init.
	EphemeralPubHex string
	ExpiresAt       time.Time
	// Attempts counts failed proofs. v1 never cleared a challenge on failure,
	// so one nonce could be attacked until it expired.
	Attempts int
}

type Registration struct {
	PublicKey string
	Secret    string
	CreatedAt time.Time
}

type tunnelConn struct {
	ws *websocket.Conn
	mu sync.Mutex
}

func (t *tunnelConn) send(msg interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ws.WriteJSON(msg)
}

type streamClient struct {
	conn net.Conn
	mu   sync.Mutex
}

func (c *streamClient) write(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.conn.Write(data)
	return err
}

type Server struct {
	mu            sync.RWMutex
	challenges    map[string]Challenge
	registrations map[string]Registration
	tunnels       map[string]*tunnelConn
	pending       map[string]chan TunnelResponse
	streams       map[string]*streamClient

	// Registration is unauthenticated and does real crypto work, so both
	// endpoints are rate limited per source address.
	regLimiter *rateLimiter
}

func newServer() *Server {
	return &Server{
		challenges:    map[string]Challenge{},
		registrations: map[string]Registration{},
		tunnels:       map[string]*tunnelConn{},
		pending:       map[string]chan TunnelResponse{},
		streams:       map[string]*streamClient{},
		// Generous for real use — a sidecar registers once at startup and again
		// only if the tunnel drops — but low enough to make brute force and CPU
		// exhaustion pointless.
		regLimiter: newRateLimiter(30, 10),
	}
}

func main() {
	srv := newServer()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", srv.handleHealth)
	mux.HandleFunc("/api/register/init", srv.handleRegisterInit)
	mux.HandleFunc("/api/register/verify", srv.handleRegisterVerify)
	mux.HandleFunc("/tunnel", srv.handleTunnel)
	mux.HandleFunc("/", srv.handleClient)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("HeadFwd Fly proxy listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("listen: %v", err)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func (s *Server) handleRegisterInit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.regLimiter.allow(clientIP(r)) {
		http.Error(w, "too many registration attempts", http.StatusTooManyRequests)
		return
	}

	// Cap the body. Without this an attacker streams an unbounded JSON document
	// and the decoder happily buffers all of it.
	var req RegistrationInitRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.PublicKey == "" || !strings.HasPrefix(req.PublicKey, "mkey:") {
		http.Error(w, "invalid publicKey", http.StatusBadRequest)
		return
	}

	// Reject a malformed key here rather than at verify. The old code accepted
	// any "mkey:"-prefixed string at init and only failed during ECDH, which
	// meant a garbage key still cost a keygen and occupied a challenge slot.
	if _, err := hex.DecodeString(strings.TrimPrefix(req.PublicKey, "mkey:")); err != nil {
		http.Error(w, "invalid publicKey", http.StatusBadRequest)
		return
	}

	fingerprint := computeFingerprint(req.PublicKey)

	// Do not let an unauthenticated caller clobber a challenge that is already
	// in flight. Previously any party could POST /init for someone else's
	// fingerprint and overwrite their nonce mid-registration — a trivial way to
	// make a legitimate sidecar's verify fail forever.
	//
	// Whoever holds the private key will succeed on retry once the existing
	// challenge expires, so this costs a real client at most one TTL.
	s.mu.Lock()
	if existing, ok := s.challenges[fingerprint]; ok && time.Now().Before(existing.ExpiresAt) {
		s.mu.Unlock()
		http.Error(w, "registration already in progress for this key", http.StatusConflict)
		return
	}
	s.mu.Unlock()

	nonce := randomHex(32)

	curve := ecdh.X25519()
	ephemeralPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		http.Error(w, "failed to generate key", http.StatusInternalServerError)
		return
	}
	ephemeralPub := ephemeralPriv.PublicKey()

	expiresAt := time.Now().Add(5 * time.Minute)

	ephemeralPubHex := hex.EncodeToString(ephemeralPub.Bytes())

	s.mu.Lock()
	s.challenges[fingerprint] = Challenge{
		PublicKey:       req.PublicKey,
		Nonce:           nonce,
		EphemeralPriv:   ephemeralPriv.Bytes(),
		EphemeralPubHex: ephemeralPubHex,
		ExpiresAt:       expiresAt,
	}
	s.mu.Unlock()

	resp := RegistrationInitResponse{
		Fingerprint:     fingerprint,
		Nonce:           nonce,
		ProxyPublicKey:  ephemeralPubHex,
		ExpiresAt:       expiresAt.UnixMilli(),
		ProtocolVersion: ProtocolVersion,
	}
	writeJSON(w, resp)
}

func (s *Server) handleRegisterVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !s.regLimiter.allow(clientIP(r)) {
		http.Error(w, "too many registration attempts", http.StatusTooManyRequests)
		return
	}

	var req RegistrationVerifyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.Fingerprint == "" || req.Proof == "" {
		http.Error(w, "missing fingerprint or proof", http.StatusBadRequest)
		return
	}

	// Count this attempt before doing any crypto, and burn the challenge after
	// a few failures. Previously a failed proof left the challenge intact, so a
	// single nonce could be hammered for its whole 5-minute TTL.
	const maxAttempts = 5

	s.mu.Lock()
	challenge, ok := s.challenges[req.Fingerprint]
	if ok && time.Now().After(challenge.ExpiresAt) {
		delete(s.challenges, req.Fingerprint)
		ok = false
	}
	if ok {
		challenge.Attempts++
		if challenge.Attempts > maxAttempts {
			delete(s.challenges, req.Fingerprint)
			ok = false
		} else {
			s.challenges[req.Fingerprint] = challenge
		}
	}
	s.mu.Unlock()
	if !ok {
		http.Error(w, "challenge expired or not found", http.StatusBadRequest)
		return
	}

	sidecarPubBytes, err := hex.DecodeString(strings.TrimPrefix(challenge.PublicKey, "mkey:"))
	if err != nil {
		http.Error(w, "invalid public key", http.StatusBadRequest)
		return
	}

	curve := ecdh.X25519()
	ephemeralPriv, err := curve.NewPrivateKey(challenge.EphemeralPriv)
	if err != nil {
		http.Error(w, "invalid proxy key", http.StatusInternalServerError)
		return
	}
	sidecarPub, err := curve.NewPublicKey(sidecarPubBytes)
	if err != nil {
		http.Error(w, "invalid sidecar key", http.StatusBadRequest)
		return
	}

	sharedSecret, err := ephemeralPriv.ECDH(sidecarPub)
	if err != nil {
		http.Error(w, "ECDH failed", http.StatusInternalServerError)
		return
	}

	expected, err := registrationProof(
		sharedSecret,
		req.Fingerprint,
		challenge.PublicKey,
		challenge.Nonce,
		challenge.EphemeralPubHex,
	)
	if err != nil {
		http.Error(w, "key derivation failed", http.StatusInternalServerError)
		return
	}
	if !timingSafeEqual(req.Proof, expected) {
		http.Error(w, "invalid proof", http.StatusForbidden)
		return
	}

	secret := randomHex(32)

	s.mu.Lock()
	s.registrations[req.Fingerprint] = Registration{
		PublicKey: challenge.PublicKey,
		Secret:    secret,
		CreatedAt: time.Now(),
	}
	delete(s.challenges, req.Fingerprint)
	s.mu.Unlock()

	baseHost := publicHostFromRequest(r)
	scheme := publicSchemeFromRequest(r)
	wsScheme := "wss"
	if scheme == "http" {
		wsScheme = "ws"
	}

	resp := RegistrationVerifyResponse{
		Fingerprint: req.Fingerprint,
		TunnelURL:   fmt.Sprintf("%s://%s.%s/tunnel?auth=%s", wsScheme, req.Fingerprint, baseHost, secret),
		PublicURL:   fmt.Sprintf("%s://%s.%s", scheme, req.Fingerprint, baseHost),
	}
	writeJSON(w, resp)
}

func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request) {
	auth := r.URL.Query().Get("auth")
	if auth == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	fingerprint := extractFingerprint(r.Host)
	if fingerprint == "" {
		http.Error(w, "invalid subdomain", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	reg, ok := s.registrations[fingerprint]
	s.mu.RUnlock()
	if !ok || reg.Secret != auth {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	upgrader := websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("tunnel upgrade failed: %v", err)
		return
	}

	tunnel := &tunnelConn{ws: conn}
	s.mu.Lock()
	if old, ok := s.tunnels[fingerprint]; ok {
		_ = old.ws.Close()
	}
	s.tunnels[fingerprint] = tunnel
	s.mu.Unlock()

	log.Printf("Tunnel connected: %s", fingerprint)
	go s.readTunnel(fingerprint, tunnel)
}

func (s *Server) handleClient(w http.ResponseWriter, r *http.Request) {
	if r.Host == "" {
		http.Error(w, "missing host", http.StatusBadRequest)
		return
	}
	fingerprint := extractFingerprint(r.Host)
	if fingerprint == "" {
		http.Error(w, "invalid subdomain", http.StatusBadRequest)
		return
	}

	upgrade := r.Header.Get("Upgrade")
	if strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") && upgrade != "" {
		s.handleStreamUpgrade(w, r, fingerprint)
		return
	}

	s.forwardHTTP(w, r, fingerprint)
}

func (s *Server) forwardHTTP(w http.ResponseWriter, r *http.Request, fingerprint string) {
	tunnel := s.getTunnel(fingerprint)
	if tunnel == nil {
		http.Error(w, "Headscale not connected", http.StatusServiceUnavailable)
		return
	}

	body, isBinary := readRequestBody(r)
	req := TunnelRequest{
		ID:       randomHex(16),
		Method:   r.Method,
		URL:      r.URL.String(),
		Headers:  headersToMap(r.Header),
		Body:     body,
		IsBinary: isBinary,
	}

	respCh := make(chan TunnelResponse, 1)
	s.mu.Lock()
	s.pending[req.ID] = respCh
	s.mu.Unlock()

	if err := tunnel.send(TunnelMessage{Type: "request", Data: mustMarshal(req)}); err != nil {
		s.clearPending(req.ID)
		http.Error(w, "tunnel write failed", http.StatusBadGateway)
		return
	}

	select {
	case resp := <-respCh:
		writeTunnelResponse(w, resp)
	case <-time.After(30 * time.Second):
		s.clearPending(req.ID)
		http.Error(w, "timeout", http.StatusGatewayTimeout)
	}
}

func (s *Server) handleStreamUpgrade(w http.ResponseWriter, r *http.Request, fingerprint string) {
	tunnel := s.getTunnel(fingerprint)
	if tunnel == nil {
		http.Error(w, "Headscale not connected", http.StatusServiceUnavailable)
		return
	}

	body, isBinary := readRequestBody(r)

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	conn, buf, err := hj.Hijack()
	if err != nil {
		http.Error(w, "hijack failed", http.StatusInternalServerError)
		return
	}
	_ = buf.Flush()

	streamID := randomHex(16)
	s.mu.Lock()
	s.streams[streamID] = &streamClient{conn: conn}
	s.mu.Unlock()

	open := WsOpen{
		ID:       streamID,
		Method:   r.Method,
		URL:      r.URL.String(),
		Headers:  headersToMap(r.Header),
		Body:     body,
		IsBinary: isBinary,
	}

	if err := tunnel.send(TunnelMessage{Type: "ws_open", Data: mustMarshal(open)}); err != nil {
		s.removeStream(streamID)
		return
	}

	go s.relayClientToTunnel(streamID, tunnel, conn)
}

func (s *Server) relayClientToTunnel(streamID string, tunnel *tunnelConn, conn net.Conn) {
	defer s.removeStream(streamID)
	buf := make([]byte, 32*1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			_ = tunnel.send(TunnelMessage{Type: "ws_close", Data: mustMarshal(WsClose{ID: streamID})})
			return
		}
		chunk := buf[:n]
		msg := WsData{
			ID:       streamID,
			Data:     toBase64(chunk),
			IsBinary: true,
		}
		if err := tunnel.send(TunnelMessage{Type: "ws_data", Data: mustMarshal(msg)}); err != nil {
			return
		}
	}
}

func (s *Server) readTunnel(fingerprint string, tunnel *tunnelConn) {
	defer func() {
		_ = tunnel.ws.Close()
		s.mu.Lock()
		delete(s.tunnels, fingerprint)
		s.mu.Unlock()
	}()

	for {
		var msg TunnelMessage
		if err := tunnel.ws.ReadJSON(&msg); err != nil {
			return
		}

		switch msg.Type {
		case "response":
			var resp TunnelResponse
			if err := json.Unmarshal(msg.Data, &resp); err != nil {
				continue
			}
			s.mu.Lock()
			ch := s.pending[resp.ID]
			delete(s.pending, resp.ID)
			s.mu.Unlock()
			if ch != nil {
				ch <- resp
			}
		case "ws_data":
			var data WsData
			if err := json.Unmarshal(msg.Data, &data); err != nil {
				continue
			}
			s.mu.RLock()
			stream := s.streams[data.ID]
			s.mu.RUnlock()
			if stream == nil {
				continue
			}
			payload := fromBase64(data.Data)
			_ = stream.write(payload)
		case "ws_close":
			var closeMsg WsClose
			if err := json.Unmarshal(msg.Data, &closeMsg); err != nil {
				continue
			}
			s.removeStream(closeMsg.ID)
		case "ping":
			_ = tunnel.send(TunnelMessage{Type: "pong", Data: mustMarshal(map[string]string{})})
		}
	}
}

func (s *Server) getTunnel(fingerprint string) *tunnelConn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tunnels[fingerprint]
}

func (s *Server) clearPending(id string) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

func (s *Server) removeStream(id string) {
	s.mu.Lock()
	stream := s.streams[id]
	delete(s.streams, id)
	s.mu.Unlock()
	if stream != nil {
		_ = stream.conn.Close()
	}
}

func readRequestBody(r *http.Request) (*string, bool) {
	if r.Body == nil {
		return nil, false
	}
	defer r.Body.Close()

	data, err := io.ReadAll(r.Body)
	if err != nil || len(data) == 0 {
		return nil, false
	}

	contentType := r.Header.Get("Content-Type")
	isText := strings.HasPrefix(contentType, "text/") ||
		strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "xml") ||
		strings.Contains(contentType, "x-www-form-urlencoded")

	if isText {
		body := string(data)
		return &body, false
	}

	encoded := toBase64(data)
	return &encoded, true
}

func writeTunnelResponse(w http.ResponseWriter, resp TunnelResponse) {
	for k, v := range resp.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(resp.Status)

	if resp.Body == nil {
		return
	}
	if resp.IsBinary {
		_, _ = w.Write(fromBase64(*resp.Body))
		return
	}
	_, _ = w.Write([]byte(*resp.Body))
}

func headersToMap(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func mustMarshal(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func computeFingerprint(pubkey string) string {
	sum := sha256.Sum256([]byte(pubkey))
	full := hex.EncodeToString(sum[:])
	if len(full) < 32 {
		return full
	}
	return full[:32]
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

// hmacSHA256 computes a raw HMAC tag. The v2 proof derives its key with HKDF
// first (see registration_crypto.go); this is only the MAC primitive.
func hmacSHA256(key, msg []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(msg)
	return h.Sum(nil)
}

func timingSafeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	result := 0
	for i := 0; i < len(a); i++ {
		result |= int(a[i] ^ b[i])
	}
	return result == 0
}

func toBase64(buf []byte) string {
	if len(buf) == 0 {
		return ""
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func fromBase64(s string) []byte {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	return data
}

var fingerprintRe = regexp.MustCompile(`^([a-f0-9]{32})\.(.+)$`)

func extractFingerprint(hostport string) string {
	host := stripPort(hostport)
	m := fingerprintRe.FindStringSubmatch(strings.ToLower(host))
	if len(m) == 3 {
		return m[1]
	}
	return ""
}

func baseHostFromHost(hostport string) string {
	host := stripPort(hostport)
	m := fingerprintRe.FindStringSubmatch(strings.ToLower(host))
	if len(m) == 3 {
		return m[2]
	}
	return host
}

func stripPort(hostport string) string {
	if strings.Contains(hostport, ":") {
		if h, _, err := net.SplitHostPort(hostport); err == nil {
			return h
		}
		parts := strings.Split(hostport, ":")
		return parts[0]
	}
	return hostport
}

func publicHostFromRequest(r *http.Request) string {
	if v := os.Getenv("PUBLIC_HOST"); v != "" {
		return v
	}
	return baseHostFromHost(r.Host)
}

func publicSchemeFromRequest(r *http.Request) string {
	if v := os.Getenv("PUBLIC_SCHEME"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-Proto"); v != "" {
		return v
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func buildRawRequest(method, requestURL string, headers map[string]string, body []byte, host string) []byte {
	u, _ := url.Parse(requestURL)
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s %s HTTP/1.1\r\n", method, path)
	for k, v := range headers {
		if strings.EqualFold(k, "Host") {
			continue
		}
		fmt.Fprintf(&buf, "%s: %s\r\n", k, v)
	}
	fmt.Fprintf(&buf, "Host: %s\r\n", host)
	fmt.Fprintf(&buf, "\r\n")
	if len(body) > 0 {
		buf.Write(body)
	}
	return buf.Bytes()
}
