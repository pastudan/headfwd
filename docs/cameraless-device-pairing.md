# Camera-less Device Pairing

How to onboard devices without cameras (Apple TV, HomePod, headless servers) to the tailnet via HeadFwd.

## Problem

The primary onboarding flow uses a QR code: the portal generates a preauth key, encodes it as JSON in a QR code, and the iOS app scans it with `DataScannerViewController`. The key never touches the proxy because the portal is accessed on the LAN and the iOS app uses the key over Noise to headscale directly.

A camera-less device like Apple TV cannot scan a QR code. It needs an alternative path to receive the preauth key securely -- ideally without ever exposing the key to the untrusted headfwd proxy on fly.io.

## Threat model recap

Per `NEVER-TRUST-PROXY.md`: the headfwd proxy is an untrusted relay. It terminates TLS and can read any HTTP payload it tunnels. The only channel the proxy cannot inspect is a Noise-encrypted session between a Tailscale client and headscale. Preauth keys must therefore travel over Noise or not touch the proxy at all.

## Approach: Device Code flow (OAuth 2.0 Device Authorization Grant pattern)

This is the same pattern used by Netflix/YouTube on smart TVs and by `tailscale up` on headless Linux boxes. The entire flow stays on the LAN -- the proxy is never involved.

### Prerequisites

- The Apple TV and the user's phone/laptop are on the same LAN as the headfwd sidecar.
- The portal is served by the sidecar on a local port (`:3001`), never exposed through the proxy tunnel. This is already the case.

### Flow

```
    Apple TV                      Sidecar (LAN :3001)              Phone/Laptop
    --------                      ------------------              ---------------
1.  POST /api/device-code  ──────>
    (no auth, just a device name)
                             generate:
                               session_id (random UUID)
                               user_code ("ABCD-1234")
                               device_code (64-char secret)
                               expires_in: 600s

2.  <────── { user_code, device_code, poll_interval, expires_in }

3.  Display on screen:
    ┌────────────────────────────┐
    │  Go to headfwd.local:3001  │
    │                            │
    │  Enter code: ABCD-1234     │
    └────────────────────────────┘

4.  (user sees TV screen)        ............          Open portal in browser
                                                       Enter user_code + select user
                                                       POST /api/device-code/approve
                                                       { user_code, user: "dan" }

5.                               sidecar:
                                   validate user_code
                                   create preauth key (10m, single-use)
                                   store key against session

6.  GET /api/device-code/poll  ──>
    { device_code }
                                 if not yet approved → 202 { status: "pending" }
                                 if approved → 200 { preauth_key, server_url }
                                 if expired → 410 { status: "expired" }

7.  <────── 200 { preauth_key, server_url }

8.  Use preauth_key to register
    with headscale over Noise   ─────────────────────────> headscale
```

### Why this is secure

- **Steps 1, 2, 6, 7** happen over the LAN between the Apple TV and the sidecar. The proxy is not involved.
- **Step 4** happens over the LAN between the user's browser and the portal. The proxy is not involved.
- The `device_code` is a long secret that the Apple TV uses to poll -- it is never displayed on screen or typed by a human. It prevents a rogue LAN device from claiming someone else's session.
- The `user_code` is a short, human-readable code displayed on screen. It is not secret (anyone in the room can see the TV), but it is useless without the `device_code`.
- The preauth key is generated server-side and delivered to the Apple TV over LAN only.
- The preauth key is then used over Noise to headscale -- the proxy sees only encrypted Noise traffic.

### Expiry and rate limiting

- Device code sessions expire after 10 minutes (same as QR-generated preauth keys).
- The sidecar should limit the number of active unapproved sessions (e.g., 10) to prevent abuse.
- The poll interval should be 5 seconds. The Apple TV polls until approved, expired, or the user cancels.

## Alternative considered: Ephemeral key exchange through the proxy

If the Apple TV is NOT on the same LAN (e.g., it can only reach headscale through the proxy), a more complex approach is needed:

1. Apple TV generates an ephemeral X25519 keypair.
2. Displays a short code + key fingerprint on screen.
3. User enters code in the portal; sidecar finds the pending session.
4. Sidecar generates preauth key, encrypts it with Apple TV's ephemeral public key using a NaCl box (X25519 + XSalsa20-Poly1305).
5. Apple TV retrieves the encrypted blob from the sidecar (through the proxy), decrypts with its ephemeral private key.
6. Proxy sees only ciphertext -- cannot recover the preauth key.

This is more complex and only necessary if the device cannot reach the sidecar on the LAN. For a PoC, the LAN-only Device Code flow is sufficient. The ephemeral key exchange is noted here for future work if remote onboarding becomes a requirement.

## Implementation scope (PoC)

Three new API endpoints on the sidecar portal:

| Endpoint | Method | Purpose |
|---|---|---|
| `/api/device-code` | POST | Apple TV requests a new pairing session |
| `/api/device-code/approve` | POST | User approves a session (from portal UI) |
| `/api/device-code/poll` | POST | Apple TV polls for the result |

Portal UI addition: a small "Pair device code" form (enter `user_code`, pick user, click approve). This could live in the existing Users/Devices page as an action.

tvOS client addition: a simple view that calls the three endpoints above and displays the user code while polling. No camera, no QR.

State is kept in-memory on the sidecar (a `map[string]*DeviceCodeSession`). No database needed. Sessions are garbage-collected on expiry.
