import SwiftUI
import WebKit

struct ContentView<T: TailscaleServiceProtocol>: View {
    @ObservedObject var tailscale: T
    @Environment(\.scenePhase) private var scenePhase
    @State private var config: HeadscaleConfig? = HeadscaleConfig.load()

    @State private var showScanner = false
    @State private var parseError: String?
    @State private var connectError: String?

    @State private var helloResponse: HelloResponse?
    @State private var helloError: String?
    @State private var helloLoading = false

    @State private var showResetConfirm = false
    @State private var connectTask: Task<Void, Never>?

    @State private var showPortal = false
    @State private var portalProxyURL: URL?

    private let network = NetworkService()

    var body: some View {
        NavigationStack {
            List {
                statusSection

                switch tailscale.connectionState {
                case .disconnected, .error:
                    if config == nil {
                        setupSection
                    } else {
                        connectSection
                    }
                case .connecting:
                    connectingSection
                case .connected:
                    demoSection
                }

                if config != nil {
                    managementSection
                }

            }
            .navigationTitle("HeadFwd")
            .onChange(of: tailscale.connectionState) { _, newState in
                if newState.isConnected {
                    Task { await fetchHello() }
                } else {
                    helloResponse = nil
                    helloError = nil
                    portalProxyURL = nil
                }
            }
            .onChange(of: scenePhase) { _, newPhase in
                // Refresh after the app returns from background — tsnet may
                // need a moment to re-establish the SOCKS proxy after sleep.
                if newPhase == .active, tailscale.connectionState.isConnected {
                    portalProxyURL = nil
                    Task {
                        // Brief grace period so tsnet can start reconnecting
                        // before we fire the first request.
                        try? await Task.sleep(for: .seconds(1.5))
                        await fetchHello()
                    }
                }
            }
            .sheet(isPresented: $showScanner) {
                QRScannerSheet { payload in
                    showScanner = false
                    handlePayload(payload, autoConnect: true)
                }
            }
            .sheet(isPresented: $showPortal) {
                if let url = portalProxyURL {
                    PortalWebView(proxyURL: url)
                }
            }
        }
    }

    // MARK: - Sections

    private var statusSection: some View {
        Section {
            // State indicator — not copyable
            HStack(spacing: 10) {
                Circle()
                    .fill(statusColor)
                    .frame(width: 8, height: 8)
                Text(tailscale.connectionState.label)
                    .font(.subheadline.weight(.medium))
            }
            .padding(.vertical, 1)

            if let name = helloResponse?.nodeName ?? tailscale.tailscaleHostname {
                copyableRow("Device", name, mono: true)
            }
            if let ip = tailscale.tailscaleIP {
                copyableRow("Tailnet IP", ip, mono: true, accent: true)
            }
            if let server = config?.server {
                // Display with middle truncation so the fingerprint doesn't overflow;
                // long-press copies the full URL.
                LabeledContent("Server") {
                    Text(truncateMiddle(server))
                        .font(.caption.monospaced())
                        .foregroundStyle(Color.secondary)
                }
                .contextMenu {
                    Button { UIPasteboard.general.string = server } label: {
                        Label("Copy Server URL", systemImage: "doc.on.doc")
                    }
                }
            }
        } header: {
            Text("Headscale Status")
        }
    }

    private var setupSection: some View {
        Section {
            Button {
                showScanner = true
            } label: {
                Label("Scan QR Code", systemImage: "qrcode.viewfinder")
            }

            if let parseError {
                Text(parseError).foregroundStyle(.red).font(.caption)
            }
        } header: {
            Text("Get Started")
        } footer: {
            Text("Scan the QR code from the HeadFwd portal to connect this device to your tailnet.")
        }
    }

    private var connectSection: some View {
        Section {
            Button("Connect to Tailnet") {
                connectError = nil
                connectTask = Task {
                    do {
                        try await tailscale.connect(config: config!)
                        config = HeadscaleConfig.load()
                    } catch {
                        connectError = error.localizedDescription
                    }
                    connectTask = nil
                }
            }
            .disabled(tailscale.connectionState == .connecting)

            if let connectError {
                Text(connectError).foregroundStyle(.red).font(.caption)
            }
        } header: {
            Text("Connection")
        }
    }

    private var connectingSection: some View {
        Section {
            HStack(spacing: 10) {
                ProgressView()
                Text("Registering with Headscale…")
                    .foregroundStyle(.secondary)
                    .font(.subheadline)
            }
            Button("Cancel", role: .destructive) {
                connectTask?.cancel()
                connectTask = nil
                Task { await tailscale.disconnect() }
            }
        }
    }

    private var demoSection: some View {
        Section {
            // Info row first, then action buttons below.
            if let ts = config?.tailnetServer {
                copyableRow("Tailnet portal", ts, mono: true)
            } else {
                responseRow("Tailnet portal", "not set — redeploy sidecar")
            }

            // Open the full portal React app in an inline WebView.
            // Requests are routed through the tsnet SOCKS5 proxy via a
            // WKURLSchemeHandler — WKWebView's sandboxed process can't use
            // URLSessionConfiguration proxy settings directly.
            if let ts = config?.tailnetServer, URL(string: ts) != nil {
                Button {
                    Task { await openPortal() }
                } label: {
                    Label("Open Portal Dashboard", systemImage: "rectangle.on.rectangle")
                }
            }

            Button {
                Task { await fetchHello() }
            } label: {
                Label(helloLoading ? "Loading…" : "Call /api/hello", systemImage: "arrow.triangle.2.circlepath")
            }
            .disabled(helloLoading || config == nil)

            if let helloError {
                HStack(alignment: .top, spacing: 8) {
                    Text(helloError).foregroundStyle(.red).font(.caption)
                    Spacer()
                    Button("Retry") { Task { await fetchHello() } }
                        .font(.caption.weight(.medium))
                        .disabled(helloLoading)
                }
            }

            if let r = helloResponse {
                copyableRow("Your IP", r.yourIP, mono: true, accent: r.isTailnet)
                responseRow("On Tailnet", r.isTailnet ? "Yes ✓" : "No", accent: r.isTailnet)
                if let node = r.nodeName { copyableRow("Device", node, mono: true) }
                if let user = r.userName { responseRow("User", user) }
                responseRow("Routed via", r.isTailnet ? "tailnet ✓" : "public internet ⚠︎", accent: r.isTailnet)
            }
        } header: {
            Text("Portal Demo")
        } footer: {
            Text("When routed via tailnet, the server sees your 100.64 IP.")
        }
    }

    private var managementSection: some View {
        Section {
            if tailscale.connectionState.isConnected {
                Button("Disconnect", role: .destructive) {
                    Task { await tailscale.disconnect() }
                }
            } else {
                Button {
                    Task {
                        await tailscale.clearState()
                        helloResponse = nil
                        helloError = nil
                        portalProxyURL = nil
                        showScanner = true
                    }
                } label: {
                    Label("Scan New QR Code", systemImage: "qrcode.viewfinder")
                }

                Button("Reset Configuration", role: .destructive) {
                    showResetConfirm = true
                }
                .confirmationDialog("Reset Configuration?", isPresented: $showResetConfirm, titleVisibility: .visible) {
                    Button("Reset", role: .destructive) {
                        connectTask?.cancel()
                        connectTask = nil
                        Task {
                            await tailscale.clearState()
                            HeadscaleConfig.clear()
                            config = nil
                            helloResponse = nil
                            helloError = nil
                            portalProxyURL = nil
                        }
                    }
                } message: {
                    Text("Removes the stored server config. You'll need to scan a new QR code to reconnect.")
                }
            }
        }
    }


    // MARK: - Row helpers

    /// A LabeledContent row whose value can be copied with a long press.
    @ViewBuilder
    private func copyableRow(_ label: String, _ value: String, mono: Bool, accent: Bool = false) -> some View {
        LabeledContent(label) {
            Text(value)
                .font(mono ? .caption.monospaced() : .caption)
                .foregroundStyle(accent ? Color.green : Color.primary)
                .multilineTextAlignment(.trailing)
        }
        .contextMenu {
            Button {
                UIPasteboard.general.string = value
            } label: {
                Label("Copy \(label)", systemImage: "doc.on.doc")
            }
        }
    }

    @ViewBuilder
    private func responseRow(_ label: String, _ value: String, mono: Bool = false, accent: Bool = false) -> some View {
        LabeledContent(label) {
            Text(value)
                .font(mono ? .caption.monospaced() : .caption)
                .foregroundStyle(accent ? Color.green : Color.primary)
        }
    }

    // MARK: - Helpers

    /// Formats a server URL for compact display.
    /// For headfwd.net fingerprint URLs: `91268f….headfwd.net`
    /// For anything else: strip scheme and show as-is (short enough in practice).
    private func truncateMiddle(_ s: String) -> String {
        var display = s
        for scheme in ["https://", "http://"] {
            if display.hasPrefix(scheme) {
                display = String(display.dropFirst(scheme.count))
                break
            }
        }
        let suffix = ".headfwd.net"
        if display.hasSuffix(suffix) {
            let sub = String(display.dropLast(suffix.count))
            return "\(sub.prefix(3))…\(sub.suffix(3))\(suffix)"
        }
        return display
    }

    // MARK: - Status color

    private var statusColor: Color {
        switch tailscale.connectionState {
        case .connected:    .green
        case .connecting:   .orange
        case .disconnected: .gray
        case .error:        .red
        }
    }

    // MARK: - Actions

    private func handlePayload(_ raw: String, autoConnect: Bool) {
        parseError = nil
        guard let parsed = HeadscaleConfig.from(qrPayload: raw) else {
            parseError = "Invalid payload. Expected: {\"server\":\"…\",\"key\":\"…\",\"v\":1}"
            return
        }
        parsed.save()
        config = parsed

        if autoConnect {
            connectError = nil
            connectTask = Task {
                do {
                    try await tailscale.connect(config: parsed)
                    config = HeadscaleConfig.load()
                } catch {
                    connectError = error.localizedDescription
                }
                connectTask = nil
            }
        }
    }

    private func openPortal() async {
        guard let tailnetAddr = config?.tailnetServer else {
            helloError = "No tailnet address — wait for sidecar to join tailnet"
            return
        }
        // Strip scheme: the proxy dialer expects bare host:port.
        let target = tailnetAddr
            .replacingOccurrences(of: "http://", with: "")
            .replacingOccurrences(of: "https://", with: "")
        print("[HeadFwd] openPortal: starting proxy → \(target)")
        do {
            portalProxyURL = try await tailscale.startPortalProxy(targetAddr: target)
            print("[HeadFwd] openPortal: proxy ready at \(portalProxyURL!)")
            showPortal = true
        } catch {
            print("[HeadFwd] openPortal: failed to start proxy: \(error)")
            helloError = "Could not start portal proxy: \(error.localizedDescription)"
        }
    }

    private func fetchHello() async {
        guard let cfg = config else { return }
        guard let tailnetAddr = cfg.tailnetServer else {
            print("[HeadFwd] fetchHello: tailnetServer not set in config (server=\(cfg.server))")
            helloError = "No tailnet address — wait for sidecar to join tailnet"
            return
        }
        print("[HeadFwd] fetchHello: target=\(tailnetAddr)/api/hello")
        helloLoading = true
        helloError = nil
        defer { helloLoading = false }

        // Up to 3 attempts — tsnet can take several seconds to re-establish
        // routing after the device wakes from sleep.
        let delays: [Double] = [2, 4]
        var lastError: Error?
        for attempt in 1...3 {
            print("[HeadFwd] fetchHello: attempt \(attempt)/3")
            do {
                guard let session = try? await tailscale.makeURLSession() else {
                    print("[HeadFwd] fetchHello: makeURLSession() returned nil on attempt \(attempt)")
                    throw URLError(.networkConnectionLost)
                }
                helloResponse = try await network.hello(session: session, serverURL: tailnetAddr)
                print("[HeadFwd] fetchHello: success")
                return
            } catch {
                print("[HeadFwd] fetchHello: attempt \(attempt) error: \(error)")
                lastError = error
                let delay = attempt <= delays.count ? delays[attempt - 1] : nil
                if let delay, tailscale.connectionState.isConnected {
                    try? await Task.sleep(for: .seconds(delay))
                }
            }
        }
        helloError = "\(lastError?.localizedDescription ?? "Unknown error") [\(tailnetAddr)]"
    }
}

// MARK: - QR scanner sheet wrapper

struct QRScannerSheet: View {
    let onScan: (String) -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            QRScannerView(onScan: onScan)
                .ignoresSafeArea()
                .navigationTitle("Scan QR Code")
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button("Cancel") { dismiss() }
                    }
                }
        }
    }
}
