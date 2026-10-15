# HeadFwd Quick Start Guide

Get your Headscale mesh up and running in 5 minutes.

## Step 1: Start Services

```bash
cd example
docker compose up -d
```

Wait a few seconds for services to initialize.

## Step 2: Setup Headscale

```bash
./setup-headscale.sh
```

This will:
- Create a `default` user namespace
- Generate a pre-authentication key
- Show you the connection command

**Save the auth key!** You'll need it to connect devices.

## Step 3: Connect Your Development Machine

### macOS

```bash
# Install Tailscale (if not already installed)
brew install tailscale

# Connect to your Headscale server
sudo tailscale up --login-server=http://localhost:8080 --authkey=YOUR_KEY_HERE
```

### Linux

```bash
# Install Tailscale
curl -fsSL https://tailscale.com/install.sh | sh

# Connect to your Headscale server
sudo tailscale up --login-server=http://localhost:8080 --authkey=YOUR_KEY_HERE
```

## Step 4: Verify Connection

```bash
./test-connection.sh
```

This checks:
- ✅ Backend is accessible
- ✅ Headscale is running
- ✅ Your device is connected to the mesh

## Step 5: Access the Backend

Once connected to the mesh:

```bash
# Via localhost (always works)
curl http://localhost:8081/api/info

# Via Docker network name (from other containers)
curl http://backend:8081/api/info

# Via Tailscale IP (once your device joins the mesh)
# Get the backend's Tailscale IP first:
./list-nodes.sh
```

## Available Helper Scripts

| Script | Purpose |
|--------|---------|
| `setup-headscale.sh` | Initial setup - creates user & auth key |
| `list-nodes.sh` | Show all connected devices |
| `generate-key.sh` | Create a new pre-auth key |
| `show-routes.sh` | Display mesh routing info |
| `connect-backend.sh` | Get instructions to connect backend |
| `test-connection.sh` | Test connectivity |
| `start.sh` | Start Docker services |

## Common Commands

### View Logs

```bash
# All services
docker compose logs -f

# Just Headscale
docker compose logs -f headscale

# Just backend
docker compose logs -f backend
```

### List Connected Devices

```bash
./list-nodes.sh
```

### Generate New Auth Key

```bash
./generate-key.sh
```

### Restart Everything

```bash
docker compose restart
```

## What You Can Do Now

### 1. Access Backend from Your Mac

```bash
# Your Mac is now part of the mesh
curl http://localhost:8081
```

### 2. Connect Your iPhone

To connect an iOS device:

1. Install the official Tailscale app from App Store
2. Open Settings in Tailscale app
3. Tap "Use alternative coordination server"
4. Enter: `http://YOUR_MAC_IP:8080` (get your Mac's IP with `ifconfig`)
5. Use the auth key from `./generate-key.sh`

**Note:** For production, you'd want HTTPS and a proper domain. This is for local dev only.

### 3. Swap in Your Own App

The Caddy backend is a placeholder. To use HeadFwd with your own self-hosted app:

1. Replace the `backend` service in `docker-compose.yml` with your app
2. Your app will automatically be on the same Docker network
3. Add the HeadFwd sidecar (see the root [`QUICKSTART.md`](../QUICKSTART.md)) so devices can reach it remotely
4. Use the same Headscale setup

## Troubleshooting

### Headscale Won't Start

```bash
# Check logs
docker compose logs headscale

# Verify config
docker exec headscale headscale version
```

### Can't Connect Device

1. Check Headscale is running: `curl http://localhost:8080/health`
2. Generate a fresh key: `./generate-key.sh`
3. Check firewall settings
4. Try using your machine's actual IP instead of `localhost`

### Backend Not Accessible

```bash
# Check container status
docker compose ps

# Test directly
docker exec backend wget -O- http://localhost
```

## Next Steps

Once you have devices connected:

1. **Your app:** Replace the placeholder backend with the app you want to reach remotely
2. **Remote access:** Run the HeadFwd sidecar and proxy (see the root [`QUICKSTART.md`](../QUICKSTART.md))
3. **Clients:** Build the example iOS app ([`ios/README.md`](../ios/README.md)) or use any Tailscale client

See the root [`README.md`](../README.md) for how the pieces fit together.

## Architecture Reminder

```
┌────────────────────────────────────────────┐
│         HeadFwd Mesh Network               │
│                                            │
│  ┌──────────┐     ┌──────────────────┐   │
│  │Headscale │◄───►│  Backend         │   │
│  │  :8080   │     │  (Caddy/Future)  │   │
│  └──────────┘     └──────────────────┘   │
│       ▲                                    │
└───────┼────────────────────────────────────┘
        │ WireGuard Tunnels
        │
   ┌────┴────┐
   │         │
 ┌─▼──┐   ┌─▼──┐
 │Mac │   │iOS │
 └────┘   └────┘
```

All traffic between devices flows through encrypted WireGuard tunnels, coordinated by your self-hosted Headscale server.

🎉 **You're ready to start building!**


