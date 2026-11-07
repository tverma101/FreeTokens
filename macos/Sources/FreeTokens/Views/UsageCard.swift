import SwiftUI

enum UsageMetric: String, CaseIterable, Identifiable {
    case cost, tokens
    var id: String { rawValue }
    var title: String { self == .cost ? "Estimated Cost" : "Tokens" }
}

/// Today / Yesterday / Last 30 Days, as QuotaBar's Total Spend card: a capsule period picker over a
/// donut of each model's share, the total in the middle and a ranked legend beside it.
struct UsageSection: View {
    let snapshot: Snapshot?
    @AppStorage("usagePeriod") private var periodIndex = 0
    @AppStorage("usageMetric") private var metricRaw = UsageMetric.cost.rawValue
    @Namespace private var picker

    private var metric: UsageMetric { UsageMetric(rawValue: metricRaw) ?? .cost }
    private var periods: [Period] { snapshot?.periods ?? [] }
    private var period: Period? { periods.indices.contains(periodIndex) ? periods[periodIndex] : periods.first }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 5) {
                metricMenu
                Image(systemName: "info.circle")
                    .imageScale(.small)
                    .foregroundStyle(.secondary)
                    .help(snapshot?.sourceNote ?? "Measured Freebuff usage. Dollars are estimates at public API rates, not Freebuff charges.")
                Spacer(minLength: 8)
            }
            .padding(.horizontal, 4)
            .padding(.vertical, 2)
            VStack(spacing: 12) {
                if periods.count > 1 { periodPicker }
                if let period, period.hasData, !(period.models ?? []).isEmpty {
                    UsageRing(period: period, metric: metric)
                    footnote(period)
                } else {
                    Text(snapshot == nil ? "Loading…" : "No measured usage in this period")
                        .font(.system(size: Theme.supporting))
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 18)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 12)
            .frame(maxWidth: .infinity)
            .cardSurface()
            .animation(.spring(response: 0.35, dampingFraction: 0.86), value: periodIndex)
            .animation(.spring(response: 0.35, dampingFraction: 0.86), value: metricRaw)
        }
    }

    /// The title doubles as the metric switch, a plain pull-down with no extra chrome.
    @ViewBuilder
    private var metricMenu: some View {
        let menu = Menu {
            ForEach(UsageMetric.allCases) { option in
                Button {
                    metricRaw = option.rawValue
                } label: {
                    if option == metric { Label(option.title, systemImage: "checkmark") } else { Text(option.title) }
                }
            }
        } label: {
            HStack(spacing: 4) {
                Text(metric.title)
                    .font(.system(size: Theme.header, weight: .semibold))
                    .foregroundStyle(.primary)
                Image(systemName: "chevron.down")
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundStyle(.secondary)
            }
        }
        .fixedSize()
        .accessibilityLabel("Usage metric")
        .accessibilityValue(metric.title)
        if #available(macOS 14, *) {
            menu.menuStyle(.button).buttonStyle(.plain).menuIndicator(.hidden)
        } else {
            menu.menuStyle(.borderlessButton)
        }
    }

    private var periodPicker: some View {
        HStack(spacing: 2) {
            ForEach(Array(periods.enumerated()), id: \.offset) { index, candidate in
                let selected = index == periodIndex
                Button {
                    periodIndex = index
                } label: {
                    Text(shortLabel(candidate.label))
                        .font(.system(size: 11, weight: selected ? .semibold : .medium))
                        .foregroundStyle(selected ? AnyShapeStyle(.primary) : AnyShapeStyle(.secondary))
                        .padding(.vertical, 4)
                        .frame(maxWidth: .infinity)
                        .contentShape(Capsule())
                }
                .buttonStyle(.plain)
                .background {
                    if selected {
                        Capsule()
                            .fill(Theme.selectedSegment)
                            .shadow(color: .black.opacity(0.14), radius: 2, y: 1)
                            .matchedGeometryEffect(id: "period", in: picker)
                    }
                }
            }
        }
        .padding(3)
        .background(Capsule().fill(Color.primary.opacity(0.06)))
    }

    private func shortLabel(_ label: String) -> String {
        label == "Last 30 Days" ? "30 Days" : label
    }

    private func footnote(_ period: Period) -> some View {
        let tokens = "\(Format.tokens(period.tokens)) tokens"
        let cost = period.costUsd.map { Format.usd($0, lowerBound: period.costLowerBound) }
        var parts = [metric == .cost ? tokens : (cost.map { "~\($0)" } ?? "no price yet")]
        if period.partial { parts.append("some runs incomplete") }
        return Text(parts.joined(separator: " · "))
            .font(.system(size: 11))
            .foregroundStyle(.secondary)
            .frame(maxWidth: .infinity, alignment: .center)
    }
}

/// The donut and its legend.
struct UsageRing: View {
    let period: Period
    let metric: UsageMetric

    private static let diameter: CGFloat = 104
    private static let minimumSliceShare = 0.025

    private struct Slice: Identifiable {
        let model: ModelRow
        let amount: Double
        var id: String { model.name }
    }

    private struct Arc: Identifiable {
        let id: String
        let start: Double
        let end: Double
    }

    private var slices: [Slice] {
        (period.models ?? [])
            .map { Slice(model: $0, amount: metric == .cost ? ($0.costUsd ?? 0) : Double($0.tokens)) }
            .filter { $0.amount > 0 || metric == .tokens }
            .sorted { $0.amount > $1.amount }
    }

    private var arcs: [Arc] {
        let items = slices
        let total = items.reduce(0) { $0 + $1.amount }
        guard total > 0 else { return [] }
        let floored = items.map { max($0.amount / total, Self.minimumSliceShare) }
        let sum = floored.reduce(0, +)
        var cursor = 0.0
        return zip(items, floored).map { slice, share in
            let width = share / sum
            defer { cursor += width }
            return Arc(id: slice.id, start: cursor, end: cursor + width)
        }
    }

    var body: some View {
        HStack(spacing: 16) {
            ZStack {
                if arcs.isEmpty {
                    Circle().stroke(Color.primary.opacity(0.08), lineWidth: Self.diameter * 0.19)
                        .padding(Self.diameter * 0.095)
                }
                ForEach(arcs) { arc in
                    RingSectorShape(startFraction: arc.start, endFraction: arc.end)
                        .fill(Theme.modelColor(arc.id))
                }
                center
            }
            .frame(width: Self.diameter, height: Self.diameter)
            VStack(alignment: .leading, spacing: 8) {
                ForEach(slices.prefix(5)) { slice in
                    legendRow(slice)
                }
                if slices.count > 5 {
                    Text("+\(slices.count - 5) more")
                        .font(.system(size: 11))
                        .foregroundStyle(.tertiary)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .accessibilityElement(children: .combine)
    }

    private var center: some View {
        let primary: String
        let unit: String
        switch metric {
        case .cost:
            primary = period.costUsd.map { Format.usd($0, lowerBound: period.costLowerBound) } ?? "—"
            unit = "est. cost"
        case .tokens:
            primary = Format.tokens(period.tokens)
            unit = "tokens"
        }
        return VStack(spacing: 1) {
            Text(primary)
                .font(.system(size: 13, weight: .semibold, design: .rounded))
                .monospacedDigit()
                .lineLimit(1)
                .minimumScaleFactor(0.6)
            Text(unit)
                .font(.system(size: 9, weight: .medium))
                .foregroundStyle(.tertiary)
        }
        .padding(.horizontal, 12)
        .help(period.costLowerBound && metric == .cost ? "A lower bound: some tokens have no public price or no complete receipt yet." : "")
    }

    /// Two-line rows (model, then amount) so long Freebuff route names fit beside the ring.
    private func legendRow(_ slice: Slice) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 7) {
            Circle()
                .fill(Theme.modelColor(slice.id))
                .frame(width: 8, height: 8)
                .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 4 }
            VStack(alignment: .leading, spacing: 1) {
                Text(Format.modelName(slice.model.name))
                    .font(.system(size: Theme.supporting))
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .help(slice.model.name)
                Text(value(slice))
                    .font(.system(size: 11, weight: .medium))
                    .foregroundStyle(.secondary)
                    .monospacedDigit()
                    .lineLimit(1)
            }
        }
    }

    private func value(_ slice: Slice) -> String {
        switch metric {
        case .cost:
            guard let cost = slice.model.costUsd else { return "no price" }
            return Format.usd(cost, lowerBound: !slice.model.costComplete)
        case .tokens:
            return Format.tokens(slice.model.tokens)
        }
    }
}
