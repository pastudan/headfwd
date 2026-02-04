# Spec B: Web UI (Users -> Add -> QR)

## Objective
Add a QR-based enrollment flow in the Immich admin UI and show headfwd device bindings for a user.

## Scope
- Add a new “Add via QR” admin action.
- Generate and display enrollment QR payloads.
- Poll enrollment status and show device bindings.

## UI flow
1. Admin opens Users list.
2. Admin clicks “Add via QR”.
3. Modal collects basic user details.
4. Backend creates user and returns QR payload.
5. QR is displayed for scanning.
6. UI polls status until device binds.
7. User detail page shows headfwd device info.

## UI components
Use existing QR components.
- `immich/web/src/lib/modals/QrCodeModal.svelte`
- `immich/web/src/lib/components/shared-components/qrcode.svelte`

## New UI elements
- Add a new action “Add via QR” in users list actions.
- Add a modal or route for QR enrollment.
- Add a new “Headfwd Devices” section on the user detail page.

## Suggested implementation
Users list action
- Update `immich/web/src/lib/services/user-admin.service.ts` to add a new action item.
- Route to a new page or open a modal to collect user info.

QR enrollment modal
- Create `immich/web/src/lib/modals/HeadfwdEnrollModal.svelte`.
- Collect email, name, admin flag, optional quota.
Submit flow steps:
Step 1: Call `createUserAdmin`.
Step 2: Call `POST /api/admin/users/:id/headfwd/enroll`.
Step 3: Display QR using `QrCodeModal` or embedded QR component.

Polling for device binding
- After QR is shown, poll `GET /api/admin/users/:id/headfwd` every 2 to 5 seconds.
- On device detected, show success and link to user detail page.

User detail page display
- Update `immich/web/src/routes/admin/users/[id]/+layout.svelte`.
- Add a new card for headfwd devices, similar to `authorized_devices`.

## UI strings
Add new i18n keys for:
- “Add via QR”
- “Scan to enroll”
- “Waiting for device…”
- “Device enrolled”

## API integration
- Add new SDK methods after OpenAPI update.
- Use `@immich/sdk` in `user-admin.service.ts`.

## File touchpoints
- `immich/web/src/lib/services/user-admin.service.ts`
- `immich/web/src/routes/admin/users/(list)/+layout.svelte`
- `immich/web/src/routes/admin/users/(list)/new/+page.svelte`
- `immich/web/src/routes/admin/users/[id]/+layout.svelte`
- `immich/web/src/lib/modals/QrCodeModal.svelte`
- `immich/web/src/lib/components/shared-components/qrcode.svelte`

## Acceptance criteria
- Admin can create a user and display a QR in one flow.
- QR is scannable and copyable.
- UI reports enrollment success within 5 to 10 seconds.
- User detail page shows headfwd device(s) when present.

## Open questions
- Should “Add via QR” replace or sit beside the current create user flow?
- Should the QR modal also show the headscale login server URL?
