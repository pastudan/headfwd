import Foundation
import TailscaleKit

/// Real Tailscale service backed by tsnet (userspace WireGuard).
/// On first connect, a preauth key registers the device with Headscale.
/// The key is single-use and discarded after registration; tsnet stores
/// its own WireGuard keypair in a UUID-stamped directory under Documents/
/// and reconnects on subsequent launches without needing a new key.
///
/// Why UUID-stamped directories?
/// libtailscale.a embeds the full Go runtime, which persists for the app's
/// lifetime. Closing a TailscaleNode and creating a new one with the *same*
/// path can allow the Go runtime to reuse the cached machine keypair from
/// memory — Headscale then assigns the same 100.64 IP. Using a fresh UUID
/// directory forces the Go library to initialise from empty state and
/// generate a genuinely new WireGuard keypair, guaranteeing a new IP.
@MainActor
final class RealTailscaleService: TailscaleServiceProtocol {
    @Published private(set) var connectionState: ConnectionState = .disconnected
    @Published private(set) var tailscaleIP: String?
    @Published private(set) var tailscaleHostname: String?

    private var node: TailscaleNode?

    private static let deviceHostname = "headfwd-ios"
    /// UserDefaults key that stores the current state-directory UUID.
    private static let stateIDKey = "headfwd_ts_state_id"

    // MARK: - State directory management

    /// One-time migration: if there is no UUID in UserDefaults yet but the
    /// legacy `Documents/tailscale/` directory exists (written by older builds),
    /// move it to a UUID-stamped path so existing keypairs are preserved.
    private static func migrateIfNeeded() {
        let docsDir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
        let legacyDir = docsDir.appendingPathComponent("tailscale")
        guard UserDefaults.standard.string(forKey: stateIDKey) == nil,
              FileManager.default.fileExists(atPath: legacyDir.path) else { return }
        let newID = UUID().uuidString
        let newDir = docsDir.appendingPathComponent("tailscale-\(newID)")
        if (try? FileManager.default.moveItem(at: legacyDir, to: newDir)) != nil {
            UserDefaults.standard.set(newID, forKey: stateIDKey)
        }
    }

    /// Returns the path to the current tsnet state directory, creating the
    /// UUID entry in UserDefaults if this is the first launch.
    private static func currentStateDirPath() -> String {
        let docsDir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
        let stateID: String
        if let stored = UserDefaults.standard.string(forKey: stateIDKey) {
            stateID = stored
        } else {
            stateID = UUID().uuidString
            UserDefaults.standard.set(stateID, forKey: stateIDKey)
        }
        return docsDir.appendingPathComponent("tailscale-\(stateID)").path
    }

    /// Rotates to a brand-new state UUID and deletes every old
    /// `tailscale-*` directory so no stale keys linger on disk.
    private static func rotateStateDir() {
        let docsDir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
        // Delete all previous UUID-stamped directories.
        if let entries = try? FileManager.default.contentsOfDirectory(
            at: docsDir, includingPropertiesForKeys: nil) {
            for entry in entries where entry.lastPathComponent.hasPrefix("tailscale-") {
                try? FileManager.default.removeItem(at: entry)
            }
        }
        // Also remove the legacy non-UUID path used by earlier builds.
        try? FileManager.default.removeItem(at: docsDir.appendingPathComponent("tailscale"))
        // Issue a new UUID; the next currentStateDirPath() call will persist it.
        UserDefaults.standard.removeObject(forKey: stateIDKey)
    }

    // MARK: - TailscaleServiceProtocol

    func connect(config: HeadscaleConfig) async throws {
        connectionState = .connecting

        // Migrate legacy Documents/tailscale/ → UUID-stamped path on first launch
        // after the UUID refactor, so existing keypairs aren't lost.
        Self.migrateIfNeeded()

        let tsDir = Self.currentStateDirPath()
        try FileManager.default.createDirectory(atPath: tsDir, withIntermediateDirectories: true)

        // Guard: no stored keypair AND no auth key means tsnet will block
        // indefinitely waiting for a login flow that never comes.  Fail fast
        // with a clear message so the user knows to scan a new QR code.
        let dirContents = (try? FileManager.default.contentsOfDirectory(atPath: tsDir)) ?? []
        if config.key == nil, dirContents.isEmpty {
            connectionState = .error("No stored credentials — scan a QR code to connect")
            throw NSError(domain: "HeadFwd", code: 1,
                userInfo: [NSLocalizedDescriptionKey:
                    "No stored credentials — scan a QR code to connect"])
        }

        // Verify the server's Noise key matches the fingerprint in the QR URL and
        // pin it to disk. The libtailscale patch reads this file in TsnetUp and
        // serves it via a local interceptor, so tsnet never fetches the key from
        // the untrusted proxy — closing the preauth-key theft attack.
        // See docs/ios-key-verification.md for the full threat model.
        do {
            try await ServerKeyVerifier.verify(config: config, stateDir: tsDir)
        } catch {
            if case .connecting = connectionState {
                connectionState = .error(error.localizedDescription)
            }
            throw error
        }

        let tsConfig = Configuration(
            hostName: Self.deviceHostname,
            path: tsDir,
            authKey: config.key,   // nil on reconnect; tsnet uses stored keypair
            controlURL: config.server,
            ephemeral: false
        )

        let newNode = try TailscaleNode(config: tsConfig, logger: nil)
        self.node = newNode

        // Wrap up() with a 10-second timeout. An expired/revoked preauth key
        // or a node removed from Headscale makes tsnet wait for interactive
        // login indefinitely; this surfaces a readable error instead of a
        // permanent spinner.
        do {
            try await withThrowingTaskGroup(of: Void.self) { group in
                group.addTask { try await newNode.up() }
                group.addTask {
                    try await Task.sleep(nanoseconds: 10_000_000_000)
                    throw NSError(domain: "HeadFwd", code: 2,
                        userInfo: [NSLocalizedDescriptionKey:
                            "Connection timed out — credentials may be revoked. Try scanning a new QR code."])
                }
                try await group.next()!
                group.cancelAll()
            }
        } catch let connectError {
            // Only update state if we're still connecting — a concurrent
            // disconnect()/clearState() call may have already moved us to
            // .disconnected, in which case we leave it alone.
            if case .connecting = connectionState {
                connectionState = .error(connectError.localizedDescription)
            }
            throw connectError
        }

        // If disconnect() ran concurrently while up() was in flight (e.g., the
        // user tapped Cancel or Reset), honour that decision and bail out now.
        guard case .connecting = connectionState else { return }

        // Preauth key is single-use — clear it from storage now that the
        // node is registered. tsnet will reconnect using its stored keypair.
        if config.key != nil {
            config.withKeyCleared().save()
        }

        let ips = try await newNode.addrs()
        let ip = ips.ip4 ?? ips.ip6 ?? "unknown"
        tailscaleIP = ip
        tailscaleHostname = Self.deviceHostname
        connectionState = .connected(ip: ip)
    }

    func disconnect() async {
        // Capture and nil out immediately so the UI updates at once and any
        // concurrent connect() sees the state change before close() returns.
        let nodeToClose = self.node
        self.node = nil
        tailscaleIP = nil
        tailscaleHostname = nil
        connectionState = .disconnected
        try? await nodeToClose?.close()
    }

    func clearState() async {
        await disconnect()
        // Rotate to a fresh UUID directory so the Go runtime cannot reuse
        // the old machine keypair from its in-process cache. The next
        // connect() will load from an empty directory and generate a new key.
        Self.rotateStateDir()
    }

    /// Returns a URLSession proxied through the tsnet SOCKS5 loopback.
    /// Use this to reach services at tailnet IPs (100.64.x.x).
    /// For public URLs use URLSession.shared directly.
    func makeURLSession() async throws -> URLSession {
        guard let node else { return URLSession.shared }
        let (config, _) = try await URLSessionConfiguration.tailscaleSession(node)
        config.timeoutIntervalForRequest = 15
        return URLSession(configuration: config)
    }
}
