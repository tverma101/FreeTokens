import SwiftUI

/// A section header above a card, as in QuotaBar: title leading, optional accessory trailing.
struct SectionHeader<Accessory: View>: View {
    let title: String
    var badge: String?
    @ViewBuilder var accessory: () -> Accessory

    var body: some View {
        HStack(spacing: 6) {
            Text(title)
                .font(.system(size: Theme.header, weight: .semibold))
            if let badge, !badge.isEmpty {
                Text(badge)
                    .font(.system(size: 11, weight: .medium))
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 1)
                    .background(Capsule().fill(Theme.cardFill))
            }
            Spacer(minLength: 8)
            accessory()
        }
        .padding(.horizontal, 4)
        .padding(.vertical, 2)
    }
}

extension SectionHeader where Accessory == EmptyView {
    init(title: String, badge: String? = nil) {
        self.init(title: title, badge: badge) { EmptyView() }
    }
}

/// A full-width capsule meter.
struct CapsuleMeter: View {
    let fraction: Double
    let color: Color
    var height: CGFloat = 5

    var body: some View {
        GeometryReader { proxy in
            ZStack(alignment: .leading) {
                Capsule().fill(Color.primary.opacity(0.09))
                Capsule()
                    .fill(color)
                    .frame(width: max(fraction > 0 ? height : 0, proxy.size.width * min(max(fraction, 0), 1)))
            }
        }
        .frame(height: height)
        .animation(.spring(response: 0.4, dampingFraction: 0.85), value: fraction)
    }
}

/// A one-line label ⟷ value row inside a card.
struct TextRow: View {
    let title: String
    let value: String
    var detail: String?
    var valueStyle: AnyShapeStyle = AnyShapeStyle(.primary)

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(title)
                .font(.system(size: Theme.label, weight: .semibold))
                .lineLimit(1)
            Spacer(minLength: 8)
            VStack(alignment: .trailing, spacing: 1) {
                Text(value)
                    .font(.system(size: Theme.supporting, weight: .medium))
                    .foregroundStyle(valueStyle)
                    .monospacedDigit()
                if let detail {
                    Text(detail)
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                }
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 7)
    }
}

/// Hairline between rows of one card, inset like grouped lists.
struct RowDivider: View {
    var body: some View {
        Rectangle()
            .fill(Color.primary.opacity(0.07))
            .frame(height: 0.5)
            .padding(.horizontal, 14)
    }
}

/// A round icon button for the footer chrome.
struct ChromeButton: View {
    let systemImage: String
    let label: String
    var spinning = false
    let action: () -> Void

    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            Image(systemName: systemImage)
                .font(.system(size: 13, weight: .medium))
                .foregroundStyle(.secondary)
                .rotationEffect(.degrees(spinning ? 360 : 0))
                .animation(spinning ? .linear(duration: 1).repeatForever(autoreverses: false) : .default, value: spinning)
                .frame(width: 28, height: 28)
                .background(Circle().fill(hovering ? Theme.cardFill : .clear))
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .help(label)
        .accessibilityLabel(label)
    }
}

/// An inline notice card: icon, title, message and actions.
struct NoticeCard<Actions: View>: View {
    let systemImage: String
    let title: String
    let message: String
    var tint: Color = Theme.notice
    @ViewBuilder var actions: () -> Actions

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .top, spacing: 10) {
                Image(systemName: systemImage)
                    .font(.system(size: 17, weight: .medium))
                    .foregroundStyle(tint)
                    .frame(width: 22)
                VStack(alignment: .leading, spacing: 3) {
                    Text(title)
                        .font(.system(size: Theme.label, weight: .semibold))
                    Text(message)
                        .font(.system(size: Theme.supporting))
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            HStack(spacing: 8) {
                Spacer(minLength: 0)
                actions()
            }
            .controlSize(.small)
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .cardSurface()
    }
}
