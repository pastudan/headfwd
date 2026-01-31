#!/bin/bash
# Connect the backend container to the Headscale mesh

set -e

echo "🔗 Connecting backend to HeadFwd mesh..."
echo ""

# Check if headscale is running
if ! docker ps | grep -q headscale; then
    echo "❌ Headscale container is not running!"
    exit 1
fi

# Check if we have a user
if ! docker exec headscale headscale users list 2>/dev/null | grep -q default; then
    echo "❌ No 'default' user found. Run ./setup-headscale.sh first"
    exit 1
fi

# Generate a key for the backend
echo "🔑 Generating auth key for backend..."
PREAUTH_KEY=$(docker exec headscale headscale preauthkeys create --user default --reusable --expiration 24h | grep -o 'nodekey:[a-z0-9]*')

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "📝 To connect backend to mesh, you need to:"
echo ""
echo "1. Install Tailscale in the backend container"
echo "2. Run: tailscale up --login-server=http://headscale:8080 --authkey=$PREAUTH_KEY"
echo ""
echo "Note: The current backend (Caddy) doesn't include Tailscale."
echo "When you build your real backend (Go/Python/Node), add Tailscale to it."
echo ""
echo "For now, connect your development machine instead:"
echo ""
echo "   sudo tailscale up --login-server=http://localhost:8080 --authkey=$PREAUTH_KEY"
echo ""
echo "Then you can access the backend at:"
echo "   http://backend:8081 (via Docker network)"
echo "   http://localhost:8081 (via host)"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""


