# Never Trust the Proxy

This project treats the proxy as an untrusted relay. All security- and identity-critical values must be derived locally and verified end-to-end.

Core requirements:

- Derive the fingerprint locally from Headscale's Noise public key (`/key?v=96`).
- Never accept a proxy-provided fingerprint or URL without verifying it against local derivation.
- The proxy may return tunnel/public URLs for convenience, but the sidecar must validate them.
- Any client-facing documentation must assume the proxy is untrusted.

If a change cannot satisfy these requirements, it should be redesigned or rejected.
