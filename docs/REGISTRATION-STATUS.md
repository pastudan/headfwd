# Registration Implementation Status

## ✅ Completed

1. **Hex32 Fingerprints** - 128-bit collision-resistant subdomain identifiers
2. **Two-Phase Registration** - `/api/register/init` and `/api/register/verify` endpoints
3. **Challenge-Response Protocol** - Nonce generation and expiration (5 minutes)
4. **Cryptographic Proof of Ownership** - X25519 ECDH + HMAC-SHA256 (see below)
5. **Sidecar Auto-Registration** - `--register` flag with automatic key detection
6. **Two Proxy Backends** - Fly.io (Go, in-memory, TS2021-capable) and Cloudflare Workers (TypeScript + KV, HTTP-only)
7. **Documentation** - `docs/REGISTRATION.md` (protocol) and `docs/ios-key-verification.md` (client-side pinning)

## ✅ Resolved: Proof of Ownership Without a Signing Key

### The original problem
Headscale's Noise key is a **Curve25519 (X25519)** key intended for ECDH key
exchange, **not** an Ed25519 signing key. An earlier design tried to have the
sidecar *sign* the challenge nonce, which is impossible with an X25519 key
without either a separate Ed25519 key or a Curve25519→Ed25519 conversion.

### The solution (implemented)
Instead of signing, registration uses **X25519 ECDH + HMAC-SHA256**, which uses
the existing Noise key directly and introduces **no new key material**:

1. On `init`, the proxy generates a per-challenge **ephemeral X25519 keypair**
   and returns its public key (`proxyPublicKey`) alongside the nonce.
2. The sidecar computes `sharedSecret = X25519(noisePrivateKey, proxyPublicKey)`
   and returns `proof = HMAC-SHA256(sharedSecret, nonce)`.
3. On `verify`, the proxy computes the same secret as
   `X25519(ephemeralPrivateKey, sidecarPublicKey)` and checks the HMAC in
   constant time.

Because ECDH is symmetric, only the holder of Headscale's Noise **private** key
can produce a matching proof — giving cryptographic proof of ownership without a
signing key, key conversion, or any Headscale modifications.

This is implemented in both `headfwd-sidecar/main.go` and both proxy backends
(`headfwd-proxy-fly/main.go`, `headfwd-proxy/src/index.ts`). There is no longer a
"legacy" unauthenticated registration endpoint.

## Storage

- **Fly.io backend**: in-memory maps in a single proxy process. If the proxy
  restarts, the sidecar re-registers automatically.
- **Cloudflare Workers backend**: `REGISTRY` KV — `challenge:{fingerprint}`
  (5-minute TTL) and `tunnel:{fingerprint}` (1-year TTL).

## Security Properties

- ✅ Unique, collision-resistant 128-bit fingerprints
- ✅ Cryptographic proof of Headscale ownership (ECDH + HMAC)
- ✅ Single-use, time-limited (5 min) challenge nonces
- ✅ No subdomain enumeration without the private key
- ✅ No new secrets to manage — the Noise key is reused
- 🔲 Rate limiting on registration endpoints (future hardening)

## See Also

- `docs/REGISTRATION.md` — full protocol description
- `docs/ios-key-verification.md` — client-side Noise-key verification and pinning
