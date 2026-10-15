// Package identity decides who a portal caller is.
//
// The only input it trusts is the TCP peer address (http.Request.RemoteAddr).
// Forwarding headers such as X-Forwarded-For, X-Real-Ip and Forwarded are
// written by the client and are never consulted: nothing legitimate sits in
// front of the portal, so any such header is attacker-controlled.
//
// What an address means depends on which listener accepted the connection:
//
//   - Tailnet (the tsnet listener): the peer address is a WireGuard-authenticated
//     tailnet IP and can be mapped to a headscale node and user. Loopback and
//     private addresses never confer "local admin" here, even if one shows up
//     (e.g. via a subnet router that does not SNAT).
//   - LAN (the plain TCP listener): loopback and RFC 1918 callers are treated as
//     local admins (first-run setup). Tailnet identity is never derived on this
//     listener, because a 100.x source there did not come through our tsnet node
//     and may belong to an unrelated tailnet.
package identity

import (
	"net"
	"net/http"
	"net/netip"

	"github.com/headfwd/sidecar/portal/headscale"
)

// Listener identifies which listener a portal handler is serving.
// The zero value is Tailnet, the more restrictive of the two.
type Listener int

const (
	// Tailnet is the tsnet listener: identity comes from headscale node lookup.
	Tailnet Listener = iota
	// LAN is the plain TCP listener: loopback/RFC 1918 callers are local admins.
	LAN
)

func (l Listener) String() string {
	if l == LAN {
		return "lan"
	}
	return "tailnet"
}

var (
	tailnetV4 = netip.MustParsePrefix("100.64.0.0/10")       // CGNAT range used by Tailscale/headscale
	tailnetV6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48") // Tailscale ULA range
	privateV4 = []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
	}
)

// RemoteIP returns the TCP peer address of r. It deliberately ignores every
// forwarding header. The zero Addr is returned if RemoteAddr is unparseable.
func RemoteIP(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap().WithZone("")
}

// IsTailnetIP reports whether addr is in the Tailscale/headscale address ranges.
func IsTailnetIP(addr netip.Addr) bool {
	addr = addr.Unmap()
	return tailnetV4.Contains(addr) || tailnetV6.Contains(addr)
}

// IsLocalIP reports whether addr is loopback or RFC 1918 private IPv4.
// IPv6 ULAs are intentionally excluded: fd7a:115c:a1e0::/48 (tailnet) is a ULA.
func IsLocalIP(addr netip.Addr) bool {
	addr = addr.Unmap()
	if addr.IsLoopback() {
		return true
	}
	for _, p := range privateV4 {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// Caller is the result of classifying a request.
type Caller struct {
	IP netip.Addr
	// Local is true only for loopback/RFC 1918 callers on the LAN listener.
	Local bool
	// Tailnet is true only for tailnet-range callers on the tailnet listener.
	// Only such callers may be mapped to a headscale user.
	Tailnet bool
}

// Classify derives the caller from r's peer address and the listener that
// accepted it.
func (l Listener) Classify(r *http.Request) Caller {
	ip := RemoteIP(r)
	return Caller{
		IP:      ip,
		Local:   l == LAN && IsLocalIP(ip),
		Tailnet: l == Tailnet && IsTailnetIP(ip),
	}
}

// FindNode returns the headscale node that owns ip, or nil.
func FindNode(nodes []headscale.Node, ip netip.Addr) *headscale.Node {
	if !ip.IsValid() {
		return nil
	}
	for i := range nodes {
		for _, s := range nodes[i].IPAddresses {
			if a, err := netip.ParseAddr(s); err == nil && a.Unmap() == ip {
				return &nodes[i]
			}
		}
	}
	return nil
}
