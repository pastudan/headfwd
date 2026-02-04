# Option 1: Existing App Demo (Immich or Frigate)

## Goal
Build a compelling end-to-end demo by deeply integrating headfwd sidecar into a popular self-hosted app, including auth, device onboarding, and mobile access.

## Decision
Target app: **Immich**.

## Why this matters
- Fastest path to a wow moment and community interest.
- Leverages existing UX and user base for credibility.
- Proves end-to-end flow with real-world constraints.

## Scope (MVP)
- Choose one target app: **Immich**.
- Embed sidecar into backend service and auth flow.
- Mobile app integration (Immich iOS or Viewu iOS) with tailscaled daemon.
- QR registration flow from mobile to server.
- Minimal UI touchpoints for user/device state.

## Milestones
- Target selection and architecture fit assessment.
- Proof-of-concept integration (server + sidecar + auth).
- Mobile daemon embedding with QR registration.
- Demo walkthrough script and recording.

## Success criteria
- New user can onboard via QR in under 2 minutes.
- Mobile app connects securely to backend without manual VPN setup.
- Demo video conveys value in 60-90 seconds.

## Constraints
- Hosted proxy at `headfwd.net` is required for the zero-config moment.
- Keep the proxy small, self-contained, and low-memory footprint.
- Provide guidance on how many Headscale instances a single hosted proxy can support.

## Risks
- Mobile daemon embedding complexity or app build constraints.
- Auth integration mismatch with existing app assumptions.
- Scope creep from app-specific features.

## Open questions
- Which target app best matches the intended user story?
- Which auth model should be used for the demo?
- What is the smallest visible UI change needed?

## Next steps
- Decide Immich vs Frigate.
- Map auth flow and sidecar insertion points.
- Identify mobile app integration surface.
