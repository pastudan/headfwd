# Spec C: Mobile iOS (QR + tailscaled)

## Objective
Add an iOS-only QR enrollment flow in the Immich mobile app that starts tailscaled, joins headscale, and claims an Immich session.

## Scope
- Add a QR scan entry point on the login screen.
- Parse and validate headfwd QR payloads.
- Start tailscaled using a native iOS integration.
- Claim Immich session using the enroll token.

## Out of scope
- Android support.
- Full production VPN UX polish.

## QR payload schema
The QR payload is a JSON string.

Example
```
{
  "v": 1,
  "type": "headfwd/immich-enroll",
  "immich": { "baseUrl": "https://immich.local:2283" },
  "headscale": { "loginServer": "https://<fingerprint>.headfwd.net", "preauthKey": "tskey-..." },
  "enrollToken": "<token>",
  "expiresAt": "<ISO>"
}
```

## UI flow
1. Login screen shows “Scan QR” button.
2. User scans QR.
3. App validates payload and shows progress UI.
4. App starts tailscaled with login server and preauth key.
5. App calls `POST /api/headfwd/claim` on Immich.
6. App stores session and navigates to main app.

## Flutter changes
New route and page
- Add `headfwd_enroll.page.dart` and route entry in `immich/mobile/lib/routing/router.dart`.

QR scanning
- Add a QR scanning package for Flutter.
- Parse QR payload into a `HeadfwdEnrollPayload` model.

Enroll service
- Add `headfwd_enroll.service.dart` to orchestrate.
Step 1: Start tailscaled via platform channel.
Step 2: Poll tailscaled status.
Step 3: Call Immich enroll endpoint.

## iOS native integration
Use the existing `tailscale-ios-integration-plan.md` as the baseline.

Required native pieces
- NetworkExtension target with `NEPacketTunnelProvider`.
- `tailscaled` binary embedded via gomobile or prebuilt framework.
- Swift bridge for start, stop, status, and login.

Flutter platform channel
- Channel name: `headfwd/tailscale`.
- Methods: `start`, `status`, `stop`.
- `start` args: `loginServer`, `preauthKey`, `deviceName`.

## Error handling
- If QR is invalid or expired, show a blocking error.
- If tailscaled fails to start, show retry.
- If claim fails, show error and reset to login screen.

## File touchpoints
- `immich/mobile/lib/pages/login/login.page.dart`
- `immich/mobile/lib/widgets/forms/login/login_form.dart`
- `immich/mobile/lib/routing/router.dart`
- `immich/mobile/lib/services/` (new headfwd enroll service)
- `immich/mobile/ios/` (native tailscaled integration)

## Acceptance criteria
- QR scan launches from login screen.
- Successful scan leads to tailscaled join.
- App obtains Immich access token without manual password entry.
- User lands on main app with a valid session.

## Open questions
- Which QR scanning package is preferred for the project.
- How to bundle tailscaled for iOS in this repo.
