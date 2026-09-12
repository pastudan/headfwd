# HeadFwd iOS App

Proof-of-concept iOS client for headscale-backed private networks.
Uses [tsnet](https://pkg.go.dev/tailscale.com/tsnet) (userspace WireGuard) via
[TailscaleKit](https://github.com/tailscale/libtailscale) — no VPN extension, no
system-wide routing, no `NetworkExtension` entitlement required.

## Quick start

```bash
make setup        # clone libtailscale, build TailscaleKit.xcframework, generate .xcodeproj
make run-device   # build + install + launch on physical device (see DEVICE_UDID in Makefile)
make build-sim    # build for simulator (no signing required)
```

Run `make generate` any time `project.yml` changes, then open Xcode once to refresh
provisioning (see Signing below).

## Architecture

```
HeadFwd portal (http://100.64.x.x:3001)
      ↑
      │  tsnet SOCKS5 (127.0.0.1:xxxx) ← all portal traffic
      │
RealTailscaleService
      │
      └─ TailscaleNode.up()  →  WireGuard handshake  →  headscale control server
                                                         (https://<fingerprint>.headfwd.net)
```

The QR code scanned at enrolment contains:
- `server` — the public headscale control URL (used for initial WireGuard registration)
- `key` — a single-use preauth key (cleared from storage immediately after registration)
- `tailnet_server` — the portal's tailnet address `http://100.64.x.x:3001` (added once
  the sidecar's tsnet node has joined the tailnet; may be absent on first scan)

## App Transport Security (`NSAllowsArbitraryLoads`)

The Info.plist contains `NSAllowsArbitraryLoads: true`. This is intentional.

The portal is served at a `100.64.x.x` (RFC 6598) address, which is the private IP
range assigned by headscale to tailnet nodes. No publicly-trusted Certificate Authority
can issue a certificate for this range — ACME validation requires a publicly reachable
host. The traffic is nonetheless encrypted and authenticated by WireGuard (ChaCha20-
Poly1305, Noise Protocol key exchange, forward secrecy), which provides equivalent
security to TLS 1.3.

For the full technical justification suitable for Apple review submission, see
[`docs/apple-ats-justification.md`](../docs/apple-ats-justification.md).

**Future:** migrate to `tsnet.Server.ListenTLS` once headscale's cert signing endpoint
is stable. That will issue `https://` certs via the tailnet CA, eliminating the
exception.

## Signing

Code signing settings live in `project.yml` (`DEVELOPMENT_TEAM`, `CODE_SIGN_STYLE`,
`CODE_SIGN_IDENTITY`) so they survive `make generate`. After any regeneration, open
Xcode → target → Signing & Capabilities once to let Xcode refresh the provisioning
profile, then CLI builds work again.

Set your own Apple Developer Team ID (find it in Xcode → Settings → Accounts, or at
developer.apple.com → Membership):

```bash
export HEADFWD_TEAM_ID=XXXXXXXXXX
export HEADFWD_DEVICE_UDID=...    # xcrun xctrace list devices
```

Or override per-invocation with `make build-device TEAM=XXXXXXXXXX`.

## Files

| Path | Purpose |
|---|---|
| `project.yml` | XcodeGen project definition |
| `Makefile` | Build orchestration |
| `HeadFwdApp/` | Swift source |
| `HeadFwdApp/Services/RealTailscaleService.swift` | tsnet connection management |
| `HeadFwdApp/Services/NetworkService.swift` | Portal API client |
| `HeadFwdApp/Models/HeadscaleConfig.swift` | QR payload model + UserDefaults persistence |
| `Frameworks/TailscaleKit.xcframework` | Built from `vendor/libtailscale` (gitignored) |
| `vendor/libtailscale` | Official Tailscale C library source (gitignored) |
