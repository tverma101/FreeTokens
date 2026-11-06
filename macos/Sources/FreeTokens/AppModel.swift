import AppKit
import Combine
import Foundation

/// App state. All work happens in the engine; this schedules it and publishes the results.
@MainActor
final class AppModel: ObservableObject {
    enum UpdateState: Equatable {
        case idle
        case checking
        case upToDate
        case available(version: String, url: String?)
        case installing(version: String)
        case failed(String)
    }

    @Published private(set) var snapshot: Snapshot?
    @Published private(set) var engineError: String?
    @Published private(set) var isRefreshing = false
    @Published private(set) var settings = EngineSettings()
    @Published private(set) var startsAtLogin = false
    @Published private(set) var settingsError: String?
    @Published private(set) var update: UpdateState = .idle

    let version: String = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "dev"
    var isRelease: Bool { version.first?.isNumber == true && !version.contains("SNAPSHOT") }

    private var refreshTimer: Timer?
    private var updateTimer: Timer?
    private var pendingInteractive = false
    private var observers: [NSObjectProtocol] = []

    func start() {
        Task {
            await loadSettings()
            await loadLogin(repair: true)
            await refresh()
            scheduleRefresh()
            await scheduledUpdateCheck()
        }
        // Daily checks are decided by the engine (it records the last check); asking hourly is cheap.
        updateTimer = Timer.scheduledTimer(withTimeInterval: 3600, repeats: true) { [weak self] _ in
            Task { await self?.scheduledUpdateCheck() }
        }
        let center = NSWorkspace.shared.notificationCenter
        observers.append(center.addObserver(forName: NSWorkspace.didWakeNotification, object: nil, queue: .main) { [weak self] _ in
            Task { await self?.refresh() }
        })
    }

    // MARK: - Usage

    /// Refreshes once. `interactive` lets macOS show the Keychain prompt if it needs approval.
    func refresh(interactive: Bool = false) async {
        if isRefreshing {
            pendingInteractive = pendingInteractive || interactive
            return
        }
        isRefreshing = true
        defer { isRefreshing = false }
        var args = ["status", "--json"]
        if interactive { args.append("--prompt") }
        do {
            snapshot = try await Engine.json(Snapshot.self, args)
            engineError = nil
        } catch {
            engineError = error.localizedDescription
        }
        if pendingInteractive {
            pendingInteractive = false
            isRefreshing = false
            await refresh(interactive: true)
        }
    }

    private func scheduleRefresh() {
        refreshTimer?.invalidate()
        let interval = TimeInterval(max(settings.refreshMinutes, 1) * 60)
        refreshTimer = Timer.scheduledTimer(withTimeInterval: interval, repeats: true) { [weak self] _ in
            Task { await self?.refresh() }
        }
        refreshTimer?.tolerance = 15
    }

    /// Unexpired session expiries, earliest first.
    func activeSessions(at now: Date) -> [Date] {
        (snapshot?.quota?.activeSessions ?? []).filter { $0 > now }.sorted()
    }

    // MARK: - Settings

    func loadSettings() async {
        do {
            settings = try await Engine.json(EngineSettings.self, ["config", "get"], timeout: 30)
        } catch {
            settingsError = error.localizedDescription
        }
    }

    func setShowTimers(_ on: Bool) { set("show-timers", on ? "true" : "false") }
    func setRefreshMinutes(_ minutes: Int) { set("refresh-minutes", String(minutes)) }
    func setAutoUpdate(_ policy: String) {
        set("auto-update", policy) { [weak self] in
            if policy != "off" { await self?.scheduledUpdateCheck() }
        }
    }

    private func set(_ key: String, _ value: String, then: (() async -> Void)? = nil) {
        Task {
            do {
                try await Engine.command(["config", "set", key, value])
                settingsError = nil
            } catch {
                settingsError = error.localizedDescription
            }
            await loadSettings()
            if key == "refresh-minutes" { scheduleRefresh() }
            await then?()
        }
    }

    /// Reads the login item. With `repair`, re-registers it so it points at this copy of the app
    /// (after a move, or when an older command-line install created it).
    func loadLogin(repair: Bool = false) async {
        let state = (try? await Engine.command(["autostart"])) ?? "off"
        startsAtLogin = state == "on"
        if repair, startsAtLogin, Bundle.main.bundleURL.pathExtension == "app" {
            _ = try? await Engine.command(["autostart", "on"])
        }
    }

    func setStartsAtLogin(_ on: Bool) {
        Task {
            do {
                try await Engine.command(["autostart", on ? "on" : "off"])
                settingsError = nil
            } catch {
                settingsError = error.localizedDescription
            }
            await loadLogin()
        }
    }

    // MARK: - Updates

    func scheduledUpdateCheck() async {
        guard isRelease, settings.autoUpdate != "off", !isUpdateBusy else { return }
        guard let result = try? await Engine.json(UpdateResult.self, ["update", "--json", "--scheduled"], timeout: 900) else { return }
        handle(result, manual: false)
    }

    func checkForUpdates() {
        guard !isUpdateBusy else { return }
        update = .checking
        Task {
            do {
                let result = try await Engine.json(UpdateResult.self, ["update", "--json", "--check"], timeout: 120)
                handle(result, manual: true)
            } catch {
                update = .failed(error.localizedDescription)
            }
        }
    }

    func installUpdate() {
        guard case let .available(version, _) = update else { return }
        update = .installing(version: version)
        Task {
            do {
                let result = try await Engine.json(UpdateResult.self, ["update", "--json"], timeout: 900)
                handle(result, manual: true)
            } catch {
                update = .failed(error.localizedDescription)
            }
        }
    }

    private var isUpdateBusy: Bool {
        switch update {
        case .checking, .installing: return true
        default: return false
        }
    }

    private func handle(_ result: UpdateResult, manual: Bool) {
        if let error = result.error {
            if manual { update = .failed(error) }
            return
        }
        if result.installed == true {
            relaunch()
            return
        }
        if result.available == true, let latest = result.latest {
            update = .available(version: latest, url: result.url)
        } else if manual {
            update = .upToDate
        }
    }

    /// Opens the freshly installed app once this process has exited.
    private func relaunch() {
        let path = Bundle.main.bundleURL.path
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/sh")
        process.arguments = ["-c", "sleep 1; /usr/bin/open \"$0\"", path]
        try? process.run()
        NSApp.terminate(nil)
    }
}
