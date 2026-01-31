#!/bin/bash
# Test connectivity to backend

echo "🧪 Testing HeadFwd connectivity..."
echo ""

echo "━━━ Testing Backend (Docker Network) ━━━"
if curl -s -f http://localhost:8081/health > /dev/null; then
    echo "✅ Backend health check: OK"
    echo ""
    echo "Backend response:"
    curl -s http://localhost:8081/api/info | jq . 2>/dev/null || curl -s http://localhost:8081/api/info
else
    echo "❌ Backend not reachable via localhost:8081"
fi

echo ""
echo "━━━ Testing Headscale ━━━"
if curl -s -f http://localhost:8080/health > /dev/null; then
    echo "✅ Headscale health check: OK"
else
    echo "❌ Headscale not reachable"
fi

echo ""
echo "━━━ Connected Nodes ━━━"
docker exec headscale headscale nodes list 2>/dev/null || echo "No nodes connected yet"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "💡 Next Steps:"
echo ""
echo "1. If no nodes are connected, run: ./setup-headscale.sh"
echo "2. Connect your device using the pre-auth key"
echo "3. Once connected, you'll be able to access backend via Tailscale IP"
echo ""


