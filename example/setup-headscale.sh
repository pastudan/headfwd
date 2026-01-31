#!/bin/bash
# HeadFwd - Initial Headscale Setup Script

set -e

echo "🔧 Setting up Headscale for HeadFwd..."
echo ""

# Check if headscale is running
if ! docker ps | grep -q headscale; then
    echo "❌ Headscale container is not running!"
    echo "   Run: docker compose up -d"
    exit 1
fi

echo "✓ Headscale is running"
echo ""

# Create default user
echo "📝 Creating 'default' user namespace..."
docker exec headscale headscale users create default 2>/dev/null || echo "   (user already exists)"

echo ""
echo "🔑 Generating pre-authentication key..."
PREAUTH_KEY=$(docker exec headscale headscale preauthkeys create --user default --reusable --expiration 24h | grep -o 'nodekey:[a-z0-9]*')

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "✅ Headscale is ready!"
echo ""
echo "📋 Connection Details:"
echo ""
echo "   Server URL: http://localhost:8080"
echo "   Pre-Auth Key: $PREAUTH_KEY"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "🚀 To connect a device:"
echo ""
echo "   macOS/Linux:"
echo "   sudo tailscale up --login-server=http://localhost:8080 --authkey=$PREAUTH_KEY"
echo ""
echo "   iOS (via Tailscale app):"
echo "   1. Install official Tailscale app"
echo "   2. Settings → Use alternative coordination server"
echo "   3. Enter: http://YOUR_MACHINE_IP:8080"
echo "   4. Use auth key: $PREAUTH_KEY"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "📊 Useful commands:"
echo ""
echo "   List nodes:    ./list-nodes.sh"
echo "   Show routes:   ./show-routes.sh"
echo "   New auth key:  ./generate-key.sh"
echo ""


