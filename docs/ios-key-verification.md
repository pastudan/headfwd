# iOS Key Verification & Pinning

HeadFwd treats the proxy as an untrusted relay (see `.cursor/rules/NEVER-TRUST-PROXY.md`). This document explains the attack this assumption motivates, and the two-layer defence the iOS app and the libtailscale patch implement together.

---

## Background: What the Noise Key Is

When a Headscale instance starts, it generates a Noise X25519 keypair and persists the private key to `noise_private.key`. The corresponding public key is served at:

```
GET /key?v=96  →  {"publicKey": "mkey:<64-hex-bytes>"}
```

The **fingerprint** embedded in every `<fingerprint>.headfwd.net` subdomain is derived from this key:

```
fingerprint = hex(SHA256("mkey:<64-hex-bytes>"))[:32]
```

When the sidecar registers with the proxy (see `docs/REGISTRATION.md`), it proves possession of the Noise *private* key via an X25519 ECDH + HMAC-SHA256 challenge-response. The proxy only grants a tunnel to a sidecar that has correctly completed this proof. The fingerprint subdomain therefore maps to exactly one Headscale instance.

---

## The Attack: Preauth Key Theft via Noise Key Substitution

Without the defence described below, the following attack is viable against a fully compromised proxy:

```
1. User scans QR code (in-person). iOS app stores:
     server = "https://<fingerprint>.headfwd.net"
     key    = "nodeauthkey:..."   (preauth key, 10-min TTL, single-use)

2. User taps Connect. iOS calls TailscaleNode.up().

3. tsnet internally fetches /key?v=96 from the proxy to bootstrap the
   Noise_IK handshake. A rogue proxy returns an attacker-controlled
   Noise public key instead of the legitimate one.

4. tsnet initiates Noise_IK using the attacker's public key.
   Only the entity with the attacker's private key can decrypt this channel.

5. The attacker decrypts the Noise session and reads the RegisterRequest
   protobuf. The preauth key ("nodeauthkey:...") is present in plaintext
   inside the encrypted channel — there is no cryptographic binding between
   the auth key and the device's node keypair.

6. The attacker POSTs a registration request to the real Headscale (through
   the tunnel it controls), using the stolen auth key and its own device
   keypair. Headscale accepts the key, registers the attacker's device, and
   marks the preauth key as consumed.

7. The legitimate user's connect attempt fails ("preauth key already used").
   The attacker's device is now on the tailnet.
```

**Why standard HTTPS does not help here.** The proxy IS the HTTPS termination point (Cloudflare or Fly.io). It can present valid TLS certificates for `*.headfwd.net` while manipulating the response body. TLS authenticates the transport layer, not the application-layer Noise key.

---

## The Defence: Two Layers

### Layer 1 — iOS verifies the key against the QR fingerprint

Before tsnet starts, `ServerKeyVerifier.swift` independently fetches `/key?v=96`:

```
fingerprint_from_url = host.components(separatedBy: ".").first
                     = "a1b2c3d4e5f60718293a4b5c6d7e8f90"

publicKey            = GET https://a1b2c3d4e5f60718293a4b5c6d7e8f90.headfwd.net/key?v=96
                     → "mkey:18c68c3d..."

computed             = SHA256("mkey:18c68c3d...").prefix(16 bytes).hex
                     = "a1b2c3d4e5f60718293a4b5c6d7e8f90"

assert computed == fingerprint_from_url
```

**Why a rogue proxy cannot forge this.** The fingerprint is established in-person via QR scan before any network connection. For the proxy to return a different key that still passes the SHA256 check, it would need to find a pre-image of the fingerprint — i.e., a string whose SHA256 hash begins with those 32 hex characters. SHA256 pre-image resistance makes this computationally infeasible (~2^128 work).

If the check passes, the verified key is written to `<stateDir>/pinned_server_key`. The state directory is the UUID-stamped `Documents/tailscale-<UUID>/` path already used for the device's WireGuard keypair.

**TOFU (Trust On First Use).** On subsequent connects the live key is re-fetched and compared to the pinned value. A change throws `keyChanged` and the connection is aborted — key rotation or substitution is surfaced immediately with a "re-scan QR code" prompt.

---

### Layer 2 — libtailscale reads the pinned key from disk

Layer 1 closes the verification gap for *our* check, but tsnet makes its own independent `/key?v=96` fetch during `Up()`. Without Layer 2, a rogue proxy could still serve tsnet a substitute key after serving us the legitimate one.

**The patch** (`ios/patches/libtailscale-pinned-key.patch`) modifies `TsnetUp` in `tailscale.go` to start a local HTTP interceptor before calling `s.s.Up()`:

```
Swift  ──TailscaleKit ObjC──▶  C symbol TsnetUp  ──cgo──▶  Go func TsnetUp
                                                                    │
                                                   check pinned_server_key
                                                                    │
                                               start local HTTP server
                                               on 127.0.0.1:<random-port>
                                                                    │
                                          s.s.ControlURL = "http://127.0.0.1:<port>"
                                                                    │
                                                       s.s.Up(ctx)  ▼
                                    tsnet ──GET /key──▶  local server
                                                         reads pinned_server_key from disk
                                                         returns {"publicKey": "mkey:..."}
                                    tsnet ──everything else──▶  reverse-proxy ──▶  real Headscale
```

#### How the interceptor works (Go implementation)

The interceptor is **pure Go** using only the standard library. There is no C code in it. The term "C→Go" refers to the ABI boundary between Swift/ObjC and the Go runtime — once inside Go, the full stdlib is available.

```go
// In TsnetUp, before s.s.Up():
if s.s.Dir != "" {
    if pinnedKey, err := readPinnedServerKey(s.s.Dir); err == nil {
        if localURL, cleanup, err := startKeyInterceptor(s.s.ControlURL, pinnedKey); err == nil {
            defer cleanup()
            s.s.ControlURL = localURL
        }
    }
}
```

`startKeyInterceptor` (in `tailscale_key_interceptor.go`, ~35 lines):

1. `net.Listen("tcp", "127.0.0.1:0")` — OS assigns a free loopback port
2. `httputil.NewSingleHostReverseProxy(target)` — standard Go reverse proxy pointed at the real Headscale URL
3. `http.ServeMux` with two routes:
   - `GET /key` → write `{"publicKey": "<pinnedKey>"}` directly from the in-memory string (which was read from the verified file on disk)
   - `/` (everything else) → forward to the real Headscale via the reverse proxy
4. `http.Server.Serve(ln)` runs in a goroutine
5. Returns `"http://127.0.0.1:<port>"` as the new `ControlURL` and a `cleanup` func

The deferred `cleanup()` calls `srv.Close()`, shutting down the loopback server as soon as `Up()` returns. The server lives for only the duration of tsnet's connection setup (typically a few seconds).

**What the proxy sees.** All control-plane traffic except `/key` still flows through the proxy to the real Headscale. The proxy can observe and relay this traffic as normal — the only thing it can no longer influence is which Noise key tsnet uses for the initial handshake.

---

## Combined Security Guarantee

| Attempt | Layer 1 result | Layer 2 result |
|---|---|---|
| Proxy returns legitimate key | ✓ passes fingerprint check, key pinned | tsnet reads pinned key — same key, correct handshake |
| Proxy returns attacker's key | ✗ SHA256 mismatch — **connection aborted before tsnet starts** | Layer 2 never reached |
| Proxy passes L1, swaps key for tsnet fetch | L1 already passed and pinned the *real* key | tsnet reads from disk — proxy response ignored |
| Key changes between connections (legitimate rotation) | ✗ TOFU mismatch — **connection aborted, re-scan prompted** | Layer 2 never reached |

All four cases either abort cleanly or proceed with the verified key.

---

## Patch Maintenance

`ios/patches/libtailscale-pinned-key.patch` is tracked in this repository. `ios/Makefile` applies it automatically after every `git clone`:

```makefile
$(LIBTAILSCALE_DIR)/.git:
    git clone --depth=1 $(LIBTAILSCALE_REPO) $(LIBTAILSCALE_DIR)
    cd $(LIBTAILSCALE_DIR) && git apply $(CURDIR)/patches/libtailscale-pinned-key.patch
```

The patch touches only `tailscale.go` (6 lines inserted into `TsnetUp`) and adds one new file (`tailscale_key_interceptor.go`, ~72 lines). It does **not** modify any `tailscale.com` dependency — all new code is in libtailscale's own package. If a future upstream libtailscale commit restructures `TsnetUp`, `git apply` will fail loudly at `make vendor`, signalling that the patch needs rebasing.

---

## Scope and Limitations

**What this protects against:**
- A rogue proxy substituting a different Noise key to tsnet
- Silent key rotation or swapping between connections (TOFU detects it)
- Misconfigured proxy routing a connect to the wrong Headscale instance

**What this does not protect against:**
- An attacker with physical access to the device (the pinned key file is on-disk)
- Compromise of the Headscale instance itself
- A rogue proxy that also controls the TLS certificate for `*.headfwd.net` *and* can break SHA256 pre-images (computationally infeasible)
- Post-quantum adversaries (X25519 is not post-quantum; this is a known limitation of the Tailscale Noise protocol generally)

**State reset.** `pinned_server_key` lives inside the UUID-stamped tsnet state directory. Calling `clearState()` (Reset in the UI) wipes the entire directory including the pinned key. The next connect will re-verify from scratch, which is the correct behaviour — a reset should also reset trust.
