#!/bin/bash
# Show routing information for the mesh

echo "🗺️  HeadFwd Mesh Routes"
echo ""

echo "━━━ Connected Nodes ━━━"
docker exec headscale headscale nodes list

echo ""
echo "━━━ Users ━━━"
docker exec headscale headscale users list

echo ""
echo "━━━ Network Info ━━━"
echo "IPv4 Range: 100.64.0.0/10"
echo "IPv6 Range: fd7a:115c:a1e0::/48"
echo ""


