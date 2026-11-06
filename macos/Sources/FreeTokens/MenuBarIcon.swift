import AppKit

/// The coin-with-a-token glyph from the Go tray icon, drawn as a template image.
enum MenuBarIcon {
    static func image(warning: Bool) -> NSImage {
        let size = NSSize(width: 18, height: 18)
        let image = NSImage(size: size, flipped: false) { rect in
            let s = rect.width
            let center = NSPoint(x: rect.midX, y: rect.midY)
            let ring = NSBezierPath()
            ring.appendOval(in: NSRect(x: center.x - s * 0.46, y: center.y - s * 0.46, width: s * 0.92, height: s * 0.92))
            ring.appendOval(in: NSRect(x: center.x - s * 0.36, y: center.y - s * 0.36, width: s * 0.72, height: s * 0.72))
            ring.windingRule = .evenOdd
            NSColor.black.setFill()
            ring.fill()
            let d = s * 0.224
            let diamond = NSBezierPath()
            diamond.move(to: NSPoint(x: center.x, y: center.y + d))
            diamond.line(to: NSPoint(x: center.x + d, y: center.y))
            diamond.line(to: NSPoint(x: center.x, y: center.y - d))
            diamond.line(to: NSPoint(x: center.x - d, y: center.y))
            diamond.close()
            diamond.fill()
            if warning {
                let r = s * 0.17
                let dot = NSRect(x: center.x + s * 0.32 - r, y: center.y + s * 0.32 - r, width: r * 2, height: r * 2)
                NSGraphicsContext.current?.compositingOperation = .clear
                NSBezierPath(ovalIn: dot.insetBy(dx: -1.2, dy: -1.2)).fill()
                NSGraphicsContext.current?.compositingOperation = .sourceOver
                NSBezierPath(ovalIn: dot).fill()
            }
            return true
        }
        image.isTemplate = true
        image.accessibilityDescription = warning ? "FreeTokens needs attention" : "FreeTokens"
        return image
    }
}
