# HeadFwd - What We Built

## The Problem You Solved

Headscale instances at home can't accept inbound connections (NAT/firewall). Traditional solutions:
- ❌ Port forwarding (complex, insecure)
- ❌ ngrok ($8/mo per instance)
- ❌ VPN to cloud then to home (slow, expensive)

## The Solution

**Reverse tunnel proxy** - Headscale connects TO proxy, clients connect THROUGH it.

```
Home Headscale ──[WebSocket]──> HeadFwd Proxy <──[HTTP / TS2021]── Clients
```

## What You Have Now

### 1. **headfwd-proxy/** (Go)
Fly.io proxy
- Routes `<fingerprint>.headfwd.net` to the correct Headscale
- Holds one tunnel per fingerprint in memory
- Hijacks Tailscale's TS2021 HTTP `Upgrade` → full control-plane support

The earlier Cloudflare Workers version is parked on the `cloudflare-proxy` branch until Workers can proxy TS2021's custom HTTP Upgrade.

### 2. **headfwd-sidecar/** (Go)
Runs alongside Headscale
- Auto-registers with the proxy (X25519 ECDH + HMAC-SHA256)
- Opens the WebSocket tunnel and forwards client requests to local Headscale
- Embedded admin portal (React) for user management and device onboarding
- Auto-reconnects on disconnect

### 3. **ios/** (SwiftUI)
- Connects via tsnet (embedded, patched `libtailscale`)
- QR onboarding + Noise-key verification and pinning

## Key Decisions Made

✅ **Reverse tunnel** (not direct proxy) - NAT traversal  
✅ **Fly.io proxy** - Supports TS2021; the HTTP-only Cloudflare version is parked on a branch  
✅ **Sidecar pattern** - No Headscale modifications  
✅ **X25519 ECDH + HMAC-SHA256** - Reuses the Noise key; no new secrets  
✅ **Proven reverse-tunnel approach** - Same pattern as ngrok/Cloudflare Tunnel

## Cost

- **Fly.io:** a single shared-cpu-1x instance is plenty for personal use

## What's Next

### Short Term
- [ ] QR code onboarding polish
- [ ] TestFlight beta

### Long Term
- [ ] Production hardening
- [ ] Monitoring/alerting
- [ ] App Store release

## Layout

```
headfwd/
├── ARCHITECTURE.md              System design
├── QUICKSTART.md                Setup guide
├── README.md                    Overview
├── SUMMARY.md                   This file
├── docker-compose.yml           Headscale + sidecar
├── headscale/config/config.yaml Headscale config
├── headfwd-proxy/               Fly.io proxy (Go)
├── headfwd-sidecar/             Go tunnel client + embedded portal
└── ios/                         SwiftUI app (tsnet via libtailscale)
```

## Architecture Highlights

**Reverse Tunnel Pattern:**
1. Sidecar auto-registers, then opens a WebSocket to the proxy
2. Proxy stores the connection keyed by fingerprint
3. Client requests route to the correct tunnel
4. Proxy forwards through the tunnel (including hijacked TS2021 streams)
5. Sidecar proxies to local Headscale
6. Response flows back

**Security:**
- Zero-knowledge proxy (encrypted WireGuard/Noise)
- Registration proven via X25519 ECDH + HMAC-SHA256
- Clients verify the Noise key against the fingerprint and pin it
- End-to-end encryption maintained

## Comparison to Alternatives

| Feature | HeadFwd | ngrok | Cloudflare Tunnel |
|---------|---------|-------|-------------------|
| NAT traversal | ✅ | ✅ | ✅ |
| Setup | QR code | Easy | Medium |
| Cost (100 users) | **$0** | $800/mo | $0 |
| Self-hosted control | ✅ | ❌ | ❌ |
| Open source | ✅ | ❌ | ❌ |
| iOS app | ✅ | ❌ | ❌ |

## The Innovation

**HeadFwd = ngrok simplicity + self-hosted control + zero-knowledge relay**

You're building a **decentralized alternative** to centralized remote access, where:
- Users own their keys
- Users run their Headscale
- Proxy is zero-knowledge
- Cost is minimal
- Setup is simple (QR code)

## Ready to Test?

See `QUICKSTART.md` for step-by-step deployment.

Estimated time: **15 minutes** from zero to working tunnel.

