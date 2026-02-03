#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS${NC} $*"; }
fail() { echo -e "${RED}FAIL${NC} $*"; }
info() { echo -e "${YELLOW}INFO${NC} $*"; }

echo "Choose mode:"
echo "1) Full wipe (delete ./headscale/data)"
echo "2) Reset only (keep data)"
read -r -p "Enter 1 or 2 (default 2): " mode
mode="${mode:-2}"

info "Stopping containers and clearing logs..."
docker compose down --remove-orphans
docker compose rm -sf >/dev/null 2>&1 || true

if [[ "$mode" == "1" ]]; then
  info "Wiping headscale data..."
  rm -rf ./headscale/data
  mkdir -p ./headscale/data
fi

info "Starting headscale + sidecar..."
docker compose up -d --build headscale headfwd-sidecar

info "Waiting for headscale /key?v=96..."
PUBKEY=""
for i in {1..60}; do
  KEY_JSON="$(curl -fsS "http://localhost:8080/key?v=96" 2>/dev/null || true)"
  if [[ -n "$KEY_JSON" ]]; then
    PUBKEY="$(printf '%s' "$KEY_JSON" | jq -r '.publicKey // empty' 2>/dev/null || true)"
    if [[ -n "$PUBKEY" && "$PUBKEY" != "null" ]]; then
      break
    fi
  fi
  sleep 1
done

if [[ -z "$PUBKEY" || "$PUBKEY" == "null" ]]; then
  echo "Failed to fetch a valid publicKey from /key?v=96."
  exit 1
fi

FINGERPRINT="$(printf '%s' "$PUBKEY" | openssl dgst -sha256 -r | awk '{print substr($1,1,32)}')"

echo "Headscale public key: $PUBKEY"
echo "Fingerprint: $FINGERPRINT"

echo "Deploy proxy?"
echo "1) Fly.io (fly deploy)"
echo "2) Cloudflare (npm run deploy)"
echo "3) Skip"
read -r -p "Enter 1, 2, or 3 (default 1): " deploy_mode
deploy_mode="${deploy_mode:-1}"

if [[ "$deploy_mode" == "1" ]]; then
  info "Deploying Fly.io proxy..."
  if command -v fly >/dev/null 2>&1; then
    cd "$ROOT_DIR/headfwd-proxy-fly"
    fly deploy
    cd "$ROOT_DIR"
  else
    echo "flyctl not found; skipping deploy."
  fi
elif [[ "$deploy_mode" == "2" ]]; then
  info "Deploying Cloudflare worker..."
  cd "$ROOT_DIR/headfwd-proxy"
  npm run deploy
  cd "$ROOT_DIR"
else
  info "Skipping proxy deploy."
fi

info "Waiting for tunnel to connect..."
CONNECTED=false
for i in {1..60}; do
  if docker compose logs --tail=50 headfwd-sidecar | grep -q "Tunnel connected"; then
    CONNECTED=true
    break
  fi
  sleep 1
done

if [[ "$CONNECTED" != "true" ]]; then
  echo "Tunnel did not connect. Check sidecar logs."
  exit 1
fi

info "Testing forwarded endpoints..."
HEALTH_CODE="$(curl -s -o /dev/null -w '%{http_code}' "https://${FINGERPRINT}.headfwd.net/health" || true)"
KEY_FORWARD="$(curl -s "https://${FINGERPRINT}.headfwd.net/key?v=96" || true)"
FORWARD_PUBKEY="$(printf '%s' "$KEY_FORWARD" | jq -r '.publicKey // empty' 2>/dev/null || true)"

echo "Forwarded /health status: $HEALTH_CODE"
echo "Forwarded /key?v=96: $KEY_FORWARD"

if [[ "$HEALTH_CODE" == "200" ]]; then
  pass "/health reachable at https://${FINGERPRINT}.headfwd.net/health"
else
  fail "/health failed (HTTP $HEALTH_CODE) at https://${FINGERPRINT}.headfwd.net/health"
  exit 1
fi

if [[ -n "$FORWARD_PUBKEY" && "$FORWARD_PUBKEY" == "$PUBKEY" ]]; then
  pass "Forwarded publicKey matches local /key?v=96"
else
  fail "Forwarded publicKey mismatch (got: $FORWARD_PUBKEY)"
  exit 1
fi

pass "Fingerprint URL validated: https://${FINGERPRINT}.headfwd.net"
echo "Done."

echo
info "Tailing logs (since startup): headscale, headfwd-sidecar"
docker compose logs -f --since 5m headscale headfwd-sidecar
