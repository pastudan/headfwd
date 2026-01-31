#!/bin/bash
# Show complete status of HeadFwd setup

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "           🔐 HeadFwd Status Dashboard"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Check Docker containers
echo "🐳 Docker Containers:"
docker compose ps 2>/dev/null || echo "   ⚠️  Docker Compose not running (run: docker compose up -d)"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Check Headscale
echo "🌐 Headscale Status:"
if curl -s -f http://localhost:8080/health > /dev/null 2>&1; then
    echo "   ✅ Running at http://localhost:8080"
    echo ""
    echo "   Users:"
    docker exec headscale headscale users list 2>/dev/null | sed 's/^/      /' || echo "      No users"
    echo ""
    echo "   Connected Nodes:"
    docker exec headscale headscale nodes list 2>/dev/null | sed 's/^/      /' || echo "      No nodes"
else
    echo "   ❌ Not responding"
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Check Backend
echo "🖥️  Backend Status:"
if curl -s -f http://localhost:8081/health > /dev/null 2>&1; then
    echo "   ✅ Running at http://localhost:8081"
    echo ""
    echo "   Service Info:"
    curl -s http://localhost:8081/api/info 2>/dev/null | sed 's/^/      /' || echo "      (no info available)"
else
    echo "   ❌ Not responding"
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Check local Tailscale
echo "📱 Local Device:"
if command -v tailscale > /dev/null 2>&1; then
    if tailscale status > /dev/null 2>&1; then
        echo "   ✅ Tailscale installed and running"
        echo ""
        TAILSCALE_IP=$(tailscale ip -4 2>/dev/null)
        if [ -n "$TAILSCALE_IP" ]; then
            echo "   Your Tailscale IP: $TAILSCALE_IP"
        fi
    else
        echo "   ⚠️  Tailscale installed but not connected"
        echo "   Run: sudo tailscale up --login-server=http://localhost:8080"
    fi
else
    echo "   ⚠️  Tailscale not installed"
    echo "   Install: brew install tailscale (macOS) or see QUICKSTART.md"
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Quick actions
echo "🚀 Quick Actions:"
echo ""
echo "   Setup:           ./setup-headscale.sh"
echo "   List nodes:      ./list-nodes.sh"
echo "   Generate key:    ./generate-key.sh"
echo "   Test:            ./test-connection.sh"
echo "   Full guide:      cat QUICKSTART.md"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"


