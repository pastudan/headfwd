# HeadFwd Portal

Admin web portal integrated into the HeadFwd sidecar. Provides user management, device onboarding (QR codes), and a connectivity proof endpoint -- all embedded in the same Go binary.

## Enabling

Set `PORTAL_ENABLED=true` (or `1`) on the sidecar. The portal starts alongside the tunnel on a separate port.

```bash
PORTAL_ENABLED=1 PORTAL_PORT=3001 ./headfwd-sidecar --register ...
```

Or in docker-compose (enabled by default):

```yaml
environment:
  PORTAL_ENABLED: "true"
  PORTAL_PORT: "3001"
```

## API Key Bootstrap

The portal needs a Headscale API key for user/key management endpoints. If `HEADSCALE_API_KEY` is not set, the sidecar automatically creates one on startup via `docker exec headscale headscale apikeys create`. This requires the Docker socket to be mounted (which it already is for container restarts).

To provide a key manually instead, set `HEADSCALE_API_KEY` in the environment.

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `PORTAL_ENABLED` | `false` | Set to `true` or `1` to enable the portal |
| `PORTAL_PORT` | `3001` | HTTP listen port for the portal |
| `HEADSCALE_API_KEY` | (auto) | Headscale API key; auto-bootstrapped via docker exec if not set |

The portal inherits `HEADSCALE_URL` from the sidecar's `--headscale` flag.

## API Endpoints

### `GET /api/hello`

Connectivity proof -- returns the caller's IP address.

```json
{
  "message": "Hello from HeadFwd!",
  "your_ip": "100.64.0.42",
  "is_tailnet": true,
  "timestamp": "2026-02-23T12:00:00Z"
}
```

### `GET /api/users`

List all Headscale users.

### `POST /api/users`

Create a new user. Body: `{"name": "alice"}`

### `POST /api/keys`

Generate a preauth key + QR data. Body: `{"user": "alice", "reusable": false}`

## Frontend Development

The React frontend (Vite + Tailwind) lives in `portal/frontend/`. For development:

```bash
cd headfwd-sidecar

# Start Go server in dev mode
DEV=1 PORTAL_ENABLED=1 go run . --register=false --tunnel=wss://placeholder

# In another terminal, start Vite dev server (proxies /api to Go)
cd portal/frontend && npx vite
```

To rebuild for embedding:

```bash
cd portal/frontend && npx vite build
cd ../.. && go build -o headfwd-sidecar .
```

## Frontend Pages

- **Hello** (`/`) -- calls `/api/hello`, highlights tailnet IPs in green
- **Users** (`/users`) -- list and create Headscale users
- **Onboard Device** (`/onboard`) -- generate preauth key + QR code for iOS app scanning
