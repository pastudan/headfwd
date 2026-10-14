package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

// The relay and the sidecar each carry their own copy of this crypto. These
// tests pin the values that both must produce; if the two copies drift, the
// pinned vector below fails here and the interop test in the sidecar fails
// there, rather than every registration silently failing in production.

func TestProofIsDeterministicAndPinned(t *testing.T) {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i)
	}
	// Pinned vector. The sidecar's copy of this crypto pins the SAME value, so
	// if the two implementations ever drift this fails here and there rather
	// than as an unexplained registration failure in production.
	const wantMAC = "f2740fc7e836710796c496894e8bc4e42e6c323aabc4c8ca7f4d983c3f1c6f9a"

	got, err := registrationProof(secret, "fp", "mkey:aa", "nonce", "eph")
	if err != nil {
		t.Fatal(err)
	}
	if got != wantMAC {
		t.Fatalf("proof changed:\n  got  %s\n  want %s\nIf this is intentional, bump ProtocolVersion and update the sidecar.", got, wantMAC)
	}

	// Determinism: same inputs, same output.
	again, err := registrationProof(secret, "fp", "mkey:aa", "nonce", "eph")
	if err != nil {
		t.Fatal(err)
	}
	if got != again {
		t.Fatal("proof is not deterministic")
	}
}

// Every field must actually be bound into the MAC. v1 covered only the nonce,
// so a proof said nothing about which registration it authorised — this is the
// regression test for that.
func TestProofBindsEveryField(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	base := []string{"fp", "mkey:aa", "nonce", "eph"}

	proofFor := func(f []string) string {
		p, err := registrationProof(secret, f[0], f[1], f[2], f[3])
		if err != nil {
			t.Fatal(err)
		}
		return p
	}

	original := proofFor(base)
	names := []string{"fingerprint", "publicKey", "nonce", "ephemeralPub"}

	for i := range base {
		altered := append([]string(nil), base...)
		altered[i] += "X"
		if proofFor(altered) == original {
			t.Errorf("changing %s did not change the proof — it is not bound into the MAC", names[i])
		}
	}
}

// Length-prefixing must stop two different field splits producing the same
// transcript. Without it, ("ab","c") and ("a","bc") collide.
func TestTranscriptIsUnambiguous(t *testing.T) {
	a := transcript("ab", "c", "nonce", "eph")
	b := transcript("a", "bc", "nonce", "eph")
	if string(a) == string(b) {
		t.Fatal("transcript is ambiguous: distinct fields serialised identically")
	}
}

// The derived key must not be the raw ECDH output — that was the v1 bug.
func TestDerivedKeyDiffersFromSharedSecret(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	key, err := deriveProofKey(secret, "nonce")
	if err != nil {
		t.Fatal(err)
	}
	if string(key) == string(secret) {
		t.Fatal("HKDF returned the input unchanged")
	}
	if len(key) != 32 {
		t.Fatalf("want a 32-byte key, got %d", len(key))
	}

	// A different nonce (salt) must give a different key, so one registration's
	// proof key cannot be reused for another.
	other, err := deriveProofKey(secret, "different-nonce")
	if err != nil {
		t.Fatal(err)
	}
	if string(key) == string(other) {
		t.Fatal("nonce is not salting the derivation")
	}
}

// End-to-end against real X25519: both sides derive the same secret from their
// own private key and the peer's public key, so both must land on the same proof.
func TestRelayAndSidecarAgreeOverRealECDH(t *testing.T) {
	curve := ecdh.X25519()

	sidecarPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	relayPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	sidecarSecret, err := sidecarPriv.ECDH(relayPriv.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	relaySecret, err := relayPriv.ECDH(sidecarPriv.PublicKey())
	if err != nil {
		t.Fatal(err)
	}

	pubKey := "mkey:" + hex.EncodeToString(sidecarPriv.PublicKey().Bytes())
	fp := computeFingerprint(pubKey)
	nonce := randomHex(32)
	ephHex := hex.EncodeToString(relayPriv.PublicKey().Bytes())

	fromSidecar, err := registrationProof(sidecarSecret, fp, pubKey, nonce, ephHex)
	if err != nil {
		t.Fatal(err)
	}
	fromRelay, err := registrationProof(relaySecret, fp, pubKey, nonce, ephHex)
	if err != nil {
		t.Fatal(err)
	}

	if fromSidecar != fromRelay {
		t.Fatalf("proofs disagree:\n  sidecar %s\n  relay   %s", fromSidecar, fromRelay)
	}

	// A different keypair must not produce an accepted proof.
	impostorPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	impostorSecret, err := impostorPriv.ECDH(relayPriv.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	impostor, err := registrationProof(impostorSecret, fp, pubKey, nonce, ephHex)
	if err != nil {
		t.Fatal(err)
	}
	if impostor == fromRelay {
		t.Fatal("a party without the private key produced a valid proof")
	}
}
