import Foundation

/// Simulates tailscale connection for UI development.
/// Returns a fake 100.64.x.x IP after a short delay.
@MainActor
final class MockTailscaleService: TailscaleServiceProtocol {
    @Published private(set) var connectionState: ConnectionState = .disconnected
    @Published private(set) var tailscaleIP: String?
    @Published private(set) var tailscaleHostname: String?

    func connect(config: HeadscaleConfig) async throws {
        connectionState = .connecting
        try await Task.sleep(for: .seconds(1.5))
        let fakeIP = "100.64.0.\(Int.random(in: 2...254))"
        tailscaleIP = fakeIP
        tailscaleHostname = "headfwd-ios"
        connectionState = .connected(ip: fakeIP)
    }

    func disconnect() async {
        connectionState = .disconnected
        tailscaleIP = nil
        tailscaleHostname = nil
    }

    func makeURLSession() async throws -> URLSession {
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 15
        return URLSession(configuration: config)
    }
}
