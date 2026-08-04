package main

import "testing"

// This vector MUST match the one pinned in
// headfwd-proxy-fly/registration_crypto_test.go.
//
// The two components carry independent copies of the registration crypto and
// never share a package. If they drift, every registration fails with "invalid
// proof" and nothing points at the cause. This test is the tripwire: change one
// copy without the other and it fails here, at build time, with an explanation.
const pinnedProofVector = "f2740fc7e836710796c496894e8bc4e42e6c323aabc4c8ca7f4d983c3f1c6f9a"

func TestProofMatchesRelayVector(t *testing.T) {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i)
	}

	got, err := registrationProof(secret, "fp", "mkey:aa", "nonce", "eph")
	if err != nil {
		t.Fatal(err)
	}
	if got != pinnedProofVector {
		t.Fatalf(
			"sidecar and relay disagree on the registration proof:\n"+
				"  sidecar %s\n  relay   %s\n"+
				"Both copies of registration_crypto.go must be identical.",
			got, pinnedProofVector)
	}
}

func TestDerivedKeyIsNotTheSharedSecret(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	key, err := deriveProofKey(secret, "nonce")
	if err != nil {
		t.Fatal(err)
	}
	if string(key) == string(secret) {
		t.Fatal("HKDF returned the input unchanged — this was the v1 bug")
	}
}

func TestProofBindsTranscript(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	base, err := registrationProof(secret, "fp", "mkey:aa", "nonce", "eph")
	if err != nil {
		t.Fatal(err)
	}
	// Changing the ephemeral key alone must change the proof; under v1 it did
	// not, because the MAC covered only the nonce.
	other, err := registrationProof(secret, "fp", "mkey:aa", "nonce", "different")
	if err != nil {
		t.Fatal(err)
	}
	if base == other {
		t.Fatal("ephemeral public key is not bound into the proof")
	}
}
