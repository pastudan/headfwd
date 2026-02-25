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
    /// Returns a URLSession that routes traffic through the tailnet (when connected).
    /// Use this to reach services at 100.64.x.x addresses. Falls back to a plain
    /// session if not connected.
    func makeURLSession() async throws -> URLSession
}
