import Foundation
import TailscaleKit

/// Real Tailscale service backed by tsnet (userspace WireGuard).
/// On first connect, a preauth key registers the device with Headscale.
/// The key is single-use and discarded after registration; tsnet stores
/// its own WireGuard keypair in the app's Documents/tailscale/ directory
/// and reconnects on subsequent launches without needing a new key.
@MainActor
final class RealTailscaleService: TailscaleServiceProtocol {
    @Published private(set) var connectionState: ConnectionState = .disconnected
    @Published private(set) var tailscaleIP: String?
    @Published private(set) var tailscaleHostname: String?

    private var node: TailscaleNode?

    private static let deviceHostname = "headfwd-ios"

    func connect(config: HeadscaleConfig) async throws {
        connectionState = .connecting

        let docsDir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
        let tsDir = docsDir.appendingPathComponent("tailscale").path
        try FileManager.default.createDirectory(atPath: tsDir, withIntermediateDirectories: true)

        let tsConfig = Configuration(
            hostName: Self.deviceHostname,
            path: tsDir,
            authKey: config.key,   // nil on reconnect; tsnet uses stored keypair
            controlURL: config.server,
            ephemeral: false
        )

        let newNode = try TailscaleNode(config: tsConfig, logger: nil)
        self.node = newNode

        try await newNode.up()

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
        if let node {
            try? await node.close()
        }
        self.node = nil
        tailscaleIP = nil
        tailscaleHostname = nil
        connectionState = .disconnected
    }

    func clearState() async {
        await disconnect()
        // Remove the persisted tsnet WireGuard state so the next connect()
        // uses the new preauth key and registers a fresh node with a new IP,
        // rather than silently re-using the old stored keypair.
        let docsDir = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
        let tsDir = docsDir.appendingPathComponent("tailscale")
        try? FileManager.default.removeItem(at: tsDir)
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
