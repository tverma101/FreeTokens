// Draws the FreeTokens app icon (the tray's coin glyph on a blue squircle) as a 1024 px PNG.
// Usage: swift scripts/make-app-icon.swift out.png
import AppKit

let size: CGFloat = 1024
let out = CommandLine.arguments.dropFirst().first ?? "AppIcon-1024.png"
let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: Int(size), pixelsHigh: Int(size), bitsPerSample: 8,
                           samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB,
                           bytesPerRow: 0, bitsPerPixel: 0)!
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
let ctx = NSGraphicsContext.current!.cgContext

// macOS icon grid: an 824 pt squircle centered on the 1024 canvas, with a soft drop shadow.
let tile = NSRect(x: 100, y: 100, width: 824, height: 824)
let squircle = NSBezierPath(roundedRect: tile, xRadius: 185, yRadius: 185)
ctx.saveGState()
ctx.setShadow(offset: CGSize(width: 0, height: -12), blur: 28, color: NSColor.black.withAlphaComponent(0.28).cgColor)
NSColor(srgbRed: 0.13, green: 0.33, blue: 0.93, alpha: 1).setFill()
squircle.fill()
ctx.restoreGState()
squircle.addClip()
NSGradient(colors: [
    NSColor(srgbRed: 0.30, green: 0.56, blue: 1.00, alpha: 1),
    NSColor(srgbRed: 0.11, green: 0.27, blue: 0.85, alpha: 1),
])!.draw(in: tile, angle: -90)
// A faint top highlight gives the tile some depth.
NSGradient(colors: [NSColor.white.withAlphaComponent(0.18), NSColor.white.withAlphaComponent(0)])!
    .draw(in: NSRect(x: tile.minX, y: tile.midY, width: tile.width, height: tile.height / 2), angle: -90)

// The glyph: coin ring + token diamond, in proportion with internal/icon.
let c = NSPoint(x: size / 2, y: size / 2)
let g: CGFloat = 560
ctx.setShadow(offset: CGSize(width: 0, height: -8), blur: 18, color: NSColor.black.withAlphaComponent(0.22).cgColor)
let ring = NSBezierPath()
ring.appendOval(in: NSRect(x: c.x - g * 0.46, y: c.y - g * 0.46, width: g * 0.92, height: g * 0.92))
ring.appendOval(in: NSRect(x: c.x - g * 0.36, y: c.y - g * 0.36, width: g * 0.72, height: g * 0.72))
ring.windingRule = .evenOdd
NSColor.white.setFill()
ring.fill()
let d = g * 0.224
let diamond = NSBezierPath()
diamond.move(to: NSPoint(x: c.x, y: c.y + d))
diamond.line(to: NSPoint(x: c.x + d, y: c.y))
diamond.line(to: NSPoint(x: c.x, y: c.y - d))
diamond.line(to: NSPoint(x: c.x - d, y: c.y))
diamond.close()
NSColor(srgbRed: 1.0, green: 0.80, blue: 0.27, alpha: 1).setFill()
diamond.fill()

NSGraphicsContext.restoreGraphicsState()
try! rep.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: out))
print(out)
