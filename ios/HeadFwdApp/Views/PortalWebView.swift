import SwiftUI
import WebKit

// MARK: - SwiftUI wrapper

/// Full-screen in-app portal dashboard, routed through the tsnet SOCKS5 proxy.
///
/// WKWebView runs in an out-of-process WebContent sandbox and cannot use
/// URLSessionConfiguration proxy settings. To work around this, every
/// request WKWebView makes uses the custom "tsnet://" scheme. A
/// WKURLSchemeHandler intercepts those requests in-process and forwards
/// them to the real HTTP server over the tsnet URLSession (which *is*
/// SOCKS5-proxied through libtailscale).
///
/// URL rewriting:
///   tsnet://100.64.0.3:3001/some/path  →  http://100.64.0.3:3001/some/path
///
/// The React SPA is loaded once; all subsequent /api/* fetch() calls from
/// the page are intercepted by the same handler and routed over the tailnet.
struct PortalWebView: View {
    /// The portal URL as stored in config, e.g. "http://100.64.0.3:3001"
    let portalURL: URL
    let session: URLSession

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            PortalWKView(portalURL: portalURL, session: session)
                .ignoresSafeArea(edges: .bottom)
                .navigationTitle("Portal Dashboard")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Done") { dismiss() }
                    }
                }
        }
    }
}

// MARK: - UIViewRepresentable bridge

private struct PortalWKView: UIViewRepresentable {
    let portalURL: URL
    let session: URLSession

    func makeCoordinator() -> TailscaleSchemeHandler {
        TailscaleSchemeHandler(session: session)
    }

    func makeUIView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.setURLSchemeHandler(context.coordinator, forURLScheme: "tsnet")
        let webView = WKWebView(frame: .zero, configuration: config)
        webView.allowsBackForwardNavigationGestures = true

        // Rewrite http:// → tsnet:// so the scheme handler intercepts it
        if let tsnetURL = httpToTsnet(portalURL) {
            webView.load(URLRequest(url: tsnetURL))
        }
        return webView
    }

    func updateUIView(_ webView: WKWebView, context: Context) {
        // Reload if the portal URL changed (e.g. tsnet reconnect with new IP)
        context.coordinator.updateSession(session)
    }

    /// Converts http://host:port/path → tsnet://host:port/path
    private func httpToTsnet(_ url: URL) -> URL? {
        var comps = URLComponents(url: url, resolvingAgainstBaseURL: false)
        comps?.scheme = "tsnet"
        return comps?.url
    }
}

// MARK: - Scheme handler

/// Intercepts tsnet:// requests from WKWebView and forwards them to the
/// real HTTP server through the tsnet-proxied URLSession.
final class TailscaleSchemeHandler: NSObject, WKURLSchemeHandler {
    private var session: URLSession
    private var activeTasks: [ObjectIdentifier: URLSessionDataTask] = [:]
    private let lock = NSLock()

    init(session: URLSession) {
        self.session = session
    }

    func updateSession(_ newSession: URLSession) {
        lock.lock()
        session = newSession
        lock.unlock()
    }

    func webView(_ webView: WKWebView, start schemeTask: any WKURLSchemeTask) {
        guard let tsnetURL = schemeTask.request.url,
              var comps = URLComponents(url: tsnetURL, resolvingAgainstBaseURL: false) else {
            schemeTask.didFailWithError(URLError(.badURL))
            return
        }

        // Rewrite tsnet:// → http:// for the outgoing request
        comps.scheme = "http"
        guard let httpURL = comps.url else {
            schemeTask.didFailWithError(URLError(.badURL))
            return
        }

        var req = schemeTask.request
        req.url = httpURL

        lock.lock()
        let sess = session
        lock.unlock()

        let task = sess.dataTask(with: req) { [weak self] data, response, error in
            guard let self else { return }

            if let error {
                // Ignore cancellation noise from stop()
                let nsErr = error as NSError
                if nsErr.code != NSURLErrorCancelled {
                    schemeTask.didFailWithError(error)
                }
                self.removeTask(for: schemeTask)
                return
            }

            guard let response else {
                schemeTask.didFailWithError(URLError(.badServerResponse))
                self.removeTask(for: schemeTask)
                return
            }

            // WKWebView requires the response URL to match the original tsnet:// URL.
            // allHeaderFields is [AnyHashable: Any] and cannot be cast directly to
            // [String: String]; iterate and cast each pair individually.
            let rewritten: URLResponse
            if let http = response as? HTTPURLResponse {
                var fields: [String: String] = [:]
                for (k, v) in http.allHeaderFields {
                    if let ks = k as? String, let vs = v as? String {
                        fields[ks] = vs
                    }
                }
                rewritten = HTTPURLResponse(
                    url: tsnetURL,
                    statusCode: http.statusCode,
                    httpVersion: "HTTP/1.1",
                    headerFields: fields
                ) ?? response
            } else {
                rewritten = URLResponse(
                    url: tsnetURL,
                    mimeType: response.mimeType,
                    expectedContentLength: Int(response.expectedContentLength),
                    textEncodingName: response.textEncodingName
                )
            }

            schemeTask.didReceive(rewritten)
            schemeTask.didReceive(data ?? Data())
            schemeTask.didFinish()
            self.removeTask(for: schemeTask)
        }

        lock.lock()
        activeTasks[ObjectIdentifier(schemeTask)] = task
        lock.unlock()

        task.resume()
    }

    func webView(_ webView: WKWebView, stop schemeTask: any WKURLSchemeTask) {
        removeTask(for: schemeTask)?.cancel()
    }

    @discardableResult
    private func removeTask(for schemeTask: any WKURLSchemeTask) -> URLSessionDataTask? {
        lock.lock()
        defer { lock.unlock() }
        return activeTasks.removeValue(forKey: ObjectIdentifier(schemeTask))
    }
}
