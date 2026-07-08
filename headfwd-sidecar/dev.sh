#!/usr/bin/env bash
set -e

HEADSCALE_URL="${HEADSCALE_URL:-http://localhost:8080}"

cleanup() {
  if [ -n "$GO_PID" ]; then
    kill "$GO_PID" 2>/dev/null || true
    wait "$GO_PID" 2>/dev/null || true
  fi
  if [ -n "$VITE_PID" ]; then
    kill "$VITE_PID" 2>/dev/null || true
    wait "$VITE_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

echo "[dev] Starting Go API server on :3001..."
PORTAL_ENABLED=1 DEV=1 HEADSCALE_URL="$HEADSCALE_URL" UPSTREAM_URL="http://localhost:5173" \
  go run . --update-config=false --restart-headscale=false &
GO_PID=$!

# Wait for the Go server to start listening
for i in $(seq 1 30); do
  if curl -sf --max-time 1 http://localhost:3001/api/hello >/dev/null 2>&1; then
    echo "[dev] Go API server ready"
    break
  fi
  if ! kill -0 "$GO_PID" 2>/dev/null; then
    echo "[dev] Go server process died, check errors above"
    exit 1
  fi
  sleep 0.5
done

echo "[dev] Starting Vite on :5173..."
cd portal/frontend
npx vite &
VITE_PID=$!

wait
