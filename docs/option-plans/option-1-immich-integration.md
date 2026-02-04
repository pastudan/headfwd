# Option 1: Immich Integration Plan (Iteration 1)

## Objective
Define the concrete integration points for Immich (server, web UI, iOS) to deliver the QR onboarding + device management magic demo.

## Repo location
- Immich submodule: `immich/`
- Coordination: `docs/option-plans/option-1-workstreams.md`
- Integration map: `docs/option-plans/option-1-immich-integration-map.md`

## High-level architecture
- Immich server + headfwd sidecar in the same compose stack.
- Headfwd proxy at `headfwd.net` terminates the tunnel.
- Immich iOS app embeds `tailscaled` and registers via QR.
- Immich web UI surfaces users/devices tied to headscale.

## Integration surfaces (to locate in repo)
- Server auth & user management
- API routes for user/device lifecycle
- Web UI: Users -> Add flow
- iOS app: onboarding / login / deep link
- Docker build pipeline for custom `pastudan/immich` image

## Minimal integration design
- QR code encodes:
  - instance fingerprint
  - enrollment secret
  - headfwd proxy URL
- iOS app:
  - scans QR
  - starts `tailscaled`
  - registers device to Headscale
  - exchanges token for Immich session
- Immich server:
  - binds Immich user to Headscale device identity
  - exposes device list in UI

## Deliverables for this iteration
- Identify exact touchpoints in Immich server + web + iOS code.
- Define required changes (files, routes, UI components).
- Define docker-compose demo stack.
- Draft 30–90 second demo script.

## Open questions
- Which Immich auth flow is best for headscale-backed identity binding?
- Where does Immich surface user/device management in the UI?
- How do we minimize app changes while embedding `tailscaled`?

## Immediate next steps
- Pull Immich repo into a local workspace and locate:
  - user management UI
  - auth/session code paths
  - iOS onboarding flow
- Decide the minimal QR payload schema.
- Sketch the “Users -> Add -> QR” UI flow.
