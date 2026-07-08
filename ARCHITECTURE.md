# HeadFwd Architecture

## Core Concept

**Reverse tunnel proxy** - Headscale instances (behind NAT) connect TO the proxy, clients connect THROUGH it.

```
Headscale (home) ──[WebSocket]──> HeadFwd Proxy <──[HTTP / TS2021]── Clients
     (initiates)                  (holds tunnel)                  (requests)
```

The proxy is a stateless relay keyed by the fingerprint subdomain. Two interchangeable backends exist:

- **Fly.io** (`headfwd-proxy-fly/`, Go) — **recommended**. A single process that holds tunnels in memory and can hijack Tailscale's custom TS2021 HTTP `Upgrade`, so it carries the full control plane.
- **Cloudflare Workers** (`headfwd-proxy/`, TypeScript) — HTTP-only. Uses one Durable Object per Headscale instance to hold the tunnel; cannot proxy the TS2021 upgrade.

## Why This Design?

Most Headscale instances run at home behind NAT/firewalls. They can't accept inbound connections. Solution: reverse tunnel.

1. Headscale opens persistent WebSocket to proxy
2. Clients make HTTP requests to proxy
3. Proxy forwards through the tunnel
4. Headscale responds back through tunnel

**This is exactly like ngrok, Cloudflare Tunnel, or headfwd-agent.**

## Components

### 1. Headscale Sidecar (Go)
Runs alongside Headscale, initiates tunnel:

```go
ws.Dial("wss://abc123.headfwd.net/tunnel?auth=secret")
for {
    req := ws.ReadMessage()  // Wait for client requests
    resp := forwardToLocalHeadscale(req)
    ws.WriteMessage(resp)
}
```

### 2. Proxy (Fly.io, Go — recommended)
A single process that keeps a tunnel per fingerprint and forwards both plain HTTP and hijacked TS2021 upgrade streams:

```go
// Registration + tunnels held in memory, keyed by fingerprint
s.tunnels[fingerprint] = &tunnelConn{ws: conn}

// Client request → serialize → send over the tunnel WebSocket → await response
tunnel.send(TunnelMessage{Type: "request", Data: mustMarshal(req)})

// TS2021 Upgrade: hijack the client conn and relay raw bytes both ways
conn, _, _ := hj.Hijack()
```

### 3. Proxy (Cloudflare Workers — HTTP-only alternative)
Edge Worker routes by fingerprint to one Durable Object per Headscale instance, which holds the tunnel WebSocket:

```typescript
const fingerprint = extractFromSubdomain(url);
const doId = env.TUNNELS.idFromName(fingerprint);
return doStub.fetch(request); // DO holds the persistent tunnel
```

## Data Flow

```
1. Headscale starts sidecar
   └─> Sidecar auto-registers (X25519 ECDH + HMAC-SHA256)
   └─> Opens WebSocket to abc123.headfwd.net/tunnel?auth=secret
   └─> Proxy stores this connection keyed by fingerprint

2. iOS client requests
   └─> GET https://abc123.headfwd.net/key?v=96  (or a TS2021 Upgrade)
   └─> Proxy routes by fingerprint to the matching tunnel
   └─> Forwards through the tunnel WebSocket
   └─> Sidecar receives, forwards to local Headscale
   └─> Response flows back

3. Tunnel stays open (Cloudflare DOs hibernate when idle; Fly.io holds it in-process)
```

## Cost

- **Fly.io:** a single shared-cpu-1x instance is plenty for personal use
- **Cloudflare Workers:** free tier covers 100+ instances; hibernating WebSockets mean you only pay when a tunnel is active

## Security

- Zero-knowledge proxy (encrypted WireGuard traffic)
- Headscale validates all keys
- Sidecar authenticates tunnel with secret
- End-to-end encryption maintained

## vs Alternatives

| Solution | Setup | NAT | Cost |
|----------|-------|-----|------|
| **HeadFwd** | QR code | ✅ Works | Free-$10/mo |
| Port forward | Complex | ❌ Requires config | $0 |
| ngrok | Easy | ✅ Works | $8/mo per instance |
| Cloudflare Tunnel | Medium | ✅ Works | Free (but locked in) |

HeadFwd = ngrok simplicity + self-hosted control + Cloudflare scale

