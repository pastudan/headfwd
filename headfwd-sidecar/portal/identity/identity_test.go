package identity

import (
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/headfwd/sidecar/portal/headscale"
)

func TestRemoteIPIgnoresForwardingHeaders(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "100.64.0.3:51234"
	r.Header.Set("X-Forwarded-For", "127.0.0.1")
	r.Header.Set("X-Real-Ip", "10.0.0.5")
	r.Header.Set("Forwarded", "for=127.0.0.1")

	if got := RemoteIP(r); got != netip.MustParseAddr("100.64.0.3") {
		t.Fatalf("RemoteIP = %v, want 100.64.0.3 (headers must be ignored)", got)
	}
}

func TestRemoteIPForms(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:1":           "127.0.0.1",
		"[::1]:1":               "::1",
		"[::ffff:10.0.0.1]:1":   "10.0.0.1", // v4-mapped is unmapped
		"[fe80::1%en0]:1":       "fe80::1",  // zone dropped
		"[fd7a:115c:a1e0::2]:1": "fd7a:115c:a1e0::2",
		"garbage":               "invalid IP",
	}
	for in, want := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = in
		if got := RemoteIP(r).String(); got != want {
			t.Errorf("RemoteIP(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsLocalIP(t *testing.T) {
	local := []string{"127.0.0.1", "127.9.9.9", "::1", "10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.1.1"}
	notLocal := []string{"172.15.0.1", "172.32.0.1", "100.64.0.1", "8.8.8.8", "fd7a:115c:a1e0::1", "fd00::1", "169.254.1.1"}
	for _, s := range local {
		if !IsLocalIP(netip.MustParseAddr(s)) {
			t.Errorf("IsLocalIP(%s) = false, want true", s)
		}
	}
	for _, s := range notLocal {
		if IsLocalIP(netip.MustParseAddr(s)) {
			t.Errorf("IsLocalIP(%s) = true, want false", s)
		}
	}
	if IsLocalIP(netip.Addr{}) {
		t.Error("IsLocalIP(invalid) = true")
	}
}

func TestIsTailnetIP(t *testing.T) {
	yes := []string{"100.64.0.1", "100.127.255.254", "fd7a:115c:a1e0::5"}
	no := []string{"100.63.255.255", "100.128.0.0", "10.0.0.1", "fd7a:115c:a1e1::1", "127.0.0.1"}
	for _, s := range yes {
		if !IsTailnetIP(netip.MustParseAddr(s)) {
			t.Errorf("IsTailnetIP(%s) = false", s)
		}
	}
	for _, s := range no {
		if IsTailnetIP(netip.MustParseAddr(s)) {
			t.Errorf("IsTailnetIP(%s) = true", s)
		}
	}
}

func TestClassifyPerListener(t *testing.T) {
	cases := []struct {
		ln          Listener
		remote      string
		local, tnet bool
	}{
		// LAN listener: private/loopback are local; tailnet IPs get no identity.
		{LAN, "127.0.0.1:1", true, false},
		{LAN, "172.17.0.1:1", true, false}, // docker bridge gateway
		{LAN, "192.168.1.20:1", true, false},
		{LAN, "100.64.0.2:1", false, false},
		{LAN, "8.8.8.8:1", false, false},
		// Tailnet listener: never local, even for private sources.
		{Tailnet, "100.64.0.2:1", false, true},
		{Tailnet, "[fd7a:115c:a1e0::2]:1", false, true},
		{Tailnet, "127.0.0.1:1", false, false},
		{Tailnet, "192.168.1.20:1", false, false}, // e.g. via non-SNAT subnet router
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		r.Header.Set("X-Forwarded-For", "127.0.0.1")
		got := c.ln.Classify(r)
		if got.Local != c.local || got.Tailnet != c.tnet {
			t.Errorf("%v.Classify(%s) = local:%v tailnet:%v, want local:%v tailnet:%v",
				c.ln, c.remote, got.Local, got.Tailnet, c.local, c.tnet)
		}
	}
}

func TestFindNode(t *testing.T) {
	nodes := []headscale.Node{
		{GivenName: "a", IPAddresses: []string{"100.64.0.2", "fd7a:115c:a1e0::2"}},
		{GivenName: "b", IPAddresses: []string{"100.64.0.3"}},
	}
	if n := FindNode(nodes, netip.MustParseAddr("fd7a:115c:a1e0::2")); n == nil || n.GivenName != "a" {
		t.Errorf("FindNode v6 = %v", n)
	}
	if n := FindNode(nodes, netip.MustParseAddr("100.64.0.3")); n == nil || n.GivenName != "b" {
		t.Errorf("FindNode v4 = %v", n)
	}
	if n := FindNode(nodes, netip.MustParseAddr("100.64.0.9")); n != nil {
		t.Errorf("FindNode unknown = %v", n)
	}
	if n := FindNode(nodes, netip.Addr{}); n != nil {
		t.Errorf("FindNode invalid = %v", n)
	}
}
