# Spec A: Backend + Sidecar (Immich Demo)

## Objective
Provide the backend changes needed for QR enrollment, headscale device binding, and a custom Immich image that runs the headfwd sidecar.

## Scope
- Add headfwd enrollment endpoints to Immich server.
- Store headfwd device bindings in user metadata.
- Integrate headfwd sidecar into a custom Immich server image.
- Provide demo-focused configuration and env vars.

## Out of scope
- Production-grade multi-tenant security hardening.
- Full headscale admin UI parity.
- Android mobile integration.

## Primary flow
1. Admin creates user in Immich.
2. Admin requests headfwd enrollment QR for that user.
3. Backend creates headscale preauth key and enrollment token.
4. QR encodes headscale login server, preauth key, and enroll token.
5. Mobile app scans QR, starts tailscaled, then calls Immich to claim enroll token.
6. Backend creates Immich session and stores device binding metadata.

## Data model changes
Add a new user metadata key for headfwd bindings.

Proposed addition
- `UserMetadataKey.Headfwd = 'headfwd'`

Proposed metadata shape
```
{
  "devices": [
    {
      "deviceId": "<headscale node id or node key>",
      "deviceName": "<user visible name>",
      "deviceOS": "iOS",
      "deviceType": "mobile",
      "lastSeenAt": "<ISO>"
    }
  ],
  "pendingEnrollment": {
    "tokenHash": "<sha256>",
    "expiresAt": "<ISO>",
    "preauthKey": "<headscale auth key>",
    "loginServer": "https://<fingerprint>.headfwd.net"
  }
}
```

Notes
- `pendingEnrollment` is removed after claim.
- For demo, `preauthKey` can be stored in metadata. For production, store only a hash or keep in memory.

## API endpoints
Create a new controller `HeadfwdController`.

1. Admin enrollment
- Method: `POST /api/admin/users/:id/headfwd/enroll`
- Auth: admin required
- Request body:
```
{
  "deviceName": "<optional>"
}
```
- Response body:
```
{
  "userId": "<uuid>",
  "qrPayload": "<string to encode>",
  "expiresAt": "<ISO>",
  "loginServer": "https://<fingerprint>.headfwd.net"
}
```

2. Headfwd status
- Method: `GET /api/admin/users/:id/headfwd`
- Auth: admin required
- Response body:
```
{
  "devices": [ ... ],
  "pendingEnrollment": { "expiresAt": "<ISO>" }
}
```

3. Mobile claim
- Method: `POST /api/headfwd/claim`
- Auth: none (token based)
- Request body:
```
{
  "enrollToken": "<token from QR>",
  "device": {
    "deviceName": "<string>",
    "deviceOS": "iOS",
    "deviceType": "mobile",
    "appVersion": "<string>"
  }
}
```
- Response body:
```
{
  "accessToken": "<immich session token>",
  "user": { "id": "<uuid>", "email": "<email>", "name": "<name>" }
}
```

## Headscale integration
Create a small adapter interface in Immich server.

Interface
- `ensureHeadscaleUser(user: UserAdmin): Promise<string>`
- `createPreauthKey(headscaleUserId: string, expiresAt: Date): Promise<string>`
- `resolveLoginServer(): string`

Implementation options
- Use headscale HTTP API at `HEADSCALE_URL` with an API key.
- Fallback: shell out to headscale CLI when running inside the compose stack.

Note
- Keep the adapter behind an interface so we can swap implementations after the demo.

## Sidecar integration
Goal: custom `pastudan/immich` image running Immich server and headfwd sidecar.

Approach
- Add a new Dockerfile under `immich/docker/Dockerfile.headfwd`.
- Multi-stage build copies the `headfwd-sidecar` binary into the image.
- Add an entrypoint script that starts sidecar, then Immich server.
- Use `tini` or `dumb-init` for signal handling.

Required mounts
- Mount headscale data and config into the Immich container so sidecar can read Noise key and update `server_url`.
- Optional mount of `/var/run/docker.sock` if sidecar restarts headscale.

Environment variables
- `HEADSCALE_URL=http://headscale:8080`
- `HEADSCALE_SERVER_URL=https://<fingerprint>.headfwd.net`
- `HEADSCALE_API_KEY=<token>`
- `HEADFWD_PROXY_URL=https://headfwd.net`
- `HEADFWD_ENROLL_TTL=300`
- `HEADFWD_AUTO_REGISTER=true`
- `RESTART_HEADSCALE=true` (demo convenience)

## File touchpoints
- `immich/server/src/controllers/` (new headfwd controller)
- `immich/server/src/services/` (new headfwd service)
- `immich/server/src/enum.ts` (add `UserMetadataKey.Headfwd`)
- `immich/server/src/types.ts` (extend `UserMetadata` type)
- `immich/server/src/repositories/user.repository.ts` (reuse existing metadata helpers)
- `immich/open-api/` (add endpoints)
- `immich/server/Dockerfile` or `immich/docker/Dockerfile.headfwd` (custom image)

## Acceptance criteria
- Admin can generate a QR code for a user.
- QR token expires and cannot be reused.
- Mobile can claim the token and receive an Immich access token.
- User detail page can show at least one headfwd device binding.
- Custom Immich image runs with sidecar and registers headscale server_url automatically.

## Open questions
- Final headscale API endpoints and auth method.
- Whether to store preauth key in metadata or return only in QR payload.
- Whether to auto-create headscale user per Immich user or use a shared user.
