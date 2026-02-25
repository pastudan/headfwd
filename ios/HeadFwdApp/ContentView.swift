import SwiftUI
import WebKit

struct ContentView<T: TailscaleServiceProtocol>: View {
    @ObservedObject var tailscale: T
    @State private var config: HeadscaleConfig? = HeadscaleConfig.load()

    @State private var showScanner = false
    @State private var parseError: String?
    @State private var connectError: String?

    @State private var helloResponse: HelloResponse?
    @State private var helloError: String?
    @State private var helloLoading = false

    @State private var showResetConfirm = false

    @State private var showPortal = false
    @State private var portalSession: URLSession?

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
            .sheet(isPresented: $showScanner) {
                QRScannerSheet { payload in
                    showScanner = false
                    handlePayload(payload, autoConnect: true)
                }
            }
            .sheet(isPresented: $showPortal) {
                if let session = portalSession,
                   let ts = config?.tailnetServer,
                   let url = URL(string: ts) {
                    PortalWebView(portalURL: url, session: session)
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

            if let name = tailscale.tailscaleHostname {
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
                Task {
                    do {
                        try await tailscale.connect(config: config!)
                        config = HeadscaleConfig.load()
                    } catch {
                        connectError = error.localizedDescription
                    }
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
                Text(helloError).foregroundStyle(.red).font(.caption)
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
                    showScanner = true
                } label: {
                    Label("Scan New QR Code", systemImage: "qrcode.viewfinder")
                }

                Button("Reset Configuration", role: .destructive) {
                    showResetConfirm = true
                }
                .confirmationDialog("Reset Configuration?", isPresented: $showResetConfirm, titleVisibility: .visible) {
                    Button("Reset", role: .destructive) {
                        Task {
                            await tailscale.disconnect()
                            HeadscaleConfig.clear()
                            config = nil
                            helloResponse = nil
                            helloError = nil
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
    /// Strips the scheme (https://) and truncates a long fingerprint subdomain,
    /// keeping the TLD suffix visible: `9126…736e.headfwd.net`
    private func truncateMiddle(_ s: String, keep: Int = 12) -> String {
        // Strip scheme
        var display = s
        for scheme in ["https://", "http://"] {
            if display.hasPrefix(scheme) {
                display = String(display.dropFirst(scheme.count))
                break
            }
        }
        // Short enough? Show as-is.
        guard display.count > keep * 2 + 1 else { return display }
        // Keep `keep` chars from start + 12 chars of domain suffix + `keep`-12 fingerprint chars
        // e.g. for .headfwd.net (12 chars) keep suffix=16 to show "736e.headfwd.net"
        let suffixLen = max(keep, 16)
        let prefixLen = keep
        guard display.count > prefixLen + suffixLen + 1 else { return display }
        return "\(display.prefix(prefixLen))…\(display.suffix(suffixLen))"
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
            Task {
                do {
                    try await tailscale.connect(config: parsed)
                    config = HeadscaleConfig.load()
                } catch {
                    connectError = error.localizedDescription
                }
            }
        }
    }

    private func openPortal() async {
        // Pre-create the tsnet URLSession so the scheme handler has it ready
        // before WKWebView starts loading. Reuse an existing session if present.
        // makeURLSession() can throw URLError.badURL when the SOCKS proxy isn't
        // ready yet; try? falls back to nil so the sheet simply won't show.
        if portalSession == nil {
            portalSession = try? await tailscale.makeURLSession()
        }
        // Only open if we have a valid session and tailnet address.
        if portalSession != nil {
            showPortal = true
        } else {
            helloError = "Tailnet session not ready — wait a moment and try again"
        }
    }

    private func fetchHello() async {
        guard let cfg = config else { return }
        helloLoading = true
        helloError = nil
        do {
            // Prefer the tailnet address so the server sees the device's real 100.64 IP.
            // Falls back to the public URL (internet IP) if not on tailnet or if the
            // SOCKS proxy session setup fails (TailscaleKit throws URLError.badURL when
            // the loopback proxy isn't ready yet).
            let session: URLSession
            let serverURL: String
            if let tailnetAddr = cfg.tailnetServer,
               tailscale.connectionState.isConnected,
               let tailnetSession = try? await tailscale.makeURLSession() {
                session = tailnetSession
                serverURL = tailnetAddr
            } else {
                session = URLSession.shared
                serverURL = cfg.server
            }
            helloResponse = try await network.hello(session: session, serverURL: serverURL)
        } catch {
            helloError = error.localizedDescription
        }
        helloLoading = false
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
