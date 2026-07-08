import Foundation
import Network

/// Minimal HTTP/1.1 reverse proxy bound to 127.0.0.1 that forwards requests
/// through the tailnet URLSession (SOCKS5 → WireGuard).
///
/// Used when the TailscaleKit framework pre-dates the Go-native portal proxy.
/// Functionally equivalent: WKWebView loads http://127.0.0.1:PORT/ and gets a
/// real http:// origin — ATS-exempt, ES-modules allowed, same-origin for /api/*.
actor LocalPortalProxy {
    private let session: URLSession
    private let targetBase: String   // e.g. "http://100.64.0.5:3001"
    private var listener: NWListener?

    init(session: URLSession, targetBase: String) {
        self.session = session
        self.targetBase = targetBase
    }

    /// Starts the proxy and returns the assigned port.
    func start() async throws -> UInt16 {
        let params = NWParameters.tcp
        let l = try NWListener(using: params)
        listener = l

        return try await withCheckedThrowingContinuation { cont in
            var done = false
            let mu = NSLock()

            l.stateUpdateHandler = { [weak l] state in
                mu.lock(); let first = !done; if first { done = true }; mu.unlock()
                guard first else { return }
                switch state {
                case .ready:
                    cont.resume(returning: l?.port?.rawValue ?? 0)
                case .failed(let e):
                    cont.resume(throwing: e)
                case .cancelled:
                    cont.resume(throwing: URLError(.cancelled))
                default:
                    break
                }
            }

            l.newConnectionHandler = { conn in
                Task { [weak self] in await self?.handle(conn) }
            }

            l.start(queue: .global(qos: .userInitiated))
        }
    }

    func stop() {
        listener?.cancel()
        listener = nil
    }

    // MARK: - Private

    private func handle(_ conn: NWConnection) async {
        conn.start(queue: .global(qos: .userInitiated))
        defer { conn.cancel() }
        do { try await forward(conn) }
        catch { /* connection closed or proxy error — swallow */ }
    }

    /// Reads one HTTP request, forwards via URLSession, writes response back.
    private func forward(_ conn: NWConnection) async throws {
        // 1. Read until end-of-headers marker \r\n\r\n
        let crlf2 = Data("\r\n\r\n".utf8)
        var buf = Data()
        while buf.range(of: crlf2) == nil {
            let chunk = try await recv(conn)
            guard !chunk.isEmpty else { return }
            buf.append(chunk)
            if buf.count > 1_048_576 { return }  // 1 MB guard
        }

        guard let sep = buf.range(of: crlf2) else { return }
        let headerSection = Data(buf[..<sep.lowerBound])
        var body = Data(buf[sep.upperBound...])

        // 2. Parse request line + headers
        guard let hStr = String(data: headerSection, encoding: .utf8) else { return }
        let lines = hStr.components(separatedBy: "\r\n")
        guard let reqLine = lines.first else { return }
        let tokens = reqLine.components(separatedBy: " ")
        guard tokens.count >= 2 else { return }
        let method = tokens[0]
        let path   = tokens[1]

        var hdrs = [String: String]()
        var contentLen = 0
        for line in lines.dropFirst() where line.contains(":") {
            guard let c = line.firstIndex(of: ":") else { continue }
            let k = String(line[..<c]).trimmingCharacters(in: .whitespaces).lowercased()
            let v = String(line[line.index(after: c)...]).trimmingCharacters(in: .whitespaces)
            hdrs[k] = v
            if k == "content-length", let n = Int(v) { contentLen = n }
        }

        // 3. Read remaining body bytes
        while body.count < contentLen {
            let chunk = try await recv(conn)
            guard !chunk.isEmpty else { break }
            body.append(chunk)
        }

        // 4. Build upstream URLRequest (forwarded over WireGuard via SOCKS5 session)
        let base = targetBase.hasSuffix("/") ? String(targetBase.dropLast()) : targetBase
        guard let url = URL(string: base + path) else { return }
        var req = URLRequest(url: url, timeoutInterval: 30)
        req.httpMethod = method
        // Strip hop-by-hop and proxy-specific headers
        let skip: Set<String> = ["host", "connection", "keep-alive",
                                  "proxy-connection", "transfer-encoding", "upgrade"]
        for (k, v) in hdrs where !skip.contains(k) { req.setValue(v, forHTTPHeaderField: k) }
        if contentLen > 0 { req.httpBody = body }

        let (data, response) = try await session.data(for: req)
        guard let http = response as? HTTPURLResponse else { return }

        // 5. Write HTTP/1.1 response
        let statusText = HTTPURLResponse.localizedString(forStatusCode: http.statusCode)
        var out = "HTTP/1.1 \(http.statusCode) \(statusText)\r\n"
        for (k, v) in http.allHeaderFields {
            guard let ks = k as? String, let vs = v as? String else { continue }
            let kl = ks.lowercased()
            if kl == "transfer-encoding" || kl == "connection" { continue }
            out += "\(ks): \(vs)\r\n"
        }
        out += "Content-Length: \(data.count)\r\nConnection: close\r\n\r\n"

        var outData = Data(out.utf8)
        outData.append(data)
        try await send(conn, outData)
    }

    // MARK: - NWConnection helpers

    private func recv(_ conn: NWConnection) async throws -> Data {
        try await withCheckedThrowingContinuation { cont in
            conn.receive(minimumIncompleteLength: 1, maximumLength: 65_536) { data, _, _, error in
                if let e = error { cont.resume(throwing: e) }
                else { cont.resume(returning: data ?? Data()) }
            }
        }
    }

    private func send(_ conn: NWConnection, _ data: Data) async throws {
        try await withCheckedThrowingContinuation { (cont: CheckedContinuation<Void, Error>) in
            conn.send(content: data, completion: .contentProcessed { error in
                if let e = error { cont.resume(throwing: e) } else { cont.resume() }
            })
        }
    }
}
