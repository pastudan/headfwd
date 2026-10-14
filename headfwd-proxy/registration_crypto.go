package main

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
)

// Registration proof crypto, shared in spirit with headfwd-sidecar.
//
// Both sides must compute this identically; if you change anything here, change
// the matching block in headfwd-sidecar/main.go and bump ProtocolVersion.

// ProtocolVersion is the registration protocol the relay speaks.
//
// v2 replaced the v1 proof, which used the raw X25519 ECDH output directly as
// an HMAC key over the bare nonce. Three problems with that:
//
//   - The ECDH output is not a uniformly random key. It is a curve point with
//     algebraic structure, and using it directly as keying material is the
//     mistake HKDF exists to prevent.
//   - The same long-term Noise static key is reused across protocols, so a
//     chosen-input MAC oracle on it is a cross-protocol risk.
//   - The MAC covered only the nonce, binding the proof to nothing else — not
//     the fingerprint it authorises, not the ephemeral key it was derived
//     against, not even a protocol label.
//
// v2 runs the shared secret through HKDF-SHA256 with a domain-separation label
// and binds the full transcript into the MAC. There is no v1 fallback: a
// downgrade path would let an attacker who can MITM the (plain HTTP) init
// response force the weaker proof, which defeats the point.
const ProtocolVersion = 2

// hkdfInfo domain-separates this derivation from any other use of the same
// shared secret. Changing this string is a breaking protocol change.
const hkdfInfo = "headfwd registration proof v2"

// deriveProofKey turns a raw ECDH shared secret into a uniformly random MAC key.
//
// The nonce doubles as the HKDF salt. It is server-generated, random, and
// single-use, which is exactly what a salt wants to be, and it means a replayed
// nonce cannot produce a replayed key.
func deriveProofKey(sharedSecret []byte, nonce string) ([]byte, error) {
	return hkdf.Key(sha256.New, sharedSecret, []byte(nonce), hkdfInfo, 32)
}

// registrationProof computes the v2 proof.
//
// The MAC covers the whole transcript — protocol version, fingerprint, claimed
// public key, nonce, and the relay's ephemeral public key — with each field
// length-prefixed so no two distinct transcripts can serialise the same way.
// v1 covered only the nonce, so a valid proof said nothing about *which*
// registration it authorised.
func registrationProof(sharedSecret []byte, fingerprint, publicKey, nonce, ephemeralPubHex string) (string, error) {
	key, err := deriveProofKey(sharedSecret, nonce)
	if err != nil {
		return "", err
	}

	mac := hmacSHA256(key, transcript(fingerprint, publicKey, nonce, ephemeralPubHex))
	return hex.EncodeToString(mac), nil
}

// transcript serialises the registration context unambiguously.
//
// Every field is length-prefixed. Plain concatenation would let ("ab","c") and
// ("a","bc") collide, which is a real (if fiddly) way to make one proof valid
// for a different registration.
func transcript(fingerprint, publicKey, nonce, ephemeralPubHex string) []byte {
	var buf []byte
	appendField := func(s string) {
		var n [4]byte
		l := len(s)
		n[0] = byte(l >> 24)
		n[1] = byte(l >> 16)
		n[2] = byte(l >> 8)
		n[3] = byte(l)
		buf = append(buf, n[:]...)
		buf = append(buf, s...)
	}
	appendField(hkdfInfo)
	appendField(fingerprint)
	appendField(publicKey)
	appendField(nonce)
	appendField(ephemeralPubHex)
	return buf
}
