# Option 2: Custom Demo App

## Goal
Create a bespoke demo app and mobile client that showcases headfwd’s core value with a clean, minimal UI.

## Why this matters
- Maximum control over UX and narrative.
- Avoids constraints imposed by third-party app architectures.
- Can become a long-term product surface if desired.

## Scope (MVP)
- Backend app with auth and roles (admin/user).
- Device list UI and access control panel.
- iOS client with embedded tailscaled daemon.
- QR registration and session establishment.

## Milestones
- Define product story and target user flow.
- Build basic backend + admin UI.
- Implement mobile app with daemon integration.
- End-to-end demo video.

## Success criteria
- Clear, simple UI for onboarding, devices, and roles.
- End-to-end flow works without manual VPN steps.
- Demo communicates value in under 2 minutes.

## Risks
- Larger build surface (backend + UI + mobile) increases time-to-demo.
- Product scope ambiguity and design churn.
- Less immediate credibility vs integrating with known apps.

## Open questions
- What is the canonical user story for the demo?
- How polished does the UI need to be for the target audience?
- Do we want this to evolve into a first-party product?

## Next steps
- Decide the simplest user story.
- Pick a stack and UI approach.
- Estimate mobile app integration effort.
