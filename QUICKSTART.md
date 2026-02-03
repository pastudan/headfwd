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

```bash
cd ..

# Start Headscale
docker compose up -d headscale

# Create user
docker compose exec headscale headscale users create default

# Register with proxy
curl -X POST https://headfwd.net/api/register \
  -H "Content-Type: application/json" \
  -d '{"pubkey": "test-key-'$(date +%s)'"}'

# Save the response:
# {
#   "fingerprint": "abc123...xyz",
#   "tunnelUrl": "wss://abc123.headfwd.net/tunnel?auth=SECRET",
#   "publicUrl": "https://abc123.headfwd.net"
# }

# Update headscale config
# Edit headscale/config/config.yaml:
# server_url: https://abc123.headfwd.net

# Restart Headscale
docker compose restart headscale

# Start sidecar
export TUNNEL_URL="wss://abc123.headfwd.net/tunnel?auth=SECRET"
docker compose --profile with-tunnel up -d headfwd-sidecar

# Check logs
docker compose logs -f headfwd-sidecar
# Should see: "✓ Tunnel connected"
```

## 3. Connect Client (2 minutes)

```bash
cd ../tailscale

# Build tailscale
go build ./cmd/tailscale
go build ./cmd/tailscaled

# Start daemon
sudo ./tailscaled --tun=userspace-networking --state=/tmp/ts/state

# Connect (in another terminal)
./tailscale --socket=/tmp/ts/tailscaled.sock up \
  --login-server=https://abc123.headfwd.net

# Follow the auth URL or use pre-auth key
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

- **iOS App:** See `tailscale-ios-integration-plan.md`
- **Production:** Use proper secrets, monitoring
- **Multiple Nodes:** Connect more devices to your mesh
