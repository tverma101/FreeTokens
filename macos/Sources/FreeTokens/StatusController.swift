import AppKit
import Combine
import SwiftUI

/// A borderless panel that can become key without activating the app, so Esc and keyboard
/// shortcuts work on the first click (QuotaBar's approach, instead of NSPopover or MenuBarExtra).
final class MenuPanel: NSPanel {
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }
}

/// Owns the menu bar item and the dashboard panel.
@MainActor
final class StatusController: NSObject {
    private let model: AppModel
    private let state = PanelState()
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    private let panel: MenuPanel
    private let host: NSHostingView<PanelView>
    private var anchorTop: CGFloat = 0
    private var outsideMonitor: Any?
    private var keyMonitor: Any?
    private var cancellables: Set<AnyCancellable> = []
    private var titleTimer: Timer?
    private static let cornerRadius: CGFloat = 13

    init(model: AppModel) {
        self.model = model
        host = NSHostingView(rootView: PanelView(model: model, state: state))
        panel = MenuPanel(
            contentRect: NSRect(x: 0, y: 0, width: PanelView.width, height: 400),
            styleMask: [.borderless, .nonactivatingPanel, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )
        super.init()
        configurePanel()
        configureStatusItem()

        Publishers.CombineLatest3(model.$snapshot, model.$settings, model.$engineError)
            .receive(on: RunLoop.main)
            .sink { [weak self] _, _, _ in self?.updateStatusItem() }
            .store(in: &cancellables)
        state.$contentHeight
            .receive(on: RunLoop.main)
            .sink { [weak self] _ in self?.applyHeight() }
            .store(in: &cancellables)
        titleTimer = Timer.scheduledTimer(withTimeInterval: 15, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.updateStatusItem() }
        }
        NSWorkspace.shared.notificationCenter.addObserver(
            self, selector: #selector(otherAppActivated), name: NSWorkspace.didActivateApplicationNotification, object: nil
        )
    }

    // MARK: - Setup

    private func configurePanel() {
        panel.level = .statusBar
        panel.hidesOnDeactivate = false
        panel.isMovable = false
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = true
        panel.animationBehavior = .utilityWindow
        panel.isReleasedWhenClosed = false
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .transient]

        host.wantsLayer = true
        host.layer?.cornerRadius = Self.cornerRadius
        host.layer?.cornerCurve = .continuous
        host.layer?.masksToBounds = true
        host.layer?.borderWidth = 0.5
        host.layer?.borderColor = NSColor.separatorColor.cgColor
        panel.contentView = host
    }

    private func configureStatusItem() {
        guard let button = statusItem.button else { return }
        button.target = self
        button.action = #selector(statusItemClicked(_:))
        button.sendAction(on: [.leftMouseUp, .rightMouseUp])
        button.imagePosition = .imageLeading
        button.font = NSFont.monospacedDigitSystemFont(ofSize: NSFont.systemFontSize, weight: .regular)
        button.setAccessibilityTitle("FreeTokens")
        updateStatusItem()
    }

    /// Icon (with a warning dot when something needs attention) and the earliest session countdown.
    private func updateStatusItem() {
        guard let button = statusItem.button else { return }
        let snapshot = model.snapshot
        let warning = model.engineError != nil || (snapshot?.quotaError != nil && snapshot?.quota == nil)
        button.image = MenuBarIcon.image(warning: warning)
        var title = ""
        if model.settings.showTimers, let next = model.activeSessions(at: Date()).first {
            title = " " + Format.duration(next.timeIntervalSinceNow)
        }
        if button.title != title { button.title = title }
        button.toolTip = tooltip(snapshot)
    }

    private func tooltip(_ snapshot: Snapshot?) -> String {
        var lines = ["FreeTokens"]
        if let meter = snapshot?.quota?.meters?.first {
            lines.append("\(meter.label): \(Format.number(meter.remaining)) of \(Format.number(meter.limit)) \(meter.unit) left")
        } else if let error = snapshot?.quotaError ?? model.engineError {
            lines.append(error)
        }
        if let next = model.activeSessions(at: Date()).first {
            lines.append("Session: \(Format.duration(next.timeIntervalSinceNow)) left")
        }
        return lines.joined(separator: "\n")
    }

    // MARK: - Clicks

    @objc private func statusItemClicked(_ sender: NSStatusBarButton) {
        let event = NSApp.currentEvent
        if event?.type == .rightMouseUp || event?.modifierFlags.contains(.control) == true {
            showMenu()
        } else if panel.isVisible {
            hidePanel()
        } else {
            showPanel()
        }
    }

    private func showMenu() {
        hidePanel()
        let menu = NSMenu()
        menu.addItem(withTitle: "Refresh Now", action: #selector(menuRefresh), keyEquivalent: "r").target = self
        menu.addItem(withTitle: "Settings…", action: #selector(menuSettings), keyEquivalent: ",").target = self
        menu.addItem(.separator())
        menu.addItem(withTitle: "Quit FreeTokens", action: #selector(menuQuit), keyEquivalent: "q").target = self
        statusItem.menu = menu
        statusItem.button?.performClick(nil)
        statusItem.menu = nil
    }

    @objc private func menuRefresh() { Task { await model.refresh(interactive: true) } }
    @objc private func menuSettings() { state.screen = .settings; showPanel() }
    @objc private func menuQuit() { NSApp.terminate(nil) }
    @objc private func otherAppActivated() { hidePanel() }

    // MARK: - Panel

    func showPanel() {
        let (anchor, screen) = panelAnchor()
        let visible = screen?.visibleFrame ?? NSRect(x: 0, y: 0, width: 1440, height: 900)
        anchorTop = anchor.minY - 5
        state.maxContentHeight = max(200, anchorTop - visible.minY - 12 - PanelView.footerHeight)
        var x = anchor.midX - PanelView.width / 2
        x = min(max(x, visible.minX + 8), visible.maxX - PanelView.width - 8)
        panel.setFrameOrigin(NSPoint(x: x, y: anchorTop - panelHeight))
        applyHeight()
        statusItem.button?.highlight(true)
        panel.alphaValue = 0
        panel.makeKeyAndOrderFront(nil)
        NSAnimationContext.runAnimationGroup { context in
            context.duration = 0.12
            panel.animator().alphaValue = 1
        }
        installMonitors()
        if let refreshed = model.snapshot?.refreshedAt, Date().timeIntervalSince(refreshed) < 120 { return }
        Task { await model.refresh() }
    }

    func hidePanel() {
        guard panel.isVisible else { return }
        removeMonitors()
        statusItem.button?.highlight(false)
        panel.orderOut(nil)
        state.screen = .dashboard
    }

    /// The status button's frame on screen and its display. When macOS hasn't placed the item (a
    /// full menu bar, or the app isn't allowed in it) the button's window has an empty frame, so the
    /// panel opens at the top right of the display under the pointer instead.
    private func panelAnchor() -> (NSRect, NSScreen?) {
        if let button = statusItem.button, let window = button.window, window.frame.height >= 1 {
            let rect = window.convertToScreen(button.convert(button.bounds, to: nil))
            if let screen = NSScreen.screens.first(where: { $0.frame.contains(NSPoint(x: rect.midX, y: rect.midY)) }) {
                return (rect, screen)
            }
        }
        let mouse = NSEvent.mouseLocation
        let screen = NSScreen.screens.first { NSMouseInRect(mouse, $0.frame, false) } ?? NSScreen.main
        let visible = screen?.visibleFrame ?? NSRect(x: 0, y: 0, width: 1440, height: 900)
        let rect = NSRect(x: visible.maxX - PanelView.width / 2 - 8, y: visible.maxY - 1, width: 0, height: 0)
        return (rect, screen)
    }

    private var panelHeight: CGFloat {
        min(state.contentHeight, state.maxContentHeight) + PanelView.footerHeight
    }

    /// Resizes the panel to its content, keeping the top edge under the menu bar.
    private func applyHeight() {
        guard panel.isVisible || anchorTop > 0 else { return }
        let height = panelHeight.rounded()
        var frame = panel.frame
        guard abs(frame.height - height) > 0.5 || abs(frame.maxY - anchorTop) > 0.5 else { return }
        frame.size = NSSize(width: PanelView.width, height: height)
        frame.origin.y = anchorTop - height
        panel.setFrame(frame, display: true)
    }

    private func installMonitors() {
        removeMonitors()
        outsideMonitor = NSEvent.addGlobalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown, .otherMouseDown]) { [weak self] _ in
            Task { @MainActor in self?.hidePanel() }
        }
        keyMonitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            guard let self, self.panel.isKeyWindow else { return event }
            if event.keyCode == 53 { // Esc
                if self.state.screen == .settings {
                    self.state.screen = .dashboard
                } else {
                    self.hidePanel()
                }
                return nil
            }
            if event.modifierFlags.contains(.command), event.charactersIgnoringModifiers == "q" {
                NSApp.terminate(nil)
                return nil
            }
            return event
        }
    }

    private func removeMonitors() {
        if let outsideMonitor { NSEvent.removeMonitor(outsideMonitor) }
        if let keyMonitor { NSEvent.removeMonitor(keyMonitor) }
        outsideMonitor = nil
        keyMonitor = nil
    }
}
