# HeadFwd Quick Start

Get Headscale accessible from anywhere in 3 steps.

## 1. Pick a Proxy (0–15 minutes)

**Public proxy (default):** nothing to do. The sidecar registers with `https://headfwd.net`, and you'll be reachable at `https://<fingerprint>.headfwd.net`.

**Your own proxy** on Fly.io, on a domain you control (`example.com` below):

```bash
cd headfwd-proxy
fly launch --no-deploy
fly secrets set PUBLIC_HOST=example.com
fly deploy
fly certs add example.com
fly certs add "*.example.com"
fly certs setup "*.example.com"   # shows the DNS records to add, incl. the _acme-challenge CNAME
```

See [`headfwd-proxy/README.md`](headfwd-proxy/README.md) for the full DNS and certificate walkthrough.

> The earlier Cloudflare Workers version is parked on the `cloudflare-proxy` branch until Workers can proxy TS2021's custom HTTP Upgrade.

## 2. Start Headscale + Sidecar (5 minutes)

The sidecar auto-registers with the proxy (X25519 ECDH + HMAC-SHA256). It works out your
`<fingerprint>.<domain>` URL locally: the fingerprint from Headscale's Noise key, and the domain
from the proxy URL you configured (`headfwd.net` by default). It opens the tunnel and can update
Headscale's `server_url` for you.

```bash
cd ..

# Start Headscale + sidecar (sidecar has --register / AUTO_REGISTER=true by default)
docker compose up -d
# ...or, with your own proxy:
# PROXY_URL=https://example.com docker compose up -d

# Create a user
docker compose exec headscale headscale users create default

# Watch the sidecar register and connect
docker compose logs -f headfwd-sidecar
# Should show your fingerprint, "server_url" update, and "Tunnel connected"
```

To run the sidecar outside Docker instead:

```bash
cd headfwd-sidecar
go build
./headfwd-sidecar \
  --register \
  --proxy "https://headfwd.net" \
  --headscale "http://localhost:8080" \
  --noise-key "/var/lib/headscale/noise_private.key"
```

For your own proxy, use `--proxy "https://example.com"`. If you register at a host that differs
from the public base domain (e.g. `https://api.example.com` while sidecars live at
`<fingerprint>.example.com`), add `--proxy-domain example.com` (or `PROXY_DOMAIN=example.com`).

## 3. Connect a Client (2 minutes)

**iOS app (primary):** build and run `ios/` (see `ios/README.md`), then scan the QR
code the portal generates. The app verifies the Noise key against the fingerprint,
pins it, and connects via tsnet.

**Any Tailscale client:** point it at your tunnel URL as the login server.

```bash
tailscale up --login-server=https://abc123.headfwd.net
# Follow the auth URL, or use a pre-auth key from the portal
```

## Verify

```bash
# Check status
./tailscale status
# Should show your node with Tailscale IP

# On Headscale
docker compose exec headscale headscale nodes list
# Should show your connected node

# Test connectivity
ping 100.64.0.1  # Or whatever IP was assigned
```

## Done! 🎉

Your Headscale is now accessible via `https://abc123.headfwd.net` (or `https://abc123.example.com` on your own proxy) even though it's behind NAT.

## Troubleshooting

**Sidecar won't connect:**
```bash
# Check tunnel URL is correct
docker compose logs headfwd-sidecar

# Verify Headscale is running
curl http://localhost:8080/health
```

**Client can't connect:**
```bash
# Verify tunnel is active
curl https://abc123.headfwd.net/health

# Check Headscale logs
docker compose logs headscale
```

**`proxy issued public URL on "X" but this sidecar expects "Y"`:**
- The proxy is handing out URLs on a domain other than the one the sidecar was configured for.
- If you run the proxy: `fly secrets set PUBLIC_HOST=Y`.
- Otherwise check `--proxy` / `PROXY_URL` and `--proxy-domain` / `PROXY_DOMAIN` on the sidecar.
- The sidecar deliberately refuses to adopt a domain from the proxy (see `.cursor/rules/NEVER-TRUST-PROXY.md`).

**"Headscale not connected" error:**
- Sidecar needs to be running first
- Check sidecar logs for connection errors
- Verify auth secret matches

## Next Steps

- **iOS App:** See `ios/README.md`
- **Security model:** See `docs/REGISTRATION.md` and `docs/ios-key-verification.md`
- **Production:** Use proper secrets, monitoring
- **Multiple Nodes:** Connect more devices to your mesh
