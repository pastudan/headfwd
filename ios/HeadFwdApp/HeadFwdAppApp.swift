import SwiftUI

@main
struct HeadFwdAppApp: App {
    @StateObject private var tailscale = RealTailscaleService()

    var body: some Scene {
        WindowGroup {
            ContentView(tailscale: tailscale)
                .task {
                    await autoConnect()
                }
        }
    }

    /// Reconnects to the tailnet on launch if a saved config exists.
    ///
    /// For this PoC the reconnect happens silently while the normal UI loads.
    ///
    /// PRODUCTION INTEGRATION NOTE:
    /// Replace `.task { await autoConnect() }` with a dedicated loading screen
    /// that observes `tailscale.connectionState`. Show a branded splash or
    /// progress view while `.connecting`, transition to the main UI on
    /// `.connected`, and surface `.error` with a retry button rather than
    /// falling through to the connect screen silently.
    private func autoConnect() async {
        guard let config = HeadscaleConfig.load() else { return }
        // Guard against double-connect on Scene restoration / SwiftUI re-init.
        guard tailscale.connectionState == .disconnected else { return }
        try? await tailscale.connect(config: config)
    }
}
