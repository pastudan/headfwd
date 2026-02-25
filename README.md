# HeadFwd

Zero-knowledge remote access. You own the keys, you own the network.

## What Is This?

HeadFwd lets you access your self-hosted apps from anywhere using Headscale (self-hosted Tailscale), even if your server is behind NAT/firewall.

**Problem:** Headscale at home → can't accept inbound connections. Maintaining DDNS and port forwarding sucks.
**Solution:** Zero-knowledge reverse tunnel (like ngrok, but the proxy can't see your keys or data)

You now have the best of both worlds; a secure, local control plane AND remote access.

## Architecture

```
┌─────────────────┐         Persistent WebSocket            ┌──────────────────┐
│   Headscale     │════════════════════════════════════════>│  Cloudflare      │
│   (at home,     │  1. Tunnel opens on startup             │  Durable Object  │
│    behind NAT)  │                                         │                  │
└─────────────────┘                                         └──────────────────┘
                                                                     ▲
                                                        2. Client    │
                                                           requests  │
                                                                     ▼
                                                            ┌─────────────────┐
                                                            │  Client (iOS)   │
                                                            │  HTTP requests  │
                                                            └─────────────────┘
```

1. Headscale sidecar opens persistent WebSocket to proxy
2. Clients make HTTP requests to `https://<fingerprint>.headfwd.net`
3. Proxy forwards through tunnel to Headscale
4. WireGuard mesh established, then direct P2P

## Components

- **`headfwd-proxy/`** - Cloudflare Workers + Durable Objects proxy
- **`headfwd-sidecar/`** - Go sidecar that runs with Headscale + optional admin portal (embedded React UI for user management, device onboarding, connectivity proof)
- **`docs/REGISTRATION.md`** - Secure registration architecture (Challenge-Response auth)
- **`docs/ios-poc-plan.md`** - iOS app PoC plan (SwiftUI + XcodeGen CLI build)
- **`tailscale-ios-integration-plan.md`** - iOS app integration guide (tailscaled compilation)

## Quick Start

### 1. Deploy Proxy

**Option A: Fly.io (recommended for TS2021)**

```bash
cd headfwd-proxy-fly
fly launch --no-deploy
fly secrets set PUBLIC_HOST=headfwd.net
fly deploy
```

**Option B: Cloudflare Workers (HTTP-only)**

```bash
cd headfwd-proxy
npx wrangler login
npx wrangler kv namespace create REGISTRY
# Update wrangler.jsonc with KV ID
npm run deploy
```

> **Note:** Tailscale TS2021 uses a custom HTTP Upgrade that Cloudflare Workers cannot proxy. Use Fly.io for full control-plane support.

### 2. Run Headscale + Sidecar

**Option A: Auto-Registration (Recommended)**
```bash
cd headfwd
docker compose up -d headscale

# Start sidecar with auto-registration
cd headfwd-sidecar
go build
./headfwd-sidecar \
  --register \
  --proxy "https://headfwd.net" \
  --headscale "http://localhost:8080" \
  --noise-key "/var/lib/headscale/noise_private.key"
```

**Option B: Manual Registration**
```bash
# Get Headscale's public key
PUBKEY=$(curl -s "http://localhost:8080/key?v=96" | jq -r .publicKey)

# Phase 1: Get challenge
RESPONSE=$(curl -s -X POST https://headfwd.net/api/register/init \
  -H "Content-Type: application/json" \
  -d "{\"publicKey\": \"$PUBKEY\"}")

# Phase 2: Sign and verify (requires Noise private key access)
# See docs/REGISTRATION.md for details

# Phase 3: Start sidecar with tunnel URL
./headfwd-sidecar --tunnel "wss://abc123.headfwd.net/tunnel?auth=secret"
```

> **Security Note**: The challenge-response protocol prevents unauthorized subdomain registration.  
> See [`docs/REGISTRATION.md`](docs/REGISTRATION.md) for architecture details.

### 3. Update Headscale Config

```yaml
# headscale/config/config.yaml
server_url: https://abc123...xyz.headfwd.net
```

### 4. Connect Clients

Clients connect to `https://abc123.headfwd.net` instead of direct IP.

## Cost

- **Free tier:** 100+ Headscale instances
- **At scale:** $0.10/user/month (1000 users)
- Hibernating WebSockets = only charged when active

## Security

- **Challenge-Response Authentication** - Cryptographic proof of Headscale ownership
- **128-bit Fingerprints** - Collision-resistant subdomain identifiers (hex32)
- **Zero-Knowledge Proxy** - Sees only encrypted WireGuard traffic
- **Ed25519 Signatures** - Noise private key signs registration challenges
- **Time-Limited Challenges** - 5-minute nonce expiration prevents replay attacks
- **No Subdomain Enumeration** - Can't guess valid tunnels without private key
- **Never Trust the Proxy** - Fingerprints must be derived locally from Headscale's Noise public key and proxy-provided URLs must be verified against local derivation

For detailed security architecture, see [`docs/REGISTRATION.md`](docs/REGISTRATION.md).

## vs Alternatives

| Solution          | NAT | Setup   | Cost             |
| ----------------- | --- | ------- | ---------------- |
| **HeadFwd**       | ✅  | QR code | Free-$10/mo      |
| Port forward      | ❌  | Complex | $0               |
| ngrok             | ✅  | Easy    | $8/mo each       |
| Cloudflare Tunnel | ✅  | Medium  | Free (locked in) |

## Documentation

- **[Registration Architecture](docs/REGISTRATION.md)** - Secure challenge-response protocol
- **[Quick Start Guide](QUICKSTART.md)** - Step-by-step setup instructions
- **[Portal README](headfwd-sidecar/portal/README.md)** - Admin portal (built into sidecar), API docs, and development guide
- **[iOS PoC Plan](docs/ios-poc-plan.md)** - iOS app proof-of-concept plan (CLI build with XcodeGen)
- **[iOS Integration](tailscale-ios-integration-plan.md)** - iOS app integration guide (tailscaled compilation)

## Next Steps

1. Deploy proxy to Cloudflare Workers
2. Register your Headscale instance with challenge-response auth
3. Connect clients to your secure tunnel
4. Build iOS app for mobile access

## References

- [Headscale](https://headscale.net/)
- [Tailscale](https://tailscale.com/)
- [Cloudflare Durable Objects](https://developers.cloudflare.com/durable-objects/)
- Inspired by [headfwd-agent](https://github.com/headfwd/headfwd-agent)
