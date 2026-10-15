package portal

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/headfwd/sidecar/portal/identity"
)

// These tests replay the attacks reproduced against a live headscale + tsnet
// setup: a non-admin tailnet peer (mallory, 100.64.0.3) sending forged
// X-Forwarded-For / X-Real-Ip headers to gain admin or impersonate alice
// (100.64.0.2), and a cross-site page driving a LAN user's browser.

const (
	aliceIP   = "100.64.0.2"
	malloryIP = "100.64.0.3"
)

type fakeHeadscale struct {
	*httptest.Server
	preauthKeysMinted atomic.Int32
	usersCreated      atomic.Int32
}

func newFakeHeadscale(t *testing.T) *fakeHeadscale {
	t.Helper()
	f := &fakeHeadscale{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/user", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"users":[
			{"id":"1","name":"alice","createdAt":"2026-01-01T00:00:00Z"},
			{"id":"2","name":"mallory","createdAt":"2026-01-02T00:00:00Z"}]}`)
	})
	mux.HandleFunc("POST /api/v1/user", func(w http.ResponseWriter, r *http.Request) {
		f.usersCreated.Add(1)
		io.WriteString(w, `{"user":{"id":"9","name":"x"}}`)
	})
	mux.HandleFunc("GET /api/v1/node", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"nodes":[
			{"id":"2","givenName":"alice-phone","ipAddresses":["`+aliceIP+`","fd7a:115c:a1e0::2"],"user":{"id":"1","name":"alice"}},
			{"id":"3","givenName":"mallory-laptop","ipAddresses":["`+malloryIP+`","fd7a:115c:a1e0::3"],"user":{"id":"2","name":"mallory"}}]}`)
	})
	mux.HandleFunc("POST /api/v1/preauthkey", func(w http.ResponseWriter, r *http.Request) {
		f.preauthKeysMinted.Add(1)
		io.WriteString(w, `{"preAuthKey":{"key":"SECRET-PREAUTH-KEY"}}`)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

// newUpstream echoes the identity/address headers it receives as JSON.
func newUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := map[string]string{}
		for k, v := range r.Header {
			lk := strings.ToLower(k)
			if strings.HasPrefix(lk, "x-tailscale") || strings.HasPrefix(lk, "x-forwarded") ||
				lk == "x-real-ip" || lk == "forwarded" {
				out[k] = strings.Join(v, ", ")
			}
		}
		json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(s.Close)
	return s
}

func newPortal(t *testing.T, ln identity.Listener) (http.Handler, *fakeHeadscale) {
	t.Helper()
	hs := newFakeHeadscale(t)
	dir := t.TempDir()
	// alice is the only admin (what Bootstrap would produce).
	if err := os.WriteFile(filepath.Join(dir, "admins.json"), []byte(`["alice"]`), 0600); err != nil {
		t.Fatal(err)
	}
	h := NewServer(Config{
		HeadscaleURL: hs.URL,
		APIKey:       "test",
		StateDir:     dir,
		DevMode:      true, // no embedded frontend needed
		UpstreamURL:  newUpstream(t).URL,
	}, ln)
	return h, hs
}

type call struct {
	method, path, body, remote string
	hdr                        map[string]string
}

func do(h http.Handler, c call) *httptest.ResponseRecorder {
	r := httptest.NewRequest(c.method, "http://100.64.0.1:3001"+c.path, strings.NewReader(c.body))
	r.RemoteAddr = c.remote
	if c.body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

var spoofHeaders = []map[string]string{
	{"X-Forwarded-For": "127.0.0.1"},
	{"X-Forwarded-For": "127.0.0.1, 10.0.0.1"},
	{"X-Real-Ip": "10.0.0.5"},
	{"X-Real-IP": "192.168.1.1"},
	{"Forwarded": "for=127.0.0.1"},
	{"X-Forwarded-For": aliceIP}, // impersonate the admin user
}

func TestTailnetSpoofedHeadersDoNotGrantAdmin(t *testing.T) {
	h, hs := newPortal(t, identity.Tailnet)
	mallory := malloryIP + ":40000"

	adminRoutes := []call{
		{method: "GET", path: "/api/users"},
		{method: "POST", path: "/api/users", body: `{"name":"evil"}`},
		{method: "DELETE", path: "/api/users/alice"},
		{method: "POST", path: "/api/keys", body: `{"user":"alice","reusable":true}`},
		{method: "GET", path: "/api/nodes"},
		{method: "DELETE", path: "/api/nodes/2"},
		{method: "GET", path: "/api/admins"},
		{method: "PUT", path: "/api/admins/mallory"},
		{method: "DELETE", path: "/api/admins/alice"},
	}
	for _, hdr := range spoofHeaders {
		for _, rt := range adminRoutes {
			rt.remote, rt.hdr = mallory, hdr
			if w := do(h, rt); w.Code != http.StatusForbidden {
				t.Errorf("%s %s with %v from mallory: got %d, want 403 (body %s)",
					rt.method, rt.path, hdr, w.Code, w.Body.String())
			}
		}
		// /api/me must agree: mallory is not admin, not local, and is still mallory.
		w := do(h, call{method: "GET", path: "/api/me", remote: mallory, hdr: hdr})
		var me struct {
			UserName      string `json:"user_name"`
			IsAdmin       bool   `json:"is_admin"`
			IsLocalAccess bool   `json:"is_local_access"`
		}
		json.Unmarshal(w.Body.Bytes(), &me)
		if me.UserName != "mallory" || me.IsAdmin || me.IsLocalAccess {
			t.Errorf("/api/me with %v from mallory = %+v, want mallory/non-admin/non-local", hdr, me)
		}
	}
	if n := hs.preauthKeysMinted.Load(); n != 0 {
		t.Errorf("preauth keys minted by spoofed requests: %d", n)
	}
	if n := hs.usersCreated.Load(); n != 0 {
		t.Errorf("users created by spoofed requests: %d", n)
	}
}

func TestTailnetAdminStillWorks(t *testing.T) {
	h, hs := newPortal(t, identity.Tailnet)
	w := do(h, call{method: "POST", path: "/api/keys", body: `{"user":"alice"}`, remote: aliceIP + ":1"})
	if w.Code != http.StatusCreated {
		t.Fatalf("admin alice POST /api/keys: got %d %s", w.Code, w.Body.String())
	}
	if hs.preauthKeysMinted.Load() != 1 {
		t.Fatal("expected one key minted")
	}
	// IPv6 tailnet address resolves to the same user.
	if w := do(h, call{method: "GET", path: "/api/users", remote: "[fd7a:115c:a1e0::2]:1"}); w.Code != http.StatusOK {
		t.Fatalf("admin alice over IPv6: got %d", w.Code)
	}
}

func TestTailnetListenerNeverGrantsLocalAdmin(t *testing.T) {
	h, _ := newPortal(t, identity.Tailnet)
	// Genuine loopback/private peer addresses on the tsnet listener (e.g. via a
	// subnet router without SNAT) must not be treated as local admin.
	for _, remote := range []string{"127.0.0.1:1", "192.168.1.10:1", "10.0.0.2:1", "172.17.0.1:1"} {
		if w := do(h, call{method: "GET", path: "/api/users", remote: remote}); w.Code != http.StatusForbidden {
			t.Errorf("tailnet listener, peer %s: got %d, want 403", remote, w.Code)
		}
	}
}

func TestLANListener(t *testing.T) {
	h, _ := newPortal(t, identity.LAN)
	// Local callers are admins (first-run setup).
	for _, remote := range []string{"127.0.0.1:1", "[::1]:1", "192.168.1.10:1", "172.17.0.1:1"} {
		if w := do(h, call{method: "GET", path: "/api/users", remote: remote}); w.Code != http.StatusOK {
			t.Errorf("LAN listener, local peer %s: got %d, want 200", remote, w.Code)
		}
	}
	// Non-local callers cannot become local by header.
	for _, hdr := range spoofHeaders {
		if w := do(h, call{method: "GET", path: "/api/users", remote: "203.0.113.7:1", hdr: hdr}); w.Code != http.StatusForbidden {
			t.Errorf("LAN listener, public peer with %v: got %d, want 403", hdr, w.Code)
		}
	}
	// A tailnet source address on the LAN listener did not come through our
	// tsnet node, so it must not be mapped to a headscale user (even alice).
	if w := do(h, call{method: "GET", path: "/api/users", remote: aliceIP + ":1"}); w.Code != http.StatusForbidden {
		t.Errorf("LAN listener, tailnet-range peer: got %d, want 403", w.Code)
	}
}

func upstreamHeaders(t *testing.T, h http.Handler, remote string, hdr map[string]string) map[string]string {
	t.Helper()
	w := do(h, call{method: "GET", path: "/app", remote: remote, hdr: hdr})
	if w.Code != http.StatusOK {
		t.Fatalf("GET /app: %d %s", w.Code, w.Body.String())
	}
	got := map[string]string{}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestUpstreamIdentityCannotBeSpoofed(t *testing.T) {
	h, _ := newPortal(t, identity.Tailnet)
	forged := map[string]string{
		"X-Forwarded-For":    aliceIP,
		"X-Real-Ip":          aliceIP,
		"Forwarded":          "for=" + aliceIP,
		"X-Tailscale-User":   "alice",
		"X-Tailscale-Login":  "alice",
		"X-Tailscale-Device": "alice-phone",
		"X-Tailscale-IP":     aliceIP,
	}
	got := upstreamHeaders(t, h, malloryIP+":40000", forged)
	want := map[string]string{
		"X-Tailscale-User":   "mallory",
		"X-Tailscale-Login":  "mallory",
		"X-Tailscale-Device": "mallory-laptop",
		"X-Tailscale-Ip":     malloryIP,
		"X-Forwarded-For":    malloryIP, // client value replaced, not appended to
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("upstream saw %s=%q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	for _, k := range []string{"X-Real-Ip", "Forwarded"} {
		if v, ok := got[k]; ok {
			t.Errorf("client header %s=%q leaked to upstream", k, v)
		}
	}
}

func TestUpstreamNoIdentityForNonTailnet(t *testing.T) {
	// LAN listener: even a forged alice IP or a real tailnet-range source must
	// not produce identity headers.
	h, _ := newPortal(t, identity.LAN)
	for _, remote := range []string{"192.168.1.10:1", aliceIP + ":1"} {
		got := upstreamHeaders(t, h, remote, map[string]string{"X-Forwarded-For": aliceIP, "X-Tailscale-User": "alice"})
		for k := range got {
			if strings.HasPrefix(strings.ToLower(k), "x-tailscale") {
				t.Errorf("LAN peer %s: upstream received identity header %s=%q", remote, k, got[k])
			}
		}
	}
}

func TestNoWildcardCORS(t *testing.T) {
	h, _ := newPortal(t, identity.LAN)
	for _, c := range []call{
		{method: "OPTIONS", path: "/api/keys", hdr: map[string]string{"Origin": "https://evil.example", "Access-Control-Request-Method": "POST"}},
		{method: "GET", path: "/api/me", hdr: map[string]string{"Origin": "https://evil.example"}},
	} {
		c.remote = "192.168.1.10:1"
		w := do(h, c)
		if v := w.Header().Get("Access-Control-Allow-Origin"); v != "" {
			t.Errorf("%s %s: Access-Control-Allow-Origin = %q, want none", c.method, c.path, v)
		}
	}
}

func TestCrossSiteMutationsRejected(t *testing.T) {
	h, hs := newPortal(t, identity.LAN)
	lanUser := "192.168.1.10:1" // a local admin whose browser visits evil.example
	crossSite := []map[string]string{
		{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"},
		{"Sec-Fetch-Site": "same-site", "Origin": "http://other.192.168.1.1.nip.io"},
		{"Origin": "https://evil.example"}, // older browser, no Sec-Fetch-Site
	}
	mutations := []call{
		{method: "POST", path: "/api/keys", body: `{"user":"alice"}`},
		{method: "POST", path: "/api/users", body: `{"name":"evil"}`},
		{method: "PUT", path: "/api/admins/mallory"},
		{method: "DELETE", path: "/api/nodes/2"},
	}
	for _, hdr := range crossSite {
		for _, m := range mutations {
			m.remote, m.hdr = lanUser, hdr
			// text/plain is what a no-preflight cross-site form/fetch would send.
			if w := do(h, m); w.Code != http.StatusForbidden {
				t.Errorf("%s %s with %v: got %d, want 403", m.method, m.path, hdr, w.Code)
			}
		}
	}
	if n := hs.preauthKeysMinted.Load(); n != 0 {
		t.Errorf("cross-site requests minted %d preauth keys", n)
	}

	// Same-origin browser requests and non-browser clients still work.
	ok := []map[string]string{
		{"Sec-Fetch-Site": "same-origin", "Origin": "http://100.64.0.1:3001"},
		{"Origin": "http://100.64.0.1:3001"},
		nil, // curl / scripts
	}
	for _, hdr := range ok {
		w := do(h, call{method: "POST", path: "/api/keys", body: `{"user":"alice"}`, remote: lanUser, hdr: hdr})
		if w.Code != http.StatusCreated {
			t.Errorf("same-origin POST /api/keys with %v: got %d %s", hdr, w.Code, w.Body.String())
		}
	}
}

func TestDNSRebindingHostRejected(t *testing.T) {
	h, hs := newPortal(t, identity.LAN)
	// evil.example re-pointed at the portal: browser sends Host: evil.example
	// and considers the request same-origin.
	r := httptest.NewRequest("POST", "http://evil.example:3001/api/keys", strings.NewReader(`{"user":"alice"}`))
	r.RemoteAddr = "192.168.1.10:1"
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Origin", "http://evil.example:3001")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMisdirectedRequest {
		t.Errorf("rebinding Host: got %d, want 421", w.Code)
	}
	if hs.preauthKeysMinted.Load() != 0 {
		t.Error("rebinding request minted a key")
	}
	// Reading is blocked too (GET /api/users would leak the user list).
	r = httptest.NewRequest("GET", "http://evil.example:3001/api/users", nil)
	r.RemoteAddr = "192.168.1.10:1"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMisdirectedRequest {
		t.Errorf("rebinding Host GET: got %d, want 421", w.Code)
	}
}

func TestHostAllowed(t *testing.T) {
	extra := map[string]bool{"headfwd-server.tail.example.com": true}
	allowed := []string{
		"100.64.0.1:3001", "127.0.0.1", "[::1]:3001", "[fd7a:115c:a1e0::1]:3001",
		"localhost:3001", "headfwd-server", "nas.local", "nas.lan", "nas.home.arpa",
		"HEADFWD-SERVER.tail.example.com.", "headfwd-server.tail.example.com:3001",
	}
	denied := []string{"", "evil.example", "evil.example:3001", "100.64.0.1.nip.io", "localhost.evil.example"}
	for _, h := range allowed {
		if !hostAllowed(h, extra) {
			t.Errorf("hostAllowed(%q) = false, want true", h)
		}
	}
	for _, h := range denied {
		if hostAllowed(h, extra) {
			t.Errorf("hostAllowed(%q) = true, want false", h)
		}
	}
}
