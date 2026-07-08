# HeadFwd Quick Start

Get Headscale accessible from anywhere in 3 steps.

## 1. Deploy Proxy (5 minutes)

### Option A: Fly.io (recommended for TS2021)

```bash
cd headfwd-proxy-fly
fly launch --no-deploy
fly secrets set PUBLIC_HOST=headfwd.net
fly deploy
```

### Option B: Cloudflare Workers (HTTP-only)

```bash
cd headfwd-proxy
npm install

# Login to Cloudflare
npx wrangler login

# Create KV namespace
npx wrangler kv namespace create REGISTRY
# Copy the ID and update wrangler.jsonc

# Set tunnel secret
npx wrangler secret put TUNNEL_SECRET
# Enter a secure random string

# Deploy
npm run deploy

# Configure routes in Cloudflare dashboard:
# Workers & Pages → headfwd-proxy → Settings → Triggers
# Add routes: headfwd.net/* and *.headfwd.net/*
```

> **Note:** Tailscale TS2021 uses a custom HTTP Upgrade that Cloudflare Workers cannot proxy. Use Fly.io for full control-plane support.

## 2. Start Headscale + Sidecar (5 minutes)

The sidecar auto-registers with the proxy (X25519 ECDH + HMAC-SHA256), derives your
`<fingerprint>.headfwd.net` subdomain from Headscale's Noise key, opens the tunnel,
and can update Headscale's `server_url` for you.

```bash
cd ..

# Start Headscale + sidecar (sidecar has --register / AUTO_REGISTER=true by default)
docker compose up -d

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

Your Headscale is now accessible via `https://abc123.headfwd.net` even though it's behind NAT.

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

**"Headscale not connected" error:**
- Sidecar needs to be running first
- Check sidecar logs for connection errors
- Verify auth secret matches

## Next Steps

- **iOS App:** See `ios/README.md`
- **Security model:** See `docs/REGISTRATION.md` and `docs/ios-key-verification.md`
- **Production:** Use proper secrets, monitoring
- **Multiple Nodes:** Connect more devices to your mesh
