package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testFingerprint = "7bbbc274d319025be9ba9ab25ddc4fd7"

// newTunnelTestServer serves /tunnel with one fingerprint already registered,
// which is all handleTunnel needs to authorise a dial.
//
// Keepalive timings are applied here, before any tunnel exists, so nothing
// reads them concurrently with the write.
func newTunnelTestServer(t *testing.T, ping, read time.Duration) (*Server, *httptest.Server, string) {
	t.Helper()

	s := newServer()
	s.tunnelPingInterval, s.tunnelReadTimeout = ping, read

	const secret = "test-secret"
	s.mu.Lock()
	s.registrations[testFingerprint] = Registration{
		PublicKey: "mkey:test",
		Secret:    secret,
		CreatedAt: time.Now(),
	}
	s.mu.Unlock()

	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel", s.handleTunnel)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return s, ts, secret
}

// dialTunnel connects as a sidecar would. The Host header carries the
// fingerprint subdomain, which is how the relay routes.
func dialTunnel(t *testing.T, ts *httptest.Server, secret string) *websocket.Conn {
	t.Helper()

	url := "ws" + strings.TrimPrefix(ts.URL, "http") + "/tunnel?auth=" + secret
	conn, resp, err := websocket.DefaultDialer.Dial(url, http.Header{
		"Host": []string{testFingerprint + ".example.test"},
	})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial tunnel: %v (HTTP %d)", err, status)
	}
	return conn
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestReconnectKeepsLiveTunnelRegistered covers the outage of 2026-09-02: a
// sidecar whose connection died in a way the relay did not notice reconnected,
// and the old connection's cleanup then retracted the *replacement* from the
// tunnel map. The surviving websocket kept answering pings, so the sidecar
// believed it was healthy while every client request got a 503.
func TestReconnectKeepsLiveTunnelRegistered(t *testing.T) {
	s, ts, secret := newTunnelTestServer(t, defaultTunnelPingInterval, defaultTunnelReadTimeout)

	first := dialTunnel(t, ts, secret)
	defer first.Close()
	waitFor(t, "first tunnel to register", func() bool {
		return s.getTunnel(testFingerprint) != nil
	})
	stale := s.getTunnel(testFingerprint)

	// Reconnecting evicts the old connection and installs this one.
	second := dialTunnel(t, ts, secret)
	defer second.Close()
	waitFor(t, "replacement to be installed", func() bool {
		cur := s.getTunnel(testFingerprint)
		return cur != nil && cur != stale
	})
	live := s.getTunnel(testFingerprint)

	// Wait until the evicted connection is genuinely torn down, so its cleanup
	// has run. Without this the test could pass while the race was still armed.
	_ = first.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		if _, _, err := first.ReadMessage(); err != nil {
			break
		}
	}

	// The replacement must still be the registered tunnel. Polling rather than
	// checking once, because the damage was asynchronous.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		switch got := s.getTunnel(testFingerprint); got {
		case live:
		case nil:
			t.Fatal("live tunnel was retracted by the evicted connection's cleanup; " +
				"clients would now get 503 while the sidecar sees a healthy tunnel")
		default:
			t.Fatalf("tunnel map points at an unexpected connection: got %p, want %p", got, live)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestUnresponsivePeerIsReaped covers the condition that armed the race: the
// relay set no read deadline, so a half-dead connection could sit in the map
// indefinitely. A gorilla client only answers pings while it is reading, so a
// client that never reads stands in for a peer that has silently gone away.
func TestUnresponsivePeerIsReaped(t *testing.T) {
	s, ts, secret := newTunnelTestServer(t, 20*time.Millisecond, 250*time.Millisecond)

	conn := dialTunnel(t, ts, secret)
	defer conn.Close()
	waitFor(t, "tunnel to register", func() bool {
		return s.getTunnel(testFingerprint) != nil
	})

	// Deliberately never read from conn, so no pongs are sent.
	waitFor(t, "unresponsive tunnel to be reaped", func() bool {
		return s.getTunnel(testFingerprint) == nil
	})
}

// TestResponsivePeerSurvives guards against the reaper being too aggressive:
// a client that reads (and so auto-answers pings) must stay registered well
// past the read timeout.
func TestResponsivePeerSurvives(t *testing.T) {
	const readTimeout = 250 * time.Millisecond
	s, ts, secret := newTunnelTestServer(t, 20*time.Millisecond, readTimeout)

	conn := dialTunnel(t, ts, secret)
	defer conn.Close()
	waitFor(t, "tunnel to register", func() bool {
		return s.getTunnel(testFingerprint) != nil
	})

	// Reading is what lets gorilla answer the relay's pings.
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	deadline := time.Now().Add(4 * readTimeout)
	for time.Now().Before(deadline) {
		if s.getTunnel(testFingerprint) == nil {
			t.Fatal("a responsive tunnel was reaped; the read deadline is not being extended by pongs")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
