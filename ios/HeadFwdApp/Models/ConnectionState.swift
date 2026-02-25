import Foundation

enum ConnectionState: Equatable {
    case disconnected
    case connecting
    case connected(ip: String)
    case error(String)

    var label: String {
        switch self {
        case .disconnected: "Disconnected"
        case .connecting:   "Connecting..."
        case .connected:    "Connected"
        case .error(let msg): "Error: \(msg)"
        }
    }

    var isConnected: Bool {
        if case .connected = self { return true }
        return false
    }
}
