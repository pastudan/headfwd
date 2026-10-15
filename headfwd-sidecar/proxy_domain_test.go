package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testFP = "0123456789abcdef0123456789abcdef"

func TestResolveProxyDomain(t *testing.T) {
	ok := []struct {
		name, proxyURL, override, want string
	}{
		{"default", defaultProxyURL, "", "headfwd.net"},
		{"custom host", "https://example.com", "", "example.com"},
		{"case port and path stripped", "https://Relay.Example.COM:8443/", "", "relay.example.com"},
		{"override wins", "https://api.example.com", "example.com", "example.com"},
		{"override over IP proxy", "http://127.0.0.1:8080", "example.com", "example.com"},
		{"override trailing dot", defaultProxyURL, "Example.com.", "example.com"},
	}
	for _, tc := range ok {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveProxyDomain(tc.proxyURL, tc.override)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}

	bad := []struct {
		name, proxyURL, override string
	}{
		{"IPv4 proxy", "https://127.0.0.1", ""},
		{"IPv6 proxy", "https://[::1]:8080", ""},
		{"no host", "https://", ""},
		{"not a URL", "example.com", ""},
		{"userinfo in proxy", "https://user@example.com", ""},
		{"override with scheme", defaultProxyURL, "https://example.com"},
		{"override with path", defaultProxyURL, "example.com/x"},
		{"override with port", defaultProxyURL, "example.com:443"},
		{"override wildcard", defaultProxyURL, "*.example.com"},
		{"override leading hyphen", defaultProxyURL, "-bad.com"},
		{"override userinfo", defaultProxyURL, "a@b.com"},
		{"override empty label", defaultProxyURL, "example..com"},
		{"override IP", defaultProxyURL, "10.0.0.1"},
	}
	for _, tc := range bad {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			if got, err := resolveProxyDomain(tc.proxyURL, tc.override); err == nil {
				t.Fatalf("expected error, got %q", got)
			}
		})
	}
}

func TestPublicURLFromFingerprint(t *testing.T) {
	if got := publicURLFromFingerprint(testFP, "example.com"); got != "https://"+testFP+".example.com" {
		t.Fatalf("got %q", got)
	}
	if got := publicURLFromFingerprint(testFP, "headfwd.net"); got != "https://"+testFP+".headfwd.net" {
		t.Fatalf("got %q", got)
	}
	if got := publicURLFromFingerprint("", "example.com"); got != "" {
		t.Fatalf("empty fingerprint should give empty URL, got %q", got)
	}
}

func TestVerifyRegistrationURLsCustomDomain(t *testing.T) {
	const domain = "example.com"
	good := RegistrationVerifyResponse{
		Fingerprint: testFP,
		TunnelURL:   "wss://" + testFP + ".example.com/tunnel?auth=secret",
		PublicURL:   "https://" + testFP + ".example.com",
	}
	if err := verifyRegistrationURLs(testFP, domain, good); err != nil {
		t.Fatalf("valid custom-domain response rejected: %v", err)
	}

	upper := good
	upper.PublicURL = "https://" + strings.ToUpper(testFP) + ".EXAMPLE.COM/"
	upper.TunnelURL = "wss://" + testFP + ".Example.Com/tunnel?auth=secret"
	if err := verifyRegistrationURLs(testFP, domain, upper); err != nil {
		t.Fatalf("case-insensitive host rejected: %v", err)
	}

	other := "fedcba9876543210fedcba9876543210"
	bad := []struct {
		name              string
		fp, tunnel, pub   string
		wantErrSubstrings []string
	}{
		{"proxy tries to switch to headfwd.net", testFP,
			"wss://" + testFP + ".headfwd.net/tunnel?auth=s", "https://" + testFP + ".headfwd.net",
			[]string{`"headfwd.net"`, `expects "example.com"`, "PUBLIC_HOST=example.com"}},
		{"attacker domain", testFP,
			"wss://" + testFP + ".attacker.net/tunnel?auth=s", "https://" + testFP + ".attacker.net", nil},
		{"suffix appended", testFP,
			"wss://" + testFP + ".example.com.evil.net/tunnel?auth=s", "https://" + testFP + ".example.com.evil.net", nil},
		{"extra leading label", testFP,
			"wss://x." + testFP + ".example.com/tunnel?auth=s", "https://x." + testFP + ".example.com", nil},
		{"fingerprint prefixed", testFP,
			"wss://evil" + testFP + ".example.com/tunnel?auth=s", "https://evil" + testFP + ".example.com", nil},
		{"different fingerprint in URLs", testFP,
			"wss://" + other + ".example.com/tunnel?auth=s", "https://" + other + ".example.com", nil},
		{"ws scheme", testFP,
			"ws://" + testFP + ".example.com/tunnel?auth=s", "https://" + testFP + ".example.com", nil},
		{"http public scheme", testFP,
			"wss://" + testFP + ".example.com/tunnel?auth=s", "http://" + testFP + ".example.com", nil},
		{"explicit port", testFP,
			"wss://" + testFP + ".example.com:8443/tunnel?auth=s", "https://" + testFP + ".example.com", nil},
		{"userinfo trick", testFP,
			"wss://" + testFP + ".example.com@evil.net/tunnel?auth=s", "https://" + testFP + ".example.com", nil},
		{"public URL host differs from tunnel", testFP,
			"wss://" + testFP + ".example.com/tunnel?auth=s", "https://" + other + ".example.com", nil},
		{"public URL with path", testFP,
			"wss://" + testFP + ".example.com/tunnel?auth=s", "https://" + testFP + ".example.com/phish", nil},
		{"response fingerprint mismatch", other,
			"wss://" + testFP + ".example.com/tunnel?auth=s", "https://" + testFP + ".example.com", nil},
		{"empty URLs", testFP, "", "", nil},
	}
	for _, tc := range bad {
		t.Run("rejects "+tc.name, func(t *testing.T) {
			err := verifyRegistrationURLs(testFP, domain, RegistrationVerifyResponse{
				Fingerprint: tc.fp, TunnelURL: tc.tunnel, PublicURL: tc.pub,
			})
			if err == nil {
				t.Fatal("expected rejection, got nil")
			}
			for _, s := range tc.wantErrSubstrings {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("error %q should mention %q", err, s)
				}
			}
		})
	}
}

func TestVerifyRegistrationURLsDefaultDomain(t *testing.T) {
	resp := RegistrationVerifyResponse{
		Fingerprint: testFP,
		TunnelURL:   "wss://" + testFP + ".headfwd.net/tunnel?auth=secret",
		PublicURL:   "https://" + testFP + ".headfwd.net",
	}
	if err := verifyRegistrationURLs(testFP, "headfwd.net", resp); err != nil {
		t.Fatalf("default domain rejected: %v", err)
	}
	// A sidecar configured for headfwd.net must not follow a proxy onto
	// another domain either.
	resp.TunnelURL = "wss://" + testFP + ".example.com/tunnel?auth=secret"
	resp.PublicURL = "https://" + testFP + ".example.com"
	if err := verifyRegistrationURLs(testFP, "headfwd.net", resp); err == nil {
		t.Fatal("headfwd.net sidecar accepted example.com URLs")
	}
}

func TestIsHeadfwdServerURL(t *testing.T) {
	cases := map[string]bool{
		"https://" + testFP + ".headfwd.net":  true,
		"https://" + testFP + ".example.com":  true,
		"https://" + testFP + ".example.com/": true,
		"https://vpn.mycorp.com":              false,
		"https://" + testFP:                   false,
		"http://headscale:8080":               false,
		"":                                    false,
		"not a url":                           false,
	}
	for in, want := range cases {
		if got := isHeadfwdServerURL(in); got != want {
			t.Errorf("isHeadfwdServerURL(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestUpdateServerURLMovesBetweenDomains(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	write := func(s string) {
		if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Previously registered on headfwd.net; re-registering on a self-hosted
	// domain rewrites server_url without --force-update.
	write("server_url: https://" + testFP + ".headfwd.net\nlisten_addr: 0.0.0.0:8080\n")
	updated, err := updateServerURL(path, "https://"+testFP+".example.com", false)
	if err != nil || !updated {
		t.Fatalf("expected update, got updated=%v err=%v", updated, err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "server_url: https://"+testFP+".example.com\n") {
		t.Fatalf("server_url not rewritten:\n%s", data)
	}

	// Already correct: no-op, no error.
	updated, err = updateServerURL(path, "https://"+testFP+".example.com", false)
	if err != nil || updated {
		t.Fatalf("expected no-op, got updated=%v err=%v", updated, err)
	}

	// Operator's own server_url is left alone without --force-update.
	write("server_url: https://vpn.mycorp.com\n")
	if _, err := updateServerURL(path, "https://"+testFP+".example.com", false); err == nil {
		t.Fatal("custom server_url was overwritten without force")
	}
}

// fakeStack starts a fake Headscale (/key?v=96) and a fake relay implementing
// the v2 registration protocol. issueDomain is the domain the relay puts in
// the URLs it returns — standing in for its PUBLIC_HOST.
func fakeStack(t *testing.T, issueDomain string) (headscaleURL, proxyURL, keyPath, fp string) {
	t.Helper()
	curve := ecdh.X25519()

	noisePriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyPath = filepath.Join(t.TempDir(), "noise_private.key")
	if err := os.WriteFile(keyPath, []byte("privkey:"+hex.EncodeToString(noisePriv.Bytes())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pubKey := "mkey:" + hex.EncodeToString(noisePriv.PublicKey().Bytes())
	fp = computeFingerprint(pubKey)

	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/key" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(HeadscaleKeyResponse{PublicKey: pubKey})
	}))
	t.Cleanup(hs.Close)

	ephem, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ephemHex := hex.EncodeToString(ephem.PublicKey().Bytes())
	const nonce = "test-nonce"

	mux := http.NewServeMux()
	mux.HandleFunc("/api/register/init", func(w http.ResponseWriter, r *http.Request) {
		var req RegistrationInitRequest
		json.NewDecoder(r.Body).Decode(&req)
		json.NewEncoder(w).Encode(RegistrationInitResponse{
			Fingerprint:     computeFingerprint(req.PublicKey),
			Nonce:           nonce,
			ProxyPublicKey:  ephemHex,
			ExpiresAt:       0,
			ProtocolVersion: ProtocolVersion,
		})
	})
	mux.HandleFunc("/api/register/verify", func(w http.ResponseWriter, r *http.Request) {
		var req RegistrationVerifyRequest
		json.NewDecoder(r.Body).Decode(&req)
		// Check the proof the way the real relay does.
		sidecarPub, _ := hex.DecodeString(strings.TrimPrefix(pubKey, "mkey:"))
		pk, err := curve.NewPublicKey(sidecarPub)
		if err != nil {
			http.Error(w, "bad key", http.StatusBadRequest)
			return
		}
		shared, err := ephem.ECDH(pk)
		if err != nil {
			http.Error(w, "ecdh", http.StatusBadRequest)
			return
		}
		want, _ := registrationProof(shared, req.Fingerprint, pubKey, nonce, ephemHex)
		if req.Proof != want {
			http.Error(w, "invalid proof", http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(RegistrationVerifyResponse{
			Fingerprint: req.Fingerprint,
			TunnelURL:   "wss://" + req.Fingerprint + "." + issueDomain + "/tunnel?auth=secret",
			PublicURL:   "https://" + req.Fingerprint + "." + issueDomain,
		})
	})
	px := httptest.NewServer(mux)
	t.Cleanup(px.Close)

	return hs.URL, px.URL, keyPath, fp
}

func TestRegisterWithProxyCustomDomain(t *testing.T) {
	t.Run("honest self-hosted proxy is accepted", func(t *testing.T) {
		hs, px, key, fp := fakeStack(t, "example.com")
		// The relay is reached at http://127.0.0.1:<port>; the public domain is
		// configured independently, exactly as --proxy-domain allows.
		domain, err := resolveProxyDomain(px, "example.com")
		if err != nil {
			t.Fatal(err)
		}
		res, err := registerWithProxy(px, domain, hs, key)
		if err != nil {
			t.Fatalf("registration failed: %v", err)
		}
		if res.Fingerprint != fp {
			t.Fatalf("fingerprint %q, want %q", res.Fingerprint, fp)
		}
		if res.PublicURL != "https://"+fp+".example.com" {
			t.Fatalf("public URL %q not derived locally", res.PublicURL)
		}
		if res.TunnelURL != "wss://"+fp+".example.com/tunnel?auth=secret" {
			t.Fatalf("tunnel URL %q", res.TunnelURL)
		}
	})

	t.Run("proxy returning another domain is rejected", func(t *testing.T) {
		hs, px, key, _ := fakeStack(t, "attacker.net")
		_, err := registerWithProxy(px, "example.com", hs, key)
		if err == nil {
			t.Fatal("sidecar accepted URLs on a domain it was not configured for")
		}
		if !strings.Contains(err.Error(), `"attacker.net"`) || !strings.Contains(err.Error(), `expects "example.com"`) {
			t.Fatalf("unhelpful error: %v", err)
		}
	})

	t.Run("proxy returning headfwd.net to a self-hosted sidecar is rejected", func(t *testing.T) {
		hs, px, key, _ := fakeStack(t, "headfwd.net")
		if _, err := registerWithProxy(px, "example.com", hs, key); err == nil {
			t.Fatal("sidecar followed the proxy back onto headfwd.net")
		}
	})
}
