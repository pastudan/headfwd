# Option 1 Iteration 1: Target Selection + Demo Skeleton

## Objective
Pick the best existing app target and define the smallest end-to-end demo that proves QR onboarding and secure access.

## Decision
Target app: **Immich**.

Rationale:
- Stronger “magical” moment with an integrated, polished app.
- High demo impact for conferences/videos.
- Clear path to show seamless user/device management.

## Decision matrix (fill as we evaluate)
Criteria:
- Mobile client integration effort
- Auth integration complexity
- Server-side integration surface
- Demo wow factor
- Time-to-first-demo
- Community reach

Candidates:
- Immich
- Frigate (or Viewu + Frigate)

## Minimum demo flow
- Admin sets up app with headfwd sidecar.
- Mobile user scans QR and enrolls.
- Mobile client connects via tailnet and accesses app.
- Admin sees device and role in UI.

## 30-second demo moment (target)
From a blank slate:
1. Open laptop.
2. Add minimal `docker-compose.yml` with two containers:
   - `pastudan/immich` (custom build with headfwd sidecar)
   - stock `headscale`.
3. Run `docker compose up -d`.
4. Open Immich in browser, show it works normally, then go to Users -> Add -> QR.
5. Scan QR with custom Immich iOS app.
6. Magic: user auth and headscale user/device management are tied into one seamless UI.

## Integration sketch (to refine)
- Backend:
  - Add sidecar deployment alongside app server.
  - Route client requests through headfwd proxy.
- Auth:
  - Bind app auth to tailnet identity or device fingerprint.
  - Gate roles based on registered device.
- Mobile:
  - Embed tailscaled daemon.
  - QR registration to bind device to app instance.

## Deliverables for this iteration
- Target app decision.
- Integration map for chosen app (touchpoints + files).
- Demo script (60-90 seconds).
- Workstream tracker for parallel agent work.

## Constraints / non-negotiables
- Hosted proxy at `headfwd.net` for zero-config onboarding.
- Keep proxy small, self-contained, and low-memory footprint.
- Provide guidance on expected scale (Headscale instances per hosted proxy tier).
- Aim to support 3 years of prepaid reserved capacity (cost predictability).

## Next actions
- Choose target app.
- Map auth and integration points.
- Identify mobile app build path.
- Assign owners in `docs/option-plans/option-1-workstreams.md`.
