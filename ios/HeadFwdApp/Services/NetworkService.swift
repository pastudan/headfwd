import Foundation

struct HelloResponse: Codable {
    let message: String
    let yourIP: String
    let isTailnet: Bool
    let timestamp: String
    let nodeName: String?
    let userName: String?

    enum CodingKeys: String, CodingKey {
        case message
        case yourIP = "your_ip"
        case isTailnet = "is_tailnet"
        case timestamp
        case nodeName = "node_name"
        case userName = "user_name"
    }
}

actor NetworkService {
    func hello(session: URLSession, serverURL: String) async throws -> HelloResponse {
        let base = serverURL.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        guard let url = URL(string: base + "/api/hello") else {
            throw NetworkError.badURL
        }
        let (data, response) = try await session.data(from: url)
        guard let http = response as? HTTPURLResponse else {
            throw NetworkError.badStatus(0, nil)
        }
        guard http.statusCode == 200 else {
            let body = String(data: data, encoding: .utf8)
            throw NetworkError.badStatus(http.statusCode, body)
        }
        do {
            return try JSONDecoder().decode(HelloResponse.self, from: data)
        } catch {
            let body = String(data: data, encoding: .utf8) ?? "(binary)"
            throw NetworkError.decodeFailed(body)
        }
    }

    enum NetworkError: LocalizedError {
        case badURL
        case badStatus(Int, String?)
        case decodeFailed(String)

        var errorDescription: String? {
            switch self {
            case .badURL:
                return "Invalid server URL"
            case .badStatus(let code, let body):
                let detail = body.flatMap { $0.isEmpty ? nil : $0 } ?? "no body"
                return "HTTP \(code): \(detail)"
            case .decodeFailed(let body):
                return "JSON decode failed. Response: \(body)"
            }
        }
    }
}
