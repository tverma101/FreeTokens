import AppKit
import SwiftUI

/// Hours, balance and session timers from the Freebuff session endpoint, or the action that fixes
/// whatever stopped them from loading.
struct FreebuffSection: View {
    @ObservedObject var model: AppModel
    @AppStorage("showResetTimes") private var showResetTimes = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            SectionHeader(title: "Freebuff", badge: planBadge)
            content
        }
    }

    private var planBadge: String? {
        guard let plan = model.snapshot?.quota?.plan, !plan.isEmpty else { return nil }
        return plan.prefix(1).uppercased() + plan.dropFirst()
    }

    @ViewBuilder
    private var content: some View {
        if let quota = model.snapshot?.quota {
            TimelineView(.periodic(from: .now, by: 15)) { context in
                card(quota, now: context.date)
            }
        } else if let snapshot = model.snapshot, let error = snapshot.quotaError {
            problem(code: snapshot.quotaErrorCode ?? "other", message: error)
        } else if let error = model.engineError {
            NoticeCard(systemImage: "exclamationmark.triangle.fill", title: "FreeTokens couldn't refresh", message: error) {
                Button("Try Again") { Task { await model.refresh() } }
            }
        } else {
            HStack(spacing: 8) {
                ProgressView().controlSize(.small)
                Text("Loading your Freebuff usage…")
                    .font(.system(size: Theme.supporting))
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, minHeight: 64)
            .cardSurface()
        }
    }

    // MARK: - Data

    private func card(_ quota: Quota, now: Date) -> some View {
        let meters = quota.meters ?? []
        let sessions = model.activeSessions(at: now)
        return VStack(spacing: 0) {
            ForEach(Array(meters.enumerated()), id: \.element.id) { index, meter in
                if index > 0 { RowDivider() }
                MeterRow(meter: meter, now: now, showResetTimes: $showResetTimes)
            }
            if let balance = quota.hoursBalance {
                if !meters.isEmpty { RowDivider() }
                TextRow(title: "Hours Balance", value: Format.hours(balance), detail: "Separate from the daily pool")
            }
            if let spend = quota.monthlySpendUsd, !meters.contains(where: { $0.unit == "usd" }) {
                RowDivider()
                TextRow(title: "Monthly Spend", value: Format.usd(spend))
            }
            if sessions.isEmpty {
                if !meters.isEmpty || quota.hoursBalance != nil { RowDivider() }
                TextRow(title: "Sessions", value: "No active session", valueStyle: AnyShapeStyle(.secondary))
            } else {
                ForEach(Array(sessions.enumerated()), id: \.offset) { index, expiry in
                    RowDivider()
                    TextRow(
                        title: sessions.count == 1 ? "Active Session" : "Session \(index + 1)",
                        value: "\(Format.duration(expiry.timeIntervalSince(now))) left",
                        detail: "Ends \(Format.time(expiry))"
                    )
                }
            }
            if let warning = quota.warning, !warning.isEmpty {
                RowDivider()
                Label(warning, systemImage: "exclamationmark.triangle.fill")
                    .font(.system(size: 11))
                    .foregroundStyle(Theme.notice)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 7)
            }
        }
        .padding(.vertical, 3)
        .cardSurface()
    }

    // MARK: - Problems

    @ViewBuilder
    private func problem(code: String, message: String) -> some View {
        switch code {
        case "signed-out", "expired":
            NoticeCard(
                systemImage: "person.crop.circle.badge.exclamationmark",
                title: code == "expired" ? "Your Freebuff login expired" : "Sign in to Freebuff",
                message: "Run freebuff login in Terminal, then refresh. FreeTokens reads that login and never asks for your password."
            ) {
                Button("Copy Command") {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString("freebuff login", forType: .string)
                }
                Button("Refresh") { Task { await model.refresh() } }
                    .keyboardShortcut(.defaultAction)
            }
        case "approval":
            NoticeCard(
                systemImage: "key.fill",
                title: "Allow Keychain access",
                message: "macOS needs your OK before FreeTokens can read Freebuff's login. Choose Always Allow when it asks."
            ) {
                Button("Allow Access…") { Task { await model.refresh(interactive: true) } }
                    .keyboardShortcut(.defaultAction)
            }
        case "network":
            NoticeCard(systemImage: "wifi.exclamationmark", title: "Can't reach Freebuff", message: message) {
                Button("Try Again") { Task { await model.refresh() } }
            }
        default:
            NoticeCard(systemImage: "exclamationmark.triangle.fill", title: "Freebuff usage is unavailable", message: message) {
                Button("Try Again") { Task { await model.refresh() } }
            }
        }
    }
}

/// A bounded meter: label, capsule bar (what's left), then "3.5 hrs left" ⟷ "Resets in 6h 12m".
/// Clicking the reset text switches between a countdown and the clock time, like QuotaBar.
struct MeterRow: View {
    let meter: Meter
    let now: Date
    @Binding var showResetTimes: Bool

    var body: some View {
        let severity = Theme.severity(remaining: meter.remainingFraction)
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 6) {
                Text(meter.label)
                    .font(.system(size: Theme.label, weight: .semibold))
                    .lineLimit(1)
                Spacer(minLength: 4)
                if meter.remaining <= 0 {
                    Label("Limit reached", systemImage: "flame.fill")
                        .font(.system(size: 11, weight: .medium))
                        .foregroundStyle(Theme.meter(.critical))
                }
            }
            CapsuleMeter(fraction: meter.remainingFraction, color: Theme.meter(severity))
                .help("\(leftText) of \(limitText)")
            HStack {
                Text(leftText)
                    .font(.system(size: Theme.supporting, weight: .medium))
                    .monospacedDigit()
                Spacer(minLength: 8)
                if let reset = meter.resetsAt {
                    Button {
                        showResetTimes.toggle()
                    } label: {
                        Text(showResetTimes ? "Resets \(Format.time(reset))" : "Resets in \(Format.duration(reset.timeIntervalSince(now)))")
                            .font(.system(size: Theme.supporting))
                            .foregroundStyle(.secondary)
                            .monospacedDigit()
                    }
                    .buttonStyle(.plain)
                    .help(showResetTimes ? "Show a countdown" : "Show the reset time")
                }
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .accessibilityElement(children: .combine)
    }

    private var leftText: String {
        switch meter.unit {
        case "hours": return "\(Format.hours(meter.remaining)) left"
        case "usd": return "\(Format.usd(meter.remaining)) left"
        case "sessions": return "\(Format.number(meter.remaining)) of \(Format.number(meter.limit)) sessions left"
        default: return "\(Format.number(meter.remaining)) left"
        }
    }

    private var limitText: String {
        switch meter.unit {
        case "hours": return Format.hours(meter.limit)
        case "usd": return Format.usd(meter.limit)
        default: return Format.number(meter.limit)
        }
    }
}
