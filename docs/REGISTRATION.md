# HeadFwd Registration Architecture

## Overview

HeadFwd uses a two-phase challenge-response authentication protocol to secure registration and prevent unauthorized subdomain enumeration. This ensures only legitimate Headscale instances with valid Noise private keys can register tunnels.

## Security Goals

1. **Proof of Ownership**: Only someone with access to Headscale's Noise private key can complete registration
2. **No Subdomain Enumeration**: Can't guess valid subdomains without the private key
3. **Time-Limited Challenges**: Nonces expire after 5 minutes to prevent replay attacks
4. **Replay Protection**: Each nonce is single-use
5. **Collision Resistance**: 128-bit fingerprints provide ~3.4×10³⁸ unique values

## Registration Flow

```mermaid
sequenceDiagram
    participant Sidecar
    participant Proxy
    participant DO as DurableObject
    
    Note over Sidecar: Get Noise public key<br/>from Headscale
    Sidecar->>Proxy: POST /api/register/init<br/>{publicKey: "mkey:..."}
    Proxy->>Proxy: Compute fingerprint<br/>(SHA256 → hex32)
    Proxy->>Proxy: Generate nonce
    Proxy->>Proxy: Store challenge in KV<br/>(fingerprint → nonce)
    Proxy-->>Sidecar: {fingerprint, nonce, expiresAt}
    
    Note over Sidecar: Sign nonce with<br/>Noise private key
    Sidecar->>Proxy: POST /api/register/verify<br/>{fingerprint, signature}
    Proxy->>Proxy: Lookup nonce from KV
    Proxy->>Proxy: Verify signature using<br/>publicKey from init
    Proxy->>Proxy: Generate tunnel secret
    Proxy->>Proxy: Store in KV:<br/>fingerprint → secret
    Proxy-->>Sidecar: {tunnelUrl, publicUrl}
    
    Sidecar->>DO: Connect WebSocket<br/>ws://fingerprint.headfwd.net/tunnel
    DO->>DO: Verify auth secret
    DO-->>Sidecar: Connection established
```

## Subdomain Fingerprinting

### Format
- **Encoding**: Hex (32 characters)
- **Bits**: 128 bits of security
- **Source**: First 128 bits of SHA256(Noise public key)
- **Example**: `b4ff5ae7bbf053c9bd58d16df68702e0.headfwd.net`

### Why Hex32?
1. **Universal Support**: Native hex encoding on all platforms (Go, JS, Swift, Kotlin)
2. **No Dependencies**: No external libraries needed
3. **DNS Compatible**: Works in subdomains without issues
4. **Collision Resistant**: ~3.4×10³⁸ possible values (2^128)
5. **Case Insensitive**: Works consistently across systems

### Security Properties
- **Collision Probability**: ~1 in 340 undecillion
- **Brute Force**: Would take billions of years with current computing
- **Enumeration**: Cannot guess valid subdomains without the private key

## Authentication Protocol

### Phase 1: Challenge Initialization

**Request:**
```bash
POST /api/register/init
Content-Type: application/json

{
  "publicKey": "mkey:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
}
```

**Response:**
```json
{
  "fingerprint": "b4ff5ae7bbf053c9bd58d16df68702e0",
  "nonce": "a1b2c3d4e5f6789...",
  "expiresAt": 1738449600000
}
```

**What Happens:**
1. Proxy computes `fingerprint = SHA256(publicKey).substring(0, 32)`
2. Proxy generates random 32-byte nonce
3. Proxy stores `{publicKey, nonce}` in KV with 5-minute TTL
4. Returns challenge to sidecar

### Phase 2: Signature Verification

**Request:**
```bash
POST /api/register/verify
Content-Type: application/json

{
  "fingerprint": "b4ff5ae7bbf053c9bd58d16df68702e0",
  "signature": "a1b2c3d4..."
}
```

**Response:**
```json
{
  "fingerprint": "b4ff5ae7bbf053c9bd58d16df68702e0",
  "tunnelUrl": "wss://b4ff5ae7bbf053c9bd58d16df68702e0.headfwd.net/tunnel?auth=SECRET",
  "publicUrl": "https://b4ff5ae7bbf053c9bd58d16df68702e0.headfwd.net"
}
```

**What Happens:**
1. Proxy retrieves stored challenge for fingerprint
2. Proxy verifies `Ed25519.verify(publicKey, nonce, signature)`
3. If valid:
   - Generate tunnel secret
   - Store `{publicKey, secret}` in KV with 1-year TTL
   - Delete challenge
   - Return tunnel credentials
4. If invalid: Return 403 Forbidden

## Client Verification (Custom Clients)

This section documents an **optional online proof** for custom clients that want to treat the proxy as a dumb forwarder.
It uses the existing Headscale Noise X25519 key and does **not** introduce new key material.

### Step 0: Verify Fingerprint Matches `/key`

1. Client fetches the public key from the Headscale control server:
   - `GET /key?v=96` → `mkey:...`
2. Client computes `fingerprint = SHA256(publicKey).substring(0, 32)`
3. Client verifies the fingerprint matches the subdomain it connected to.

If the fingerprint does **not** match, the client must abort. This detects a proxy misroute or key substitution.

### Step 1 (Planned): Online Attestation Using Noise Key

Add a small endpoint (e.g. `/api/attest`) on the Headscale sidecar to prove possession of the Noise private key.

**Request:**
```json
{
  "clientPublicKey": "<hex X25519 public key>",
  "nonce": "<random 32 bytes, hex>"
}
```

**Server behavior:**
1. Compute `sharedSecret = X25519(noisePrivateKey, clientPublicKey)`
2. Return `proof = HMAC-SHA256(sharedSecret, nonce)`

**Response:**
```json
{
  "proof": "<hex HMAC>"
}
```

**Client verification:**
1. Compute the same `sharedSecret = X25519(clientPrivateKey, serverPublicKey)`
2. Verify `HMAC-SHA256(sharedSecret, nonce)` matches the response

This provides an **interactive proof of key possession** without adding new keys.

## Cryptographic Details

### Key Types
- **Noise Protocol**: Headscale uses Noise_IK pattern with Curve25519/Ed25519
- **Public Key Format**: `mkey:` prefix + 64 hex characters (32 bytes)
- **Private Key**: 64 bytes (32-byte seed + 32-byte public key)

### Signature Scheme
- **Algorithm**: Ed25519 (Elliptic Curve Digital Signature Algorithm)
- **Message**: Raw nonce bytes (32 bytes)
- **Signature**: 64 bytes (encoded as 128 hex characters)
- **Verification**: Standard Ed25519.verify()

### Storage
- **KV Namespace**: `REGISTRY` (Cloudflare Workers KV)
- **Challenge Key**: `challenge:{fingerprint}` (TTL: 5 minutes)
- **Registration Key**: `tunnel:{fingerprint}` (TTL: 1 year)

## Sidecar Implementation

### Command-Line Usage

**Manual Tunnel URL:**
```bash
./headfwd-sidecar \
  --tunnel "wss://b4ff5ae7.headfwd.net/tunnel?auth=SECRET" \
  --headscale "http://localhost:8080"
```

**Auto-Registration:**
```bash
./headfwd-sidecar \
  --register \
  --proxy "https://headfwd.net" \
  --headscale "http://localhost:8080" \
  --noise-key "/var/lib/headscale/noise_private.key"
```

### Docker Compose

```yaml
headfwd-sidecar:
  build: ./headfwd-sidecar
  environment:
    - AUTO_REGISTER=true
    - PROXY_URL=https://headfwd.net
    - HEADSCALE_URL=http://headscale:8080
  volumes:
    - ./headscale/data:/keys:ro
```

### Key Auto-Detection
The sidecar automatically searches for Noise private key in:
1. Path specified by `--noise-key` flag
2. `/var/lib/headscale/noise_private.key`
3. `/keys/noise_private.key` (Docker mount)
4. `./headscale/data/noise_private.key` (local dev)

## Attack Resistance

### Subdomain Enumeration
- **Attack**: Guess valid subdomains by trying random fingerprints
- **Defense**: 
  - 128-bit fingerprints = 2^128 possibilities
  - Without private key, cannot generate valid signatures
  - Challenge-response prevents unauthorized registration

### Replay Attacks
- **Attack**: Reuse captured nonce/signature pairs
- **Defense**:
  - Nonces are single-use (deleted after verification)
  - 5-minute expiration on challenges
  - New signature required for each registration

### Man-in-the-Middle
- **Attack**: Intercept registration traffic
- **Defense**:
  - All production traffic over TLS (wss://, https://)
  - Signature verification prevents tampering
  - Public key cryptography ensures authenticity

### Brute Force
- **Attack**: Try to brute force private keys
- **Defense**:
  - Ed25519 is quantum-resistant up to ~128-bit security level
  - Private keys are 256-bit, brute force infeasible
  - Rate limiting on registration endpoints (TODO)

## Comparison with Alternatives

| Approach | Security | Complexity | Enumeration Risk |
|----------|----------|------------|------------------|
| **Challenge-Response (Current)** | High | Medium | None |
| Shared Secret | Medium | Low | Medium |
| Rate Limiting Only | Low | Low | High |
| OAuth/JWT | High | High | None |

## Future Enhancements

1. **Rate Limiting**: Add rate limits on `/api/register/init` (e.g., 10 req/min per IP)
2. **Key Rotation**: Support for rotating tunnel secrets
3. **Revocation**: API endpoint to revoke tunnel access
4. **Monitoring**: Track registration attempts and failed verifications
5. **Multi-Key**: Support for registering multiple Headscale instances with one account

## References

- [Noise Protocol Framework](http://www.noiseprotocol.org/)
- [Ed25519 Signature Scheme](https://ed25519.cr.yp.to/)
- [Cloudflare Workers KV](https://developers.cloudflare.com/kv/)
- [Headscale Documentation](https://headscale.net/)
