import Foundation

@MainActor
protocol TailscaleServiceProtocol: ObservableObject {
    var connectionState: ConnectionState { get }
    /// The device's tailnet IP (100.64.x.x) once connected.
    var tailscaleIP: String? { get }
    /// The hostname this device registered under (e.g. "headfwd-ios").
    var tailscaleHostname: String? { get }
    func connect(config: HeadscaleConfig) async throws
    func disconnect() async
    /// Disconnects and wipes the persisted tsnet WireGuard state (keypair + node registration).
    /// Call this on "Reset Configuration" so the next connect() registers a brand-new node
    /// using the new preauth key rather than re-using the old identity.
    func clearState() async
    /// Returns a URLSession that routes traffic through the tailnet (when connected).
    /// Use this to reach services at 100.64.x.x addresses. Falls back to a plain
    /// session if not connected.
    func makeURLSession() async throws -> URLSession
}
