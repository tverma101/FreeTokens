import AppKit
import SwiftUI

/// Renders the panel to a PNG without showing it, for documentation screenshots:
///
///   FREETOKENS_SNAPSHOT=out.png [FREETOKENS_SNAPSHOT_SCREEN=settings] [FREETOKENS_APPEARANCE=dark] FreeTokens
///
/// The app loads data as usual (point FREETOKENS_ENGINE at a demo engine), draws the panel in an
/// offscreen window with a rounded, shadowed frame, writes the file and quits.
@MainActor
enum Snapshotter {
    static func runIfRequested(model: AppModel) -> Bool {
        let env = ProcessInfo.processInfo.environment
        guard let path = env["FREETOKENS_SNAPSHOT"], !path.isEmpty else { return false }
        let state = PanelState()
        state.maxContentHeight = 2000
        if env["FREETOKENS_SNAPSHOT_SCREEN"] == "settings" { state.screen = .settings }
        let appearance = NSAppearance(named: env["FREETOKENS_APPEARANCE"] == "dark" ? .darkAqua : .aqua)
        Task {
            await model.loadSettings()
            await model.refresh()
            let host = NSHostingView(rootView: PanelView(model: model, state: state))
            host.appearance = appearance
            let window = NSWindow(contentRect: NSRect(x: -10_000, y: -10_000, width: PanelView.width, height: 600),
                                  styleMask: [.borderless], backing: .buffered, defer: false)
            window.appearance = appearance
            window.contentView = host
            window.orderFrontRegardless()
            // Let SwiftUI measure, then size the window to the content and let it settle.
            for _ in 0..<3 {
                try? await Task.sleep(nanoseconds: 400_000_000)
                let height = state.contentHeight + PanelView.footerHeight
                window.setContentSize(NSSize(width: PanelView.width, height: height.rounded()))
            }
            try? await Task.sleep(nanoseconds: 600_000_000)
            write(host: host, appearance: appearance, to: path)
            NSApp.terminate(nil)
        }
        return true
    }

    private static func write(host: NSView, appearance: NSAppearance?, to path: String) {
        guard let rep = host.bitmapImageRepForCachingDisplay(in: host.bounds) else { return }
        host.cacheDisplay(in: host.bounds, to: rep)
        let scale = CGFloat(rep.pixelsWide) / host.bounds.width
        let margin: CGFloat = 40
        let size = NSSize(width: host.bounds.width + margin * 2, height: host.bounds.height + margin * 2)
        let out = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: Int(size.width * scale), pixelsHigh: Int(size.height * scale),
                                   bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
                                   colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
        out.size = size
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: out)
        let frame = NSRect(x: margin, y: margin, width: host.bounds.width, height: host.bounds.height)
        let shape = NSBezierPath(roundedRect: frame, xRadius: 13, yRadius: 13)
        let ctx = NSGraphicsContext.current!.cgContext
        ctx.saveGState()
        ctx.setShadow(offset: CGSize(width: 0, height: -10), blur: 30, color: NSColor.black.withAlphaComponent(0.28).cgColor)
        NSColor.black.setFill()
        shape.fill()
        ctx.restoreGState()
        ctx.saveGState()
        shape.addClip()
        rep.draw(in: frame)
        ctx.restoreGState()
        let dark = appearance?.name == .darkAqua
        (dark ? NSColor.white.withAlphaComponent(0.16) : NSColor.black.withAlphaComponent(0.12)).setStroke()
        shape.lineWidth = 1
        shape.stroke()
        NSGraphicsContext.restoreGraphicsState()
        try? out.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: path))
    }
}
