import SwiftUI
import WebKit

// MARK: - SwiftUI wrapper

/// Full-screen in-app portal dashboard loaded from the local portal proxy.
///
/// The proxy (started via TailscaleNode.startPortalProxy) listens on
/// 127.0.0.1:PORT and forwards all requests to the tailnet portal over the
/// WireGuard tunnel using tsnet.Dial.  WKWebView loads http://127.0.0.1:PORT/
/// — a real http:// origin — which gives us:
///
///   • ES modules execute  (http:// is a valid module-loading origin; no
///     custom-scheme opaque-origin restrictions)
///   • WebSocket upgrades forwarded  (Vite HMR works in dev; portal events work)
///   • ATS exempt  (localhost is never subject to App Transport Security)
///   • Same-origin  (all JS fetch('/api/…') calls stay on 127.0.0.1:PORT → proxy)
///
/// Compare to the previous tsnet:// custom-scheme handler (~200 lines):
/// this file is ~30 lines and has no custom networking code at all.
struct PortalWebView: View {
    /// Base URL of the local proxy, e.g. http://127.0.0.1:54321
    let proxyURL: URL

    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            PortalWKView(proxyURL: proxyURL)
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
    let proxyURL: URL

    func makeUIView(context: Context) -> WKWebView {
        let webView = WKWebView(frame: .zero)
        webView.allowsBackForwardNavigationGestures = true
        print("[HeadFwd] PortalWebView: loading \(proxyURL)")
        webView.load(URLRequest(url: proxyURL))
        return webView
    }

    func updateUIView(_ webView: WKWebView, context: Context) {
        // Proxy URL is fixed for the lifetime of this view.
    }
}
