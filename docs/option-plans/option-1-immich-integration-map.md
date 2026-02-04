# Immich Integration Map (Option 1)

## Purpose
Concrete file-level map for where to integrate headfwd into Immich. Use this to split work across agents.

## Web UI (Svelte)
User list and actions
- `immich/web/src/routes/admin/users/(list)/+layout.svelte`
- `immich/web/src/lib/services/user-admin.service.ts`

Create user modal (add QR option or new flow)
- `immich/web/src/routes/admin/users/(list)/new/+page.svelte`

User detail page (device visibility)
- `immich/web/src/routes/admin/users/[id]/+layout.svelte`
- `immich/web/src/lib/components/user-settings-page/device-card.svelte`

Routes and navigation
- `immich/web/src/lib/route.ts`

## Server (NestJS)
Admin user CRUD endpoints
- `immich/server/src/controllers/user-admin.controller.ts`
- `immich/server/src/services/user-admin.service.ts`
- `immich/server/src/dtos/user.dto.ts`

Auth and session endpoints (possible QR enroll/login flow)
- `immich/server/src/controllers/auth.controller.ts`
- `immich/server/src/services/auth.service.ts`
- `immich/server/src/dtos/auth.dto.ts`

User metadata and storage
- `immich/server/src/database.ts`
- `immich/server/src/repositories/user.repository.ts`

## API + SDK
OpenAPI definitions
- `immich/open-api/`

SDK types used by web
- `@immich/sdk` (generated from OpenAPI)

## Mobile (Flutter)
Login / onboarding flow
- `immich/mobile/lib/pages/login/login.page.dart`
- `immich/mobile/lib/widgets/forms/login/login_form.dart`
- `immich/mobile/lib/services/auth.service.dart`
- `immich/mobile/lib/providers/auth.provider.dart`

Routing (add QR scan route)
- `immich/mobile/lib/routing/router.dart`

iOS host integration
- `immich/mobile/ios/`

## Docker / Build
Immich server image
- `immich/server/Dockerfile`
- `immich/docker/docker-compose.yml`

Custom demo image
- `pastudan/immich` (to embed headfwd sidecar)

## Suggested integration pieces
- Add QR enroll endpoint in server to return enrollment token + fingerprint.
- Add web UI action: “Add user via QR”.
- Add device binding storage (user metadata or new table).
- Add mobile QR scan -> tailscaled start -> enroll -> login.
- Add device list to user admin UI (or extend existing “Authorized devices”).
