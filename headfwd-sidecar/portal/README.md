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
| `PORTAL_ALLOWED_HOSTS` | (empty) | Extra hostnames (comma-separated, no port) that browsers may use to reach `/api/*`, e.g. a MagicDNS FQDN. See "DNS rebinding" below |

The portal inherits `HEADSCALE_URL` from the sidecar's `--headscale` flag.

## Security model: who is the caller?

The portal has no passwords. It decides who you are from the network
connection, so the rules below are the whole of its access control.

**Only the TCP peer address counts.** `X-Forwarded-For`, `X-Real-Ip` and
`Forwarded` are written by the client and are ignored for identity. Nothing
legitimate sits in front of the portal; if you put a reverse proxy in front of
it, every request will look like it comes from the proxy (see below).

**The portal listens twice, and each listener means something different:**

| Listener | Who can reach it | Who is admin |
|---|---|---|
| Tailnet (`100.x.y.z:3001`, via the sidecar's tsnet node) | Devices that joined your headscale tailnet; the WireGuard key proves which device | Only devices whose headscale user is in `admins.json`. Loopback or private source addresses get nothing here |
| LAN (`:PORTAL_PORT`, plain TCP on the host) | Anything that can reach the host's port | Any loopback or RFC 1918 caller (`127/8`, `10/8`, `172.16/12`, `192.168/16`). Tailnet identities are not recognised on this listener |

The LAN listener exists for first-run setup: you open the portal from a laptop
on the same network, create the first user and scan the QR code. After that,
think about whether you still want it:

- **Anyone who can reach the LAN port is admin.** That covers everyone on your
  Wi-Fi, any compromised device on it, and every other container on the Docker
  network. With Docker Desktop (macOS/Windows), *all* traffic to a published
  port appears to come from the Docker gateway, so anyone who can reach the
  host's port 3001 is local admin.
- **Options, from most to least open:**
  1. *Leave it as is* (default `docker-compose.yml`, `"3001:3001"`): convenient
     on a trusted home network.
  2. *Restrict it to the host*: publish it as `"127.0.0.1:3001:3001"`. Setup
     then happens on the machine itself, or through `ssh -L 3001:127.0.0.1:3001`.
  3. *Tailnet only*: don't publish the port at all. This suits appliances that
     ship with an enrolment QR code. The first user joins by scanning it and
     becomes admin through `admins.json` bootstrap, and nobody manages the
     install from the LAN.
- Apps behind `UPSTREAM_URL` decide their own LAN story. For example, one app
  might keep a username/password login for LAN visitors and sign tailnet
  visitors in automatically from the `X-Tailscale-User` header, which the
  portal only ever sets on the tailnet listener.

**Upstream identity headers.** For tailnet callers on the tailnet listener, the
`UPSTREAM_URL` proxy sets `X-Tailscale-User`, `-Login`, `-Device` and `-IP`
from the caller's headscale node. It strips any client-supplied copies of those
headers, along with `X-Real-Ip` and `Forwarded`, and replaces
`X-Forwarded-For` with the real peer address. LAN callers reach the upstream
with no identity headers. The upstream app should be reachable *only* through
the portal; anything that can talk to it directly can forge these headers.

**Browsers.** Because the portal trusts the network address, a malicious web
page open in an admin's browser is the main remaining threat. Two protections
cover it:

- *No CORS.* The API sends no `Access-Control-Allow-Origin`, so other sites
  cannot read its responses (such as a freshly minted preauth key). The portal
  UI is always same-origin: embedded, behind the Vite dev proxy, or behind the
  iOS app's 127.0.0.1 proxy.
- *Cross-origin writes are rejected.* Go's `http.CrossOriginProtection` returns
  403 for any `POST`/`PUT`/`DELETE` to `/api/*` that a browser marks as coming
  from another site. A change is accepted only from a page served by the
  portal itself. curl, scripts and native app code don't send those browser
  headers and are unaffected.
- *DNS rebinding.* An attacker can point their own domain at the portal's IP,
  and the browser would then call it "same-origin". To prevent this, `/api/*`
  only answers when the `Host` header is one of:
  - an IP address, `localhost`, or a single-label name;
  - a private-DNS name (`.local`, `.lan`, `.home.arpa`, `.internal`, ...);
  - a name listed in `PORTAL_ALLOWED_HOSTS`.

  Anything else gets `421`. If you reach the portal through a MagicDNS FQDN,
  add that name to `PORTAL_ALLOWED_HOSTS`.

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
