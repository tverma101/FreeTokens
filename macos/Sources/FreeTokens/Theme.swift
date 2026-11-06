import AppKit
import SwiftUI

/// The panel look, after QuotaBar's: an opaque page ("tray") with borderless grouped cards lifted
/// off it by a subtle fill, system colors for meters, and one corner radius.
enum Theme {
    static let trayNSColor: NSColor = .textBackgroundColor
    static var tray: Color { Color(nsColor: trayNSColor) }

    /// The grouped-card fill: about the System Settings grouped box in light and dark.
    static let cardFill = Color(nsColor: NSColor(name: nil) { appearance in
        appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua
            ? NSColor(white: 1, alpha: 0.065)
            : NSColor(white: 0, alpha: 0.035)
    })

    /// The selected segment of a capsule picker.
    static let selectedSegment = Color(nsColor: NSColor(name: nil) { appearance in
        appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua
            ? NSColor(white: 1, alpha: 0.17)
            : NSColor.white
    })

    static let cornerRadius: CGFloat = 12
    static var cardShape: RoundedRectangle { RoundedRectangle(cornerRadius: cornerRadius, style: .continuous) }

    static let notice = Color(nsColor: .systemOrange)
    static let positive = Color(nsColor: .systemGreen)

    enum Severity { case normal, warning, critical }

    /// Battery-style traffic light on how much is left.
    static func severity(remaining fraction: Double) -> Severity {
        if fraction <= 0.1 { return .critical }
        if fraction <= 0.25 { return .warning }
        return .normal
    }

    static func meter(_ severity: Severity) -> Color {
        switch severity {
        case .normal: return Color(nsColor: .systemBlue)
        case .warning: return Color(nsColor: .systemYellow)
        case .critical: return Color(nsColor: .systemRed)
        }
    }

    // Font sizes (QuotaBar's default density).
    static let label: CGFloat = 13
    static let supporting: CGFloat = 12
    static let header: CGFloat = 14

    /// Stable per-model colors for the usage ring, keyed by the route so a model keeps its color.
    static func modelColor(_ name: String) -> Color {
        let palette: [NSColor] = [.systemBlue, .systemPurple, .systemTeal, .systemOrange, .systemPink, .systemGreen, .systemIndigo, .systemYellow, .systemBrown, .systemMint]
        let hash = name.unicodeScalars.reduce(UInt32(5381)) { ($0 &* 33) &+ $1.value }
        return Color(nsColor: palette[Int(hash % UInt32(palette.count))])
    }
}

extension View {
    func cardSurface() -> some View {
        background(Theme.cardShape.fill(Theme.cardFill))
    }
}
