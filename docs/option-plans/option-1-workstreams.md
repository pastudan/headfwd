# Option 1 Workstreams (Immich Demo)

## Purpose
Coordinate parallel work across backend, web UI, iOS, and infra. Agents should update their section with status, links, and next steps.

## Reference docs
- `docs/option-plans/option-1-immich-integration-map.md`
- `docs/option-plans/option-1-progress.md`
- `docs/option-plans/option-1-spec-backend.md`
- `docs/option-plans/option-1-spec-web-ui.md`
- `docs/option-plans/option-1-spec-ios.md`

## Status legend
- Planned
- In progress
- Blocked
- Done

## Workstreams
| Area | Owner | Status | Goal | Dependencies |
| --- | --- | --- | --- | --- |
| Backend + Sidecar | TBD | Planned | Integrate headfwd sidecar into Immich server image and runtime | Immich server code, headfwd-sidecar |
| Web UI | TBD | Planned | Add Users -> Add -> QR flow and device visibility in Immich UI | Backend API endpoints |
| iOS App | TBD | Planned | Embed `tailscaled`, scan QR, enroll device, connect to Immich | QR schema, proxy endpoint |
| Proxy + Infra | TBD | Planned | Keep `headfwd.net` small/low-memory, define scale guidance | Proxy metrics, DO config |
| Demo + Docs | TBD | Planned | 30–90s script, demo compose, talk track | All above |

## Backend + Sidecar
Status: Planned

Key tasks
- Task: Identify Immich server container build path and entrypoint.
- Task: Embed headfwd sidecar into `pastudan/immich` image.
- Task: Define config for sidecar (proxy URL, auth secret, fingerprint).
- Task: Wire headscale registration flow into Immich server.

## Web UI
Status: Planned

Key tasks
- Task: Locate Users management UI.
- Task: Add “Add User -> QR” flow.
- Task: Display headscale device identity and status.
- Task: Expose admin controls (role mapping, revoke device).

## iOS App
Status: Planned

Key tasks
- Task: Embed `tailscaled` daemon in Immich iOS app.
- Task: Implement QR scan and enroll flow.
- Task: Bind device to Immich user session.
- Task: Handle reconnect and status display.

## Proxy + Infra
Status: Planned

Key tasks
- Task: Baseline memory footprint for worker + DO.
- Task: Define scale guidance (instances per hosted proxy tier).
- Task: Document 3-year cost posture for hosted proxy.

## Demo + Docs
Status: Planned

Key tasks
- Task: Draft docker-compose for demo (Immich + Headscale + sidecar).
- Task: Script the 30-second demo moment.
- Task: Draft a short README for demo setup.
