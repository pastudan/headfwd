# HeadFwd Docker Compose Example

This example demonstrates the basic HeadFwd architecture with:
- **Headscale**: Self-hosted Tailscale control plane for mesh networking
- **Backend**: Simple service (Caddy) for testing connectivity

## 🚀 Quick Start

```bash
# 1. Start services
docker compose up -d

# 2. Setup Headscale (creates user, generates auth key)
./setup-headscale.sh

# 3. Check status
./status.sh
```

**📖 For detailed instructions, see [QUICKSTART.md](QUICKSTART.md)**

## Helper Scripts

| Script | Purpose |
|--------|---------|
| `setup-headscale.sh` | ⚙️ Initial setup - creates user & auth key |
| `status.sh` | 📊 Show complete system status |
| `list-nodes.sh` | 🌐 Show all connected devices |
| `generate-key.sh` | 🔑 Create a new pre-auth key |
| `show-routes.sh` | 🗺️ Display mesh routing info |
| `test-connection.sh` | 🧪 Test connectivity |
| `start.sh` | 🎬 Start Docker services |

## Connecting Devices to the Mesh

### On macOS/Linux

Install Tailscale client:

```bash
# macOS
brew install tailscale

# Linux
curl -fsSL https://tailscale.com/install.sh | sh
```

Connect to your Headscale server:

```bash
sudo tailscale up --login-server=http://localhost:8080 --authkey=YOUR_PREAUTH_KEY
```

### List Connected Nodes

```bash
docker exec headfwd-headscale headscale nodes list
```

## Architecture

```
┌─────────────────────────────────────────────┐
│            Docker Network                    │
│                                              │
│  ┌──────────────┐      ┌─────────────────┐ │
│  │  Headscale   │◄────►│  Backend        │ │
│  │  :8080       │      │  (Caddy)        │ │
│  │              │      │  :80            │ │
│  └──────────────┘      └─────────────────┘ │
│         ▲                                    │
└─────────┼────────────────────────────────────┘
          │
     WireGuard
      Tunnels
          │
    ┌─────┴─────┐
    │           │
┌───▼───┐   ┌───▼───┐
│  iOS  │   │ macOS │
│  App  │   │ Client│
└───────┘   └───────┘
```

## Configuration Files

- `docker-compose.yml` - Service definitions
- `headscale/config/config.yaml` - Headscale configuration
- `backend/Caddyfile` - Backend web server config

## Next Steps (Phase 1 Backend)

Replace the Caddy backend with a real music server:

1. Build a Go/Python/Node backend with:
   - Music library indexing
   - REST API for metadata
   - Audio streaming endpoint
   
2. Update `docker-compose.yml` to use custom backend image

3. Connect iOS app to backend via Tailscale mesh IP

## Useful Commands

### View Logs

```bash
# All services
docker compose logs -f

# Just Headscale
docker compose logs -f headscale

# Just backend
docker compose logs -f backend
```

### Restart Services

```bash
docker compose restart
```

### Stop Services

```bash
docker compose down
```

### Reset Everything

```bash
docker compose down -v
rm -rf headscale/data/*
```

## Ports

- `8080` - Headscale HTTP API
- `8081` - Backend service (exposed for testing)
- `9090` - Headscale metrics (optional)
- `50443` - Headscale gRPC

## Security Notes

⚠️ **This is a development setup!** For production:

1. Enable HTTPS/TLS on Headscale
2. Use proper domain names (not localhost)
3. Run your own DERP servers
4. Implement proper authentication on backend
5. Don't expose Headscale ports publicly
6. Use firewall rules

## Troubleshooting

### Headscale won't start

Check logs:
```bash
docker compose logs headscale
```

Ensure config is valid:
```bash
docker exec headfwd-headscale headscale version
```

### Can't connect devices

1. Check Headscale is running: `curl http://localhost:8080/health`
2. Verify pre-auth key is valid
3. Check firewall settings
4. Review Headscale logs

### Backend not accessible

```bash
# Check if running
docker compose ps

# Test directly
docker exec headfwd-backend wget -O- http://localhost
```

## References

- [Headscale Documentation](https://github.com/juanfont/headscale)
- [Tailscale](https://tailscale.com)
- [Caddy Web Server](https://caddyserver.com)

