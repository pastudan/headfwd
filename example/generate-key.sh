#!/bin/bash
# Generate a new pre-authentication key

echo "🔑 Generating new pre-authentication key..."
echo ""

PREAUTH_KEY=$(docker exec headscale headscale preauthkeys create --user default --reusable --expiration 24h | grep -o 'nodekey:[a-z0-9]*')

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "✅ New Pre-Auth Key:"
echo ""
echo "   $PREAUTH_KEY"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "📋 Connect command:"
echo ""
echo "   sudo tailscale up --login-server=http://localhost:8080 --authkey=$PREAUTH_KEY"
echo ""


