# Registration Implementation Status

## ✅ Completed

1. **Hex32 Fingerprints** - 128-bit collision-resistant subdomain identifiers
2. **Two-Phase Registration** - `/api/register/init` and `/api/register/verify` endpoints
3. **Challenge-Response Protocol** - Nonce generation and expiration (5 minutes)
4. **In-Memory Storage** - Local dev support without KV namespace
5. **Sidecar Auto-Registration** - `--register` flag with automatic key detection
6. **Documentation** - Comprehensive architecture docs in `docs/REGISTRATION.md`
7. **README Updates** - Security section and registration flow documented

## ⚠️ Known Limitation: Signature Verification

### Issue
The Noise protocol key used by Headscale is a **Curve25519** key (for ECDH key exchange), not an **Ed25519** key (for signing). While these curves are related, Curve25519 keys cannot directly sign messages.

### Current Behavior
- Registration init works ✅
- Challenge nonce generation works ✅
- Signature verification fails ❌ (Curve25519 vs Ed25519 mismatch)

### Solutions

#### Option 1: Use Legacy Registration (Temporary)
For now, use the legacy `/api/register` endpoint which doesn't require signature verification:

```bash
PUBKEY=$(curl -s "http://localhost:8080/key?v=96" | jq -r .publicKey)
curl -X POST http://localhost:8787/api/register \
  -H "Content-Type: application/json" \
  -d "{\"pubkey\": \"$PUBKEY\"}" | jq .
```

#### Option 2: Generate Separate Signing Key (Recommended for Production)
Create a dedicated Ed25519 keypair for HeadFwd registration:

```bash
# Generate Ed25519 keypair
openssl genpkey -algorithm ED25519 -out headfwd_private.key
openssl pkey -in headfwd_private.key -pubout -out headfwd_public.key

# Use this for registration instead of Noise key
```

#### Option 3: Implement Curve25519-to-Ed25519 Conversion
There's a mathematical relationship between Curve25519 and Ed25519 keys that allows conversion. This requires:
- Implementing the conversion algorithm in both Go (sidecar) and TypeScript (proxy)
- Using libraries like `@noble/curves` or `golang.org/x/crypto/curve25519`

### Recommended Path Forward

**For MVP/Testing:**
- Use legacy `/api/register` endpoint (no signature verification)
- Still provides unique fingerprints and tunnel secrets
- Good enough for private deployments

**For Production:**
- Implement Option 2 (separate Ed25519 keypair)
- Store HeadFwd signing key alongside Noise key
- Update sidecar to use HeadFwd key for registration
- Keeps Noise key unchanged (no Headscale modifications needed)

### Code Changes Needed for Option 2

**1. Generate HeadFwd Keypair During Setup:**
```bash
# In headscale/data/ directory
openssl genpkey -algorithm ED25519 -out headfwd_signing.key
openssl pkey -in headfwd_signing.key -pubout -out headfwd_signing.pub
```

**2. Update Sidecar to Use HeadFwd Key:**
```go
// Instead of reading Noise key, read HeadFwd signing key
keyPath := "/var/lib/headscale/headfwd_signing.key"
```

**3. Update Registration Init:**
```bash
# Send HeadFwd public key instead of Noise public key
PUBKEY=$(cat /var/lib/headscale/headfwd_signing.pub | base64)
curl -X POST /api/register/init -d "{\"publicKey\": \"$PUBKEY\"}"
```

## Testing Status

### ✅ Working
- Proxy health endpoint
- Fingerprint computation (hex32)
- Challenge generation and storage
- Sidecar auto-registration flow
- In-memory storage for local dev

### ❌ Not Working
- Ed25519 signature verification (key type mismatch)

### 🔄 Workaround
- Use legacy `/api/register` endpoint
- Works for testing and private deployments
- Provides unique fingerprints without signature verification

## Next Steps

1. **Short Term**: Document legacy registration as the recommended approach
2. **Medium Term**: Implement Option 2 (separate Ed25519 keypair)
3. **Long Term**: Consider if signature verification is necessary for the threat model

## Security Implications

**Without Signature Verification:**
- ✅ Still have unique, collision-resistant fingerprints
- ✅ Still have tunnel secrets for authentication
- ✅ Still prevent subdomain enumeration (128-bit fingerprints)
- ❌ Anyone with the public key can register (but they need it first)
- ❌ No cryptographic proof of Headscale ownership

**Risk Assessment:**
- **Low Risk** for private deployments (you control who knows the public key)
- **Medium Risk** for public proxy (anyone can register if they have a Headscale)
- **Mitigation**: Rate limiting + monitoring on registration endpoints

## Conclusion

The challenge-response architecture is sound, but requires either:
1. A separate Ed25519 signing key (clean solution)
2. Curve25519-to-Ed25519 conversion (complex but elegant)
3. Accept legacy registration without signature verification (pragmatic for now)

For the current stage of development, **Option 3 (legacy registration) is recommended** to unblock testing while we decide on the long-term approach.

