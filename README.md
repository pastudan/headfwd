# HeadFwd

A secure design pattern for reaching self-hosted apps at home.

## What Is This?

HeadFwd lets you access your self-hosted apps from anywhere using Headscale (self-hosted Tailscale), even if your server is behind NAT/firewall.

**Problem:** Setting up remote access for a self-hosted app sucks. Typically, you:
1. Set up port forwarding so you can get to your app
2. Set up DDNS so you can always reach it
3. Set up an auth layer so no bad actors get in / see your data

**Solution:** A zero-knowledge reverse tunnel. It's like ngrok, but the proxy can't see your keys or data

## Architecture

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/architecture-default-dark.svg">
  <img alt="HeadFwd architecture: Headscale, the HeadFwd sidecar and your app run behind NAT and can ship as a single app. The sidecar dials out a WSS tunnel to the untrusted HeadFwd proxy; clients open their own WSS tunnel to the proxy, which splices it into the sidecar's tunnel. The control traffic inside is Noise-encrypted end to end so only Headscale can read it, and devices then reach your app over a secure direct WireGuard channel." src="docs/images/architecture-default-light.svg" width="960">
</picture>\
You now have the best of both worlds; a secure, fully local control plane AND remote access.

1. The sidecar dials out and keeps a persistent WSS tunnel open to the proxy, so no inbound ports are needed.
2. Clients open a WSS tunnel to `https://<fingerprint>.headfwd.net`, and the proxy forwards it into the sidecar's tunnel. This carries Tailscale's TS2021 control protocol, which is Noise-encrypted end to end, so only your Headscale instance at home can read it. The client was given the Headscale's pubkey when it first scanned a QR code to access your app.
3. Devices then talk to your app directly over a secure direct WireGuard channel.

Headscale, the sidecar and your app don't have to be separate services: they can ship together as a single app.

The proxy is a stateless relay and only forwards to your Headscale based on your your public fingerprint (the subdomain in \`\<fingerprint>.headfwd.net\`. It never sees your keys or plaintext.

I've been using this for my apps for 6 months now, and its been working super reliably. Since hosting a control plane proxy costs next to nothing, I am committing to running `headfwd.net` as a public proxy for anyone to use for at least the next 5 years.

## Example: Frigate cameras

You've set up cameras at home with [Frigate](https://frigate.video/), and you want to check them from your phone while you're out. What you don't want:

* **Footage passing through a middle party.** A cloud camera service, or a relay that terminates TLS, can see your video. It also adds latency and has to carry a lot of bandwidth
* **Port forwarding.** Every open port is reachable by the whole internet, and a router or ISP change quietly breaks it.
* **A public login page** in front of Frigate, or a reverse proxy with external auth. That puts an attack surface on the internet, and it has to be patched and kept working forever.

With HeadFwd, Headscale and the sidecar run on the same box as Frigate, either alongside it or built into a single app. Nothing listens on a public port: the sidecar dials out to the proxy. Your phone scans a QR code to join your own tailnet via that tunnel, and the proxy only relays control traffic it cannot decrypt. Video then travels over WireGuard, encrypted end to end between your phone and Frigate, so no middle party can ever read your footage.

## Components

- **`headfwd-proxy-fly/`** - Go proxy for Fly.io (recommended; full TS2021 control-plane support)
- **`headfwd-proxy/`** - Cloudflare Workers + Durable Objects proxy (HTTP-only alternative)
- **`headfwd-sidecar/`** - Go sidecar that runs with Headscale: opens the tunnel, auto-registers, and serves an optional admin portal (embedded React UI for user management, device onboarding, connectivity proof)
- **`ios/`** - SwiftUI iOS app that connects via tsnet (embedded `libtailscale`), with QR onboarding and Noise-key verification/pinning
- **`docs/REGISTRATION.md`** - Sidecar↔proxy registration protocol (X25519 ECDH + HMAC-SHA256 challenge-response)
- **`docs/ios-key-verification.md`** - iOS key verification & pinning (preauth-key theft protection)

## Security

HeadFwd is designed on the principle that **the proxy is untrusted**. All security-critical values are derived and verified locally; the proxy is treated as a dumb (and potentially rogue) relay.

### Sidecar ↔ Proxy Registration

Before a sidecar is granted a tunnel, it must prove possession of Headscale's Noise private key via an X25519 ECDH + HMAC-SHA256 challenge-response. The proxy cannot impersonate a legitimate sidecar, and a sidecar cannot register without the private key.

See **[`docs/REGISTRATION.md`](docs/REGISTRATION.md)** for the full protocol.

### iOS Key Verification & Pinning

Before the iOS app hands control to tsnet, it independently verifies that the Headscale Noise public key served by the control URL hashes to the fingerprint embedded in the QR code's server URL. A patched libtailscale then reads the verified key from disk so tsnet never fetches it from the proxy — closing a preauth-key theft attack that would otherwise be viable against a rogue proxy.

See **[`docs/ios-key-verification.md`](docs/ios-key-verification.md)** for the full threat model, attack chain, and implementation details.

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

# Phase 2: Compute HMAC proof from the X25519 ECDH shared secret
#          (sidecar Noise private key × proxy ephemeral public key) and verify.
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

- **Fly.io:** a single shared-cpu-1x instance comfortably runs the proxy; fits within the free/low-cost tier for personal use
- **Cloudflare Workers:** free tier covers 100+ Headscale instances; hibernating WebSockets mean you're only billed when a tunnel is active
- Either way, the proxy only relays encrypted traffic — no per-GB data egress for the control plane

## Security Properties at a Glance

- **Challenge-Response Authentication** - Cryptographic proof of Headscale ownership
- **128-bit Fingerprints** - Collision-resistant subdomain identifiers (hex32)
- **Zero-Knowledge Proxy** - Sees only encrypted WireGuard/Noise traffic
- **X25519 ECDH + HMAC-SHA256** - The sidecar proves possession of Headscale's Noise private key via an ECDH shared secret with the proxy's ephemeral key; no new key material is introduced
- **iOS Key Pinning** - The app verifies the Noise key against the QR fingerprint and pins it so tsnet never trusts a proxy-supplied key
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

- **[Registration Architecture](docs/REGISTRATION.md)** - Sidecar↔proxy challenge-response protocol (X25519 ECDH + HMAC-SHA256)
- **[iOS Key Verification](docs/ios-key-verification.md)** - Noise-key verification, pinning, and the preauth-key theft threat model
- **[Quick Start Guide](QUICKSTART.md)** - Step-by-step setup instructions
- **[Portal README](headfwd-sidecar/portal/README.md)** - Admin portal (built into sidecar), API docs, and development guide
- **[iOS App](ios/README.md)** - SwiftUI app build (XcodeGen + patched `libtailscale`)

## Next Steps

1. Deploy the proxy (Fly.io recommended, Cloudflare Workers for HTTP-only)
2. Run Headscale + sidecar; the sidecar auto-registers with challenge-response auth
3. Point Headscale's `server_url` at your `<fingerprint>.headfwd.net`
4. Connect the iOS app (or any Tailscale client) via QR onboarding

## License

MIT — see [`LICENSE`](LICENSE).

Built by [@pastudan](https://github.com/pastudan) as a personal project, now maintained by Bridge.

## References

- [Headscale](https://headscale.net/)
- [Tailscale](https://tailscale.com/)
- [Fly.io](https://fly.io/)
- [Cloudflare Durable Objects](https://developers.cloudflare.com/durable-objects/)
