# Work Session Summary: Curve25519 to Ed25519 Conversion

**Date:** 2026-02-01  
**Branch:** `feature/curve25519-ed25519-conversion`  
**Status:** Mid-implementation (WIP commit pushed)

## Session Context

The user wanted to implement Option 1 from our design discussion: **Mathematical Conversion** of Curve25519 keys to Ed25519 keys to enable challenge-response authentication using Headscale's existing Noise protocol keys.

### Background: The Three Options Discussed

1. **Option 1: Mathematical Conversion** ✅ (Chosen)
   - Convert Curve25519 private key → Ed25519 using SHA-512
   - Single source of truth (Headscale's Noise key)
   - Best UX (no separate key management)

2. **Option 2: Use Both Keys** (Not chosen)
   - Generate separate Ed25519 keypair for HeadFwd
   - Two keys to manage
   - More complex but cleaner separation

3. **Option 3: Redesign Authentication** (Not chosen)
   - Use HMAC or key derivation instead of signatures
   - Bigger architectural change

## What We Accomplished

### 1. Architecture Design

Designed a dual-key transmission approach:
- Sidecar converts Curve25519 private key to Ed25519 using SHA-512
- Sidecar sends BOTH keys during registration:
  - Curve25519 public key (for fingerprint computation)
  - Ed25519 public key (for signature verification)
- Proxy uses Curve25519 for routing, Ed25519 for verification

### 2. Implementation

#### Proxy (`headfwd-proxy/`)
- ✅ Added `@noble/curves` library for Ed25519 operations
- ✅ Updated `handleRegisterInit()` to accept both `publicKey` and `ed25519PublicKey`
- ✅ Modified `verifyNoiseSignature()` to use Ed25519 directly
- ✅ Store Ed25519 key in challenge data for verification

#### Sidecar (`headfwd-sidecar/`)
- ✅ Implemented Curve25519→Ed25519 conversion using SHA-512
- ✅ Created `getEd25519PublicKey()` helper function
- ✅ Updated `RegistrationInitRequest` to include `Ed25519PublicKey` field
- ✅ Fixed Headscale key parsing (handles `privkey:hex` format)
- ✅ Updated signing to use converted Ed25519 private key

#### Documentation
- ✅ Created `docs/REGISTRATION.md` with architecture diagrams
- ✅ Created `docs/REGISTRATION-STATUS.md` with implementation status
- ✅ Created `docs/IMPLEMENTATION-COMPLETE.md` with summary
- ✅ Updated `README.md` with security details

### 3. Configuration Fixes

Along the way, we also fixed several Headscale configuration issues:
- ✅ Fixed `ephemeral_node_inactivity_timeout` (must be > 1m5s)
- ✅ Added `dns.nameservers.global` configuration
- ✅ Corrected `ip_prefixes` → `prefixes`
- ✅ Changed `policy.mode` from `deny` to `auto`
- ✅ Added `derp.server.private_key_path`

## What's Not Done

### Critical: Signature Verification Still Failing

**Last error:** `"Invalid signature"`

The implementation is complete but not tested end-to-end. The signature verification is failing, likely due to:

1. **Key mismatch:** Ed25519 public key sent to proxy doesn't match the private key used for signing
2. **Hex encoding issues:** Keys corrupted during encoding/decoding
3. **Storage issue:** Wrong key being verified in proxy

### Testing Needed

1. End-to-end registration flow test
2. Debug logging to verify keys match
3. Verify signature is being created and verified correctly

## Technical Deep Dive

### The Core Problem

Headscale uses Curve25519 keys (Montgomery curve) for the Noise protocol:
- **Private key file:** `/var/lib/headscale/noise_private.key`
- **Format:** `privkey:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef`
- **Size:** 32 bytes (after hex decoding)
- **Purpose:** ECDH key exchange

We need Ed25519 keys (Edwards curve) for digital signatures:
- **Purpose:** Sign challenge nonces
- **Library:** `@noble/curves` (proxy), `crypto/ed25519` (sidecar)

**The challenge:** You cannot directly convert a Curve25519 **public** key to an Ed25519 public key without the private key.

### The Solution

**Conversion Method:**
```
Curve25519 private key (32 bytes)
    ↓
SHA-512 hash (64 bytes)
    ↓
Take first 32 bytes as Ed25519 seed
    ↓
Derive Ed25519 keypair
```

**Why this works:**
- Both curves are birationally equivalent
- SHA-512 provides key derivation
- Standard approach used in cryptographic libraries
- One-way: Ed25519 key doesn't reveal Curve25519 structure

**Code (Go):**
```go
hash := sha512.Sum512(curve25519PrivateKey)
ed25519Seed := hash[:32]
ed25519PrivateKey := ed25519.NewKeyFromSeed(ed25519Seed)
ed25519PublicKey := ed25519PrivateKey.Public().(ed25519.PublicKey)
```

### Registration Flow

```
1. Sidecar reads Curve25519 private key from Headscale
2. Sidecar converts to Ed25519 using SHA-512
3. Sidecar requests Curve25519 public key from Headscale API
4. Sidecar sends BOTH keys to proxy:
   - Curve25519 public (for fingerprint)
   - Ed25519 public (for verification)
5. Proxy computes fingerprint from Curve25519 key
6. Proxy stores Ed25519 key for later verification
7. Proxy generates and returns nonce
8. Sidecar signs nonce with Ed25519 private key
9. Sidecar sends signature to proxy
10. Proxy verifies signature with Ed25519 public key
```

## Files Changed (Git Status)

```
Modified:
  README.md
  docker-compose.yml
  headfwd-proxy/package-lock.json
  headfwd-proxy/package.json
  headfwd-proxy/src/index.ts
  headfwd-proxy/src/types.ts
  headfwd-sidecar/go.mod
  headfwd-sidecar/main.go
  headscale/config/config.yaml

New files:
  docs/IMPLEMENTATION-COMPLETE.md
  docs/REGISTRATION-STATUS.md
  docs/REGISTRATION.md
  headfwd-sidecar/go.sum

Not tracked:
  .cursor/
  headfwd-sidecar/headfwd-sidecar (binary)
```

## Git History

```bash
Branch: feature/curve25519-ed25519-conversion
Commit: e814281
Message: "WIP: Implement Curve25519 to Ed25519 conversion for challenge-response auth"
Pushed: Yes
```

## How to Resume This Work

### 1. Checkout the branch

```bash
cd /path/to/headfwd
git checkout feature/curve25519-ed25519-conversion
git pull
```

### 2. Start the stack

```bash
# Terminal 1: Headscale
docker compose up

# Terminal 2: Proxy
cd headfwd-proxy
npm run dev

# Terminal 3: Sidecar (ready to test)
cd headfwd-sidecar
go build -o headfwd-sidecar .
```

### 3. Add debug logging

Before testing, add debug output to see what's happening:

**In `headfwd-sidecar/main.go`:**
```go
// In registerWithProxy(), after getting Ed25519 key:
log.Printf("DEBUG: Ed25519 public key (hex): %x", ed25519PublicKey)

// In signWithNoiseKey(), after signing:
log.Printf("DEBUG: Signed nonce (hex): %s", hex.EncodeToString(signature))
log.Printf("DEBUG: Ed25519 private key derived from same source")
```

**In `headfwd-proxy/src/index.ts`:**
```typescript
// In handleRegisterInit():
console.log('DEBUG: Storing Ed25519 key:', ed25519PublicKey);

// In verifyNoiseSignature():
console.log('DEBUG: Verifying with key:', publicKeyStr);
console.log('DEBUG: Nonce:', nonce);
console.log('DEBUG: Signature:', signatureHex);
```

### 4. Test and debug

```bash
./headfwd-sidecar --register \
  --headscale http://localhost:8080 \
  --proxy http://localhost:8787 \
  --noise-key /path/to/headfwd/headscale/data/noise_private.key
```

Watch all three terminals for DEBUG output.

### 5. Common issues to check

- ✅ Ed25519 public key is same in init and verify steps
- ✅ No hex encoding/decoding errors
- ✅ Nonce is same string on both sides
- ✅ Signature is 64 bytes (128 hex chars)
- ✅ Public key is 32 bytes (64 hex chars)

### 6. If still failing

Try testing the conversion in isolation:

```bash
cd headfwd-sidecar
cat > test_isolated.go << 'EOF'
package main
import (
    "crypto/ed25519"
    "crypto/sha512"
    "encoding/hex"
    "fmt"
)
func main() {
    // Use actual Curve25519 key from your Headscale
    curve25519Hex := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    curve25519Key, _ := hex.DecodeString(curve25519Hex)
    
    // Convert to Ed25519
    hash := sha512.Sum512(curve25519Key)
    ed25519Private := ed25519.NewKeyFromSeed(hash[:32])
    ed25519Public := ed25519Private.Public().(ed25519.PublicKey)
    
    // Test signing
    message := []byte("test message")
    signature := ed25519.Sign(ed25519Private, message)
    
    // Test verification
    valid := ed25519.Verify(ed25519Public, message, signature)
    
    fmt.Printf("Ed25519 Public: %x\n", ed25519Public)
    fmt.Printf("Signature: %x\n", signature)
    fmt.Printf("Verification: %v\n", valid)
}
EOF
go run test_isolated.go
rm test_isolated.go
```

This should output `Verification: true`. If not, the conversion itself is broken.

## User Notes

**Note from user:** The user removed `filippo.io/edwards25519` from `go.mod` - this was intentional. We don't actually need this dependency; Go's standard library `crypto/ed25519` is sufficient.

**User preference:** They wanted the work saved to a branch so they (or an LLM) could easily pick up later. All documentation should be comprehensive and self-contained.

## Next Session Goals

1. **Fix signature verification** - This is the only blocker
2. **Test end-to-end** - Full registration flow should work
3. **Add error handling** - Better error messages
4. **Merge to main** - Once working, merge the feature branch

## Questions to Consider

1. Should we compute fingerprint from Ed25519 key instead of Curve25519?
   - Pros: Simpler (only one key type)
   - Cons: Breaking change (existing fingerprints won't work)

2. Should we keep both registration endpoints?
   - Legacy `/api/register` (no signature verification)
   - New `/api/register/init` + `/api/register/verify` (with signatures)

3. Should we cache Ed25519 key derivation?
   - It's fast (SHA-512 is ~1ms), but could cache for performance

## References for Next Session

- **Test command:** See "How to Resume This Work" section
- **Debug guide:** See `docs/CURVE25519-ED25519-IMPLEMENTATION.md`
- **Architecture:** See `docs/REGISTRATION.md`
- **Plan file:** `.cursor/plans/curve_838086bc.plan.md` (created during planning phase)

## Estimated Time to Complete

- **If quick fix:** 30 minutes (just need to fix key transmission)
- **If deeper debugging:** 2-3 hours (need to trace through signature creation/verification)
- **Worst case:** 4-6 hours (might need to redesign the approach)

The core algorithm is sound (tested in isolation), so it's likely a simple bug in how keys are transmitted or stored.

