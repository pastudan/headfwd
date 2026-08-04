package main

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Registration proof crypto. MUST match headfwd-proxy-fly/registration_crypto.go
// byte for byte — the relay recomputes this and compares, so any divergence
// makes every registration fail.
//
// See the relay's copy for why v1 (raw ECDH output as an HMAC key over the bare
// nonce) was replaced.

// ProtocolVersion is the registration protocol this sidecar speaks.
const ProtocolVersion = 2

// hkdfInfo domain-separates this derivation from any other use of the same
// shared secret. Changing this string is a breaking protocol change.
const hkdfInfo = "headfwd registration proof v2"

// deriveProofKey turns a raw ECDH shared secret into a uniformly random MAC key,
// salted with the relay's single-use nonce.
func deriveProofKey(sharedSecret []byte, nonce string) ([]byte, error) {
	return hkdf.Key(sha256.New, sharedSecret, []byte(nonce), hkdfInfo, 32)
}

// registrationProof computes the v2 proof over the full registration transcript.
func registrationProof(sharedSecret []byte, fingerprint, publicKey, nonce, ephemeralPubHex string) (string, error) {
	key, err := deriveProofKey(sharedSecret, nonce)
	if err != nil {
		return "", err
	}

	h := hmac.New(sha256.New, key)
	h.Write(transcript(fingerprint, publicKey, nonce, ephemeralPubHex))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// transcript serialises the registration context unambiguously, length-prefixing
// every field so two distinct transcripts cannot serialise identically.
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
