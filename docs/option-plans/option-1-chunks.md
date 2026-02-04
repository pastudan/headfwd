# Option 1 Chunk Plan (Immich Demo)

## Chunk A: Backend + Sidecar
Goal: Custom Immich server image with headfwd sidecar and QR enrollment endpoints.

Tasks
- Task: Define QR payload schema and enrollment endpoint.
- Task: Add server-side storage for headscale device binding.
- Task: Add sidecar configuration and lifecycle wiring.
- Task: Add or update API routes for device visibility.

## Chunk B: Web UI
Goal: “Users -> Add -> QR” flow and device visibility in admin UI.

Tasks
- Task: Add “Add via QR” action in Users list.
- Task: Add QR modal/view and polling status.
- Task: Add device list to user detail (or extend Authorized devices).

## Chunk C: Mobile iOS
Goal: Scan QR, start `tailscaled`, enroll device, and connect to Immich.

Tasks
- Task: Add QR scan route and UI.
- Task: Integrate `tailscaled` into iOS host app.
- Task: Implement enroll flow to headfwd proxy + server.
- Task: Bind session to Immich user.

## Chunk D: Proxy + Demo
Goal: Stable hosted proxy posture and a slick demo setup.

Tasks
- Task: Confirm proxy memory footprint and cost guidance.
- Task: Draft demo `docker-compose.yml` with Immich + Headscale.
- Task: Produce 30–90s demo script and runbook.

