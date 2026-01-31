#!/bin/bash
# HeadFwd Quick Start Script

set -e

echo "🚀 Starting HeadFwd Services..."
echo ""

# Start services
echo "▶️  Starting Docker Compose..."
docker compose up -d

echo ""
echo "⏳ Waiting for services to be ready..."
sleep 3

# Check if Headscale is ready
echo "🔍 Checking Headscale..."
if curl -s http://localhost:8080/health > /dev/null; then
    echo "✓ Headscale is running"
else
    echo "⚠️  Headscale not responding yet, give it a moment..."
fi

# Check if backend is ready
echo "🔍 Checking Backend..."
if curl -s http://localhost:8081/health > /dev/null; then
    echo "✓ Backend is running"
else
    echo "⚠️  Backend not responding yet, give it a moment..."
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "🎉 HeadFwd is running!"
echo ""
echo "Next steps:"
echo ""
echo "1. Create a Headscale user:"
echo "   docker exec headfwd-headscale headscale users create default"
echo ""
echo "2. Generate a pre-auth key:"
echo "   docker exec headfwd-headscale headscale preauthkeys create --user default --reusable"
echo ""
echo "3. Visit the backend:"
echo "   http://localhost:8081"
echo ""
echo "4. View logs:"
echo "   docker compose logs -f"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

