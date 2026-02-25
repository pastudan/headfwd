# iOS Background Execution & Push Notifications

## The fundamental constraint

APNs (Apple Push Notification service) is built on the assumption that the **app developer**
controls the push server. The `.p8` auth key you create in the Apple Developer portal
authenticates **the app publisher** to Apple. It proves "I am allowed to send pushes to devices
running `net.headfwd.app`".

**This key cannot be shipped in the headfwd-sidecar Docker image.** Any user who runs
`docker inspect` or extracts the binary gets it, can send arbitrary notifications to every
device with your app installed, and Apple will eventually revoke the key for abuse —
breaking push for all users simultaneously.

```
Apple's assumption:    developer-controlled server → APNs → device
headfwd reality:       user's self-hosted sidecar → ??? → device
```

The user's sidecar is an untrusted third party Apple has never heard of. There is no
supported mechanism for it to send APNs pushes for your bundle ID without holding your
credential.

---

## Option A: Push relay at headfwd.net (anonymous, recommended for production)

The standard pattern for self-hosted apps — used by Matrix/Element, Nextcloud, etc.:

```
user's sidecar  →  push.headfwd.net  →  APNs  →  device
```

The `.p8` key lives only on `push.headfwd.net`, which the app publisher controls. The sidecar
sends an authenticated push request; the relay fires it. Users never see the key.

This is identical to how Matrix homeservers use [Sygnal](https://github.com/matrix-org/sygnal)
for push — the pattern is well understood. The relay is a small, stateless Go service
(~100 lines wrapping the APNs HTTP/2 API).

**Trade-off**: `push.headfwd.net` must be reachable for push *delivery*. Crucially, no
user data flows through it — only "please push token X with payload Y". All actual
network data continues to flow directly between the iOS device and the sidecar over
the tailnet.

---

### Full data flow (how the pieces connect)

The key question: **how does a sidecar know which device tokens belong to which users,
and how does the relay know which sidecar is authorised to send what?**

```
┌─────────────────────────────────────────────────────────────────────┐
│  Phase 1: Enrolment (one-time, happens when iOS app connects)       │
│                                                                     │
│  iOS app  ──(tailnet 100.64.x.x)──►  sidecar  POST /api/push-token │
│           { "token": "abc…", "node_ip": "100.64.0.5" }             │
│                                                                     │
│  Sidecar stores: headscale_node_id → apns_token                     │
│  It knows the node because the request arrived from a 100.64 IP,   │
│  which it can look up in headscale's own node table.                │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│  Phase 2: Push (whenever the sidecar has something to say)          │
│                                                                     │
│  sidecar  ──(internet)──►  push.headfwd.net  POST /push            │
│           {                                                         │
│             "token":   "abc…",                                      │
│             "payload": { "aps": { "content-available": 1 },        │
│                          "event": "device-joined" },               │
│             "sig":     "<noise-key-signature>"                      │
│           }                                                         │
│                                                                     │
│  push.headfwd.net  ──►  APNs  ──►  iOS device                      │
│                                                                     │
│  iOS app wakes in background, reconnects tsnet, fetches event list  │
└─────────────────────────────────────────────────────────────────────┘
```

**The relay is completely stateless.** It has no concept of users, servers, or
relationships. It does not store device tokens. It does not know which tokens belong
to which sidecar. It only does two things:

1. Verify the request is from a legitimate headfwd-sidecar (authentication below)
2. Forward the push to APNs

---

### Authentication: noise key signatures, no registration required

Each headfwd-sidecar already has a WireGuard noise private key. Its public key is the
fingerprint that forms the sidecar's URL (e.g. `91268f…736e.headfwd.net`). This gives
us a self-authenticating identity that requires no pre-registration:

```
sidecar signs request body with its noise private key
    ↓
relay verifies the signature is valid (any valid Ed25519 key is accepted)
relay does NOT need a database of "known sidecars"
    ↓
relay forwards to APNs
```

Why this is safe: the relay only needs to ensure requests are signed by *some* valid
noise key. It does not need to authorise specific sidecars, because the push payload
contains only an opaque device token and a small metadata blob — there is no user data,
no PII, nothing sensitive. A rogue caller with a forged key could push spam
notifications to a token they somehow obtained, but they cannot enumerate tokens (the
sidecar holds those) and the rate-limiter on `push.headfwd.net` constrains volume.

For stronger auth (future): the relay could verify the noise key against headfwd's own
fingerprint registry, rejecting keys that are not associated with a known deployed
sidecar.

---

### Why the relay doesn't need user-to-server mapping

This is the part that isn't obvious. The answer is: **the sidecar is the authority on
which users it serves.** The relay never needs to know.

- The iOS device sends its APNs token directly to *its own sidecar* (over the tailnet).
  The sidecar stores it.
- When the sidecar wants to notify a specific user, it already knows that user's token
  (it stored it at enrolment) and sends it directly to the relay.
- The relay fires it at Apple and forgets.

The relay has no routing table, no user database, no per-server configuration. It is
purely a credentialed APNs proxy — a thin adapter that lets sidecars call APNs without
holding the `.p8` key themselves.

```
Sidecar A  stores: [alice_token, bob_token]   ──► relay ──► APNs
Sidecar B  stores: [carol_token]              ──► relay ──► APNs
Sidecar C  stores: [dave_token, eve_token]    ──► relay ──► APNs
                                                 ^
                          relay has no idea which sidecar owns which token.
                          it just validates the signature and forwards.
```

---

### Relay implementation sketch (Go, ~100 lines)

```go
// POST /push
// Body: { "token": "...", "payload": {...}, "sig": "base64(sign(body_without_sig))" }
func handlePush(w http.ResponseWriter, r *http.Request) {
    var req PushRequest
    json.NewDecoder(r.Body).Decode(&req)

    // Verify Ed25519 signature — any valid noise key is accepted
    if !verifyNoiseSignature(req) {
        http.Error(w, "unauthorized", 401); return
    }

    // Rate-limit per signing key to prevent spam
    if !rateLimiter.Allow(req.SigningKeyFingerprint) {
        http.Error(w, "rate limited", 429); return
    }

    // Forward to APNs using the stored .p8 key
    apnsClient.Push(req.Token, req.Payload)
    w.WriteHeader(202)
}
```

The full implementation is straightforward using
[`github.com/sideshow/apns2`](https://github.com/sideshow/apns2) on the relay side.

---

## Option B: BGTaskScheduler — fully self-hosted, zero cloud dependency

Register a `BGAppRefreshTask`. iOS wakes the app periodically in the background
(iOS decides when, typically every 15–60 minutes based on battery and usage heuristics).
The app reconnects via tsnet, polls for changes, optionally shows a local notification.

```swift
// Registration (call from AppDelegate / @main App init)
BGTaskScheduler.shared.register(
    forTaskWithIdentifier: "net.headfwd.refresh",
    using: nil
) { task in
    Task {
        guard let config = HeadscaleConfig.load() else {
            task.setTaskCompleted(success: false); return
        }
        try? await tailscaleService.connect(config: config)
        // fetch device list, check for new events, post local UNNotification if needed
        task.setTaskCompleted(success: true)
    }
    task.expirationHandler = { tailscaleService.disconnect() }
}
```

Add `BGTaskSchedulerPermittedIdentifiers` to `Info.plist` with value
`["net.headfwd.refresh"]`.

**Trade-off**: not real-time. iOS schedules this based on its own heuristics and will
throttle it in Low Power Mode. Fine for homelab management; not suitable for
latency-sensitive use cases.

---

## Option C: Just don't (correct for PoC, honest for homelab)

headfwd is a management tool, not a chat client. Users who want to check their network
open the app. The auto-connect on launch (added in `HeadFwdAppApp.swift`) means the
WireGuard reconnect happens the instant they tap the icon — typically 4–8 seconds.

For a homelab PoC this is the right call. Implement push when there is a clear,
concrete use case that requires it (e.g. "alert me when a new device joins the network").

---

## When a real tsnet reconnect is needed in the background

If you implement Option A or B, here is the timing profile for reconnecting tsnet
from a suspended/terminated state:

| Phase | Typical time |
|---|---|
| iOS wakes app from push / BGTask | ~0.5s |
| tsnet `up()` (cached WireGuard keypair) | 2–4s |
| WireGuard handshake to server | 0.5–2s |
| API call over tailnet | <1s |
| **Total** | **~4–8s** |

The 30-second background execution window is enough. tsnet reuses the stored keypair —
no preauth key is needed since the node is already registered in headscale.

---

## What future push notifications could enable

Once a relay is built:

- **"New device joined"** — notify the admin when a QR code is scanned
- **Access revocation** — device checks auth status on wakeup, disconnects if revoked  
- **Config push** — updated tailnet settings without requiring app foreground
- **Proactive reconnect** — re-establish WireGuard before the user opens the app

These are all post-PoC features. None require changes to the iOS app's core architecture.
