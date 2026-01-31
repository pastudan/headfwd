#!/bin/bash
# List all nodes connected to the Headscale mesh

echo "🌐 HeadFwd Mesh Nodes"
echo ""

docker exec headscale headscale nodes list

echo ""
echo "💡 Tip: Backend container is at 'backend' on the Docker network"
echo "   Once your device joins the mesh, you can access services via their Tailscale IPs"


