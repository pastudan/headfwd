import CryptoKit
import Foundation

/// Verifies that the Headscale Noise public key returned by the control server
/// matches the fingerprint embedded in the server URL, then writes the verified
/// key to `<stateDir>/pinned_server_key` so that libtailscale reads it from
/// disk instead of fetching it from the untrusted proxy.
///
/// ## Why this matters
///
/// The proxy is treated as potentially rogue (see NEVER-TRUST-PROXY rule).
/// Without this layer, tsnet independently fetches `/key?v=96` from the proxy
/// on every app launch with no verification. A rogue proxy could serve a
/// substitute Noise key, decrypt the Noise channel, and extract the preauth
/// key in plaintext — then register its own device on your Headscale instance.
///
/// This verifier closes that attack via two layers (see docs/ios-key-verification.md):
///
/// **Layer 1 (here):** Fetch `/key?v=96` over standard HTTPS, verify that
/// `SHA256(key)[:16 bytes as hex]` equals the fingerprint in the QR server URL.
/// SHA256 preimage resistance means the proxy cannot forge a key that passes
/// this check. The verified key is written to disk.
///
/// **Layer 2 (libtailscale patch):** `TsnetUp` reads `pinned_server_key` from
/// the state dir and starts a local HTTP interceptor that serves it for all
/// `/key` requests — so tsnet never asks the proxy for the key at all.
enum ServerKeyVerifier {

    enum VerificationError: LocalizedError {
        case badServerURL(String)
        case fetchFailed(String)
        case badKeyResponse
        case fingerprintMismatch(expected: String, got: String)
        case keyChanged(current: String)

        var errorDescription: String? {
            switch self {
            case .badServerURL(let u):
                return "Cannot parse server URL: \(u)"
            case .fetchFailed(let msg):
                return "Failed to fetch server key: \(msg)"
            case .badKeyResponse:
                return "Server returned an unexpected key format (expected mkey: prefix)"
            case .fingerprintMismatch(let expected, let got):
                return "Server key fingerprint mismatch — expected \(expected), got \(got). " +
                       "The server may be misconfigured or the proxy may be substituting a different key."
            case .keyChanged(let current):
                return "Server key has changed since last connection " +
                       "(current: \(current.prefix(20))…). " +
                       "Re-scan the QR code to re-establish trust, or contact your administrator."
            }
        }
    }

    // MARK: - Public

    /// Fetches the Noise public key from `<config.server>/key?v=96`, verifies
    /// it matches the fingerprint in the server URL, then writes it to
    /// `<stateDir>/pinned_server_key`.
    ///
    /// On first call the key is pinned (TOFU — trust on first use).
    /// On subsequent calls the live key is re-verified AND compared to the
    /// pinned value — a mismatch throws immediately so key substitution is
    /// surfaced before tsnet ever starts.
    ///
    /// After this returns successfully, the libtailscale interceptor in
    /// `TsnetUp` will find `pinned_server_key` and serve it for tsnet's own
    /// internal `/key` fetch, bypassing the proxy entirely.
    static func verify(config: HeadscaleConfig, stateDir: String) async throws {
        let expectedFingerprint = try parseFingerprint(from: config.server)
        let publicKey = try await fetchPublicKey(from: config.server)

        let computed = computeFingerprint(of: publicKey)
        guard computed == expectedFingerprint else {
            throw VerificationError.fingerprintMismatch(
                expected: expectedFingerprint, got: computed)
        }

        // TOFU: compare with any previously pinned key.
        let pinPath = (stateDir as NSString).appendingPathComponent("pinned_server_key")
        if let existing = try? String(contentsOfFile: pinPath, encoding: .utf8)
            .trimmingCharacters(in: .whitespacesAndNewlines),
           !existing.isEmpty,
           existing != publicKey {
            throw VerificationError.keyChanged(current: publicKey)
        }

        // Write (or refresh) the pinned key file.
        // libtailscale's TsnetUp patch reads this before calling tsnet.Server.Up(),
        // serving it via a local interceptor so the proxy never gets to respond.
        try publicKey.write(toFile: pinPath, atomically: true, encoding: .utf8)
    }

    // MARK: - Private helpers

    /// Extracts the 32-hex-char fingerprint from a server URL of the form
    /// `https://<fingerprint>.headfwd.net`.
    private static func parseFingerprint(from serverURL: String) throws -> String {
        guard let url = URL(string: serverURL), let host = url.host else {
            throw VerificationError.badServerURL(serverURL)
        }
        let subdomain = host.components(separatedBy: ".").first ?? ""
        guard subdomain.count == 32, subdomain.allSatisfy(\.isHexDigit) else {
            throw VerificationError.badServerURL(
                "no 32-hex fingerprint found in host: \(host)")
        }
        return subdomain
    }

    /// GETs `<serverURL>/key?v=96` and returns the raw `publicKey` string
    /// (e.g. `"mkey:18c68c3d..."`).
    private static func fetchPublicKey(from serverURL: String) async throws -> String {
        guard let url = URL(string: "\(serverURL)/key?v=96") else {
            throw VerificationError.badServerURL(serverURL)
        }
        let (data, response): (Data, URLResponse)
        do {
            (data, response) = try await URLSession.shared.data(from: url)
        } catch {
            throw VerificationError.fetchFailed(error.localizedDescription)
        }
        guard let http = response as? HTTPURLResponse, http.statusCode == 200 else {
            let code = (response as? HTTPURLResponse)?.statusCode ?? -1
            throw VerificationError.fetchFailed("HTTP \(code)")
        }
        struct KeyResponse: Decodable { let publicKey: String }
        guard let parsed = try? JSONDecoder().decode(KeyResponse.self, from: data),
              parsed.publicKey.hasPrefix("mkey:") else {
            throw VerificationError.badKeyResponse
        }
        return parsed.publicKey
    }

    /// Returns the first 32 hex characters of SHA256(utf8 bytes of `key`),
    /// which matches the fingerprint derivation used by the sidecar and proxy:
    ///
    ///     fingerprint = hex(SHA256("mkey:<64-hex-bytes>"))[:32]
    private static func computeFingerprint(of key: String) -> String {
        SHA256.hash(data: Data(key.utf8))
            .prefix(16)
            .map { String(format: "%02x", $0) }
            .joined()
    }
}
