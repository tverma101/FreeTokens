import AppKit

@main
enum FreeTokensApp {
    @MainActor
    static func main() {
        let app = NSApplication.shared
        let delegate = AppDelegate()
        app.delegate = delegate
        app.setActivationPolicy(.accessory)
        app.run()
    }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private let model = AppModel()
    private var controller: StatusController?

    func applicationDidFinishLaunching(_ notification: Notification) {
        if Snapshotter.runIfRequested(model: model) { return }
        // One menu bar item: a second copy asks the running one to show its panel, then quits.
        if let id = Bundle.main.bundleIdentifier,
           NSRunningApplication.runningApplications(withBundleIdentifier: id).contains(where: { $0 != .current }) {
            DistributedNotificationCenter.default().postNotificationName(
                Self.showPanelNotification, object: nil, userInfo: nil, deliverImmediately: true
            )
            NSApp.terminate(nil)
            return
        }
        DistributedNotificationCenter.default().addObserver(
            self, selector: #selector(showPanelRequested), name: Self.showPanelNotification, object: nil
        )
        controller = StatusController(model: model)
        model.start()
        if ProcessInfo.processInfo.environment["FREETOKENS_OPEN_PANEL"] != nil {
            DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) { [weak self] in self?.controller?.showPanel() }
        }
    }

    private static let showPanelNotification = Notification.Name("io.github.tverma101.FreeTokens.showPanel")

    /// Opening the app again from Finder, Spotlight or Launchpad shows the panel, so it stays
    /// reachable even when macOS hides the menu bar icon.
    func applicationShouldHandleReopen(_ sender: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        controller?.showPanel()
        return false
    }

    @objc private func showPanelRequested() {
        controller?.showPanel()
    }
}
