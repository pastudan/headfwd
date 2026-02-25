import Foundation

/// Parsed from QR code JSON payload:
/// {"server":"https://abc.headfwd.net","key":"...","tailnet_server":"http://100.64.0.1:3001","v":1}
struct HeadscaleConfig: Codable, Equatable {
    let server: String
    let key: String?          // nil after initial registration; tsnet uses stored node key thereafter
    /// Direct tailnet address of the portal (e.g. http://100.64.0.1:3001).
    /// Present once the sidecar has self-registered as a headscale node.
    /// iOS uses this (via makeURLSession SOCKS5) so /api/hello sees the real 100.64 IP.
    let tailnetServer: String?
    let v: Int

    enum CodingKeys: String, CodingKey {
        case server
        case key
        case tailnetServer = "tailnet_server"
        case v
    }

    static func from(qrPayload raw: String) -> HeadscaleConfig? {
        guard let data = raw.data(using: .utf8) else { return nil }
        return try? JSONDecoder().decode(HeadscaleConfig.self, from: data)
    }

    /// Returns a copy with the preauth key removed (call after successful tailnet registration).
    func withKeyCleared() -> HeadscaleConfig {
        HeadscaleConfig(server: server, key: nil, tailnetServer: tailnetServer, v: v)
    }
}

extension HeadscaleConfig {
    private static let storageKey = "headfwd_config"

    func save() {
        guard let data = try? JSONEncoder().encode(self) else { return }
        UserDefaults.standard.set(data, forKey: Self.storageKey)
    }

    static func load() -> HeadscaleConfig? {
        guard let data = UserDefaults.standard.data(forKey: Self.storageKey) else { return nil }
        return try? JSONDecoder().decode(HeadscaleConfig.self, from: data)
    }

    static func clear() {
        UserDefaults.standard.removeObject(forKey: Self.storageKey)
    }
}
