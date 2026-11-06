import SwiftUI

enum PanelScreen { case dashboard, settings }

/// Panel-only state shared with the window controller.
@MainActor
final class PanelState: ObservableObject {
    @Published var screen: PanelScreen = .dashboard
    @Published var contentHeight: CGFloat = 320
    /// The tallest the scrolling region may get on the current screen.
    @Published var maxContentHeight: CGFloat = 640
}

struct PanelView: View {
    static let width: CGFloat = 340
    static let footerHeight: CGFloat = 52

    @ObservedObject var model: AppModel
    @ObservedObject var state: PanelState

    var body: some View {
        VStack(spacing: 0) {
            ScrollView(.vertical, showsIndicators: state.contentHeight > state.maxContentHeight) {
                Group {
                    switch state.screen {
                    case .dashboard: DashboardView(model: model)
                    case .settings: SettingsView(model: model)
                    }
                }
                .padding(12)
                .frame(width: Self.width)
                .background(GeometryReader { proxy in
                    Color.clear.preference(key: ContentHeightKey.self, value: proxy.size.height)
                })
            }
            .frame(height: min(state.contentHeight, state.maxContentHeight))
            FooterView(model: model, state: state)
                .frame(height: Self.footerHeight)
        }
        .frame(width: Self.width)
        .background(Theme.tray)
        .onPreferenceChange(ContentHeightKey.self) { height in
            if abs(height - state.contentHeight) > 0.5 { state.contentHeight = height }
        }
    }
}

private struct ContentHeightKey: PreferenceKey {
    static var defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
}

struct DashboardView: View {
    @ObservedObject var model: AppModel

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if case let .available(version, url) = model.update {
                NoticeCard(
                    systemImage: "arrow.down.circle.fill",
                    title: "FreeTokens v\(version) is available",
                    message: "It's checked against the release's checksums before it replaces this app.",
                    tint: .accentColor
                ) {
                    if let url, let link = URL(string: url) {
                        Link("Release Notes", destination: link)
                    }
                    Button("Install and Relaunch") { model.installUpdate() }
                        .keyboardShortcut(.defaultAction)
                }
            }
            FreebuffSection(model: model)
            if let snapshot = model.snapshot, !(snapshot.periods ?? []).isEmpty {
                UsageSection(snapshot: snapshot)
            }
            if let days = model.snapshot?.days, days.contains(where: { $0.tokens > 0 }) {
                TrendSection(days: days)
            }
        }
    }
}

/// Fixed footer chrome: identity and freshness on the left, actions on the right.
struct FooterView: View {
    @ObservedObject var model: AppModel
    @ObservedObject var state: PanelState

    var body: some View {
        HStack(spacing: 6) {
            VStack(alignment: .leading, spacing: 1) {
                Text(state.screen == .settings ? "Settings" : "FreeTokens")
                    .font(.system(size: 12, weight: .semibold))
                status
            }
            Spacer(minLength: 8)
            if state.screen == .dashboard {
                ChromeButton(systemImage: "arrow.clockwise", label: "Refresh Now", spinning: model.isRefreshing) {
                    Task { await model.refresh(interactive: true) }
                }
                .keyboardShortcut("r", modifiers: .command)
                ChromeButton(systemImage: "gearshape", label: "Settings") {
                    withAnimation(.easeOut(duration: 0.15)) { state.screen = .settings }
                }
                .keyboardShortcut(",", modifiers: .command)
            } else {
                Button("Done") {
                    withAnimation(.easeOut(duration: 0.15)) { state.screen = .dashboard }
                }
                .controlSize(.small)
                .keyboardShortcut(.defaultAction)
            }
        }
        .padding(.horizontal, 14)
        .frame(maxHeight: .infinity)
        .background(Theme.cardFill.opacity(0.6))
        .overlay(alignment: .top) {
            Rectangle().fill(Color.primary.opacity(0.08)).frame(height: 0.5)
        }
    }

    @ViewBuilder
    private var status: some View {
        TimelineView(.periodic(from: .now, by: 30)) { context in
            Group {
                if model.isRefreshing {
                    Text("Refreshing…")
                } else if let warning = model.snapshot?.historyWarning, state.screen == .dashboard {
                    Label("Some history is incomplete", systemImage: "exclamationmark.triangle.fill")
                        .foregroundStyle(Theme.notice)
                        .help(warning)
                } else if let refreshed = model.snapshot?.refreshedAt {
                    Text("Updated \(Format.ago(refreshed, now: context.date))")
                } else {
                    Text(model.isRelease ? "v\(model.version)" : "Development build")
                }
            }
            .font(.system(size: 11))
            .foregroundStyle(.secondary)
            .lineLimit(1)
        }
    }
}
