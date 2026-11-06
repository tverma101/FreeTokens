import AppKit
import SwiftUI

/// Settings, shown inside the panel like QuotaBar's: grouped cards of label ⟷ control rows.
struct SettingsView: View {
    @ObservedObject var model: AppModel

    private static let refreshChoices = [1, 2, 5, 10, 15, 30, 60]

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            group("General") {
                toggleRow("Start at Login", isOn: Binding(get: { model.startsAtLogin }, set: { model.setStartsAtLogin($0) }))
                RowDivider()
                toggleRow("Session Timer in Menu Bar", isOn: Binding(get: { model.settings.showTimers }, set: { model.setShowTimers($0) }))
                RowDivider()
                row("Refresh Every") {
                    Picker("Refresh Every", selection: Binding(get: { model.settings.refreshMinutes }, set: { model.setRefreshMinutes($0) })) {
                        ForEach(choices, id: \.self) { minutes in
                            Text(minutes == 60 ? "1 hour" : "\(minutes) min").tag(minutes)
                        }
                    }
                    .labelsHidden()
                    .fixedSize()
                }
            }
            group("Updates") {
                row("GitHub Releases") {
                    Picker("Updates", selection: Binding(get: { model.settings.autoUpdate }, set: { model.setAutoUpdate($0) })) {
                        Text("Never Check").tag("off")
                        Text("Check Daily and Notify").tag("notify")
                        Text("Check Daily and Install").tag("auto")
                    }
                    .labelsHidden()
                    .fixedSize()
                }
                RowDivider()
                HStack(spacing: 8) {
                    updateStatus
                    Spacer(minLength: 8)
                    updateButton
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
            }
            if let error = model.settingsError {
                Label(error, systemImage: "exclamationmark.triangle.fill")
                    .font(.system(size: 11))
                    .foregroundStyle(Theme.notice)
                    .padding(.horizontal, 4)
            }
            group("About") {
                HStack(spacing: 10) {
                    Image(nsImage: NSApp.applicationIconImage)
                        .resizable()
                        .frame(width: 32, height: 32)
                    VStack(alignment: .leading, spacing: 2) {
                        Text("FreeTokens \(model.isRelease ? "v" + model.version : "development build")")
                            .font(.system(size: Theme.label, weight: .semibold))
                        Text("Unofficial. Not affiliated with Freebuff or Codebuff.")
                            .font(.system(size: 11))
                            .foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 0)
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                RowDivider()
                HStack(spacing: 14) {
                    Link("GitHub", destination: URL(string: "https://github.com/tverma101/FreeTokens")!)
                    Link("Report an Issue", destination: URL(string: "https://github.com/tverma101/FreeTokens/issues")!)
                    Spacer()
                    Button("Quit FreeTokens") { NSApp.terminate(nil) }
                        .controlSize(.small)
                }
                .font(.system(size: Theme.supporting))
                .padding(.horizontal, 14)
                .padding(.vertical, 8)
            }
        }
    }

    private var choices: [Int] {
        Self.refreshChoices.contains(model.settings.refreshMinutes)
            ? Self.refreshChoices
            : (Self.refreshChoices + [model.settings.refreshMinutes]).sorted()
    }

    private var updateStatus: some View {
        let (text, tint) = updateText
        return Text(text)
            .font(.system(size: 11))
            .foregroundStyle(tint.map(AnyShapeStyle.init) ?? AnyShapeStyle(.secondary))
            .fixedSize(horizontal: false, vertical: true)
    }

    private var updateText: (String, Color?) {
        let text: String
        var tint: Color? = nil
        switch model.update {
        case .idle: text = model.settings.autoUpdate == "off" ? "FreeTokens never contacts GitHub unless you ask." : "Checks once a day."
        case .checking: text = "Checking…"
        case .upToDate: text = "FreeTokens is up to date."; tint = Theme.positive
        case let .available(version, _): text = "v\(version) is available."
        case let .installing(version): text = "Installing v\(version)…"
        case let .failed(message): text = message; tint = Theme.notice
        }
        return (text, tint)
    }

    @ViewBuilder
    private var updateButton: some View {
        switch model.update {
        case .available:
            Button("Install and Relaunch") { model.installUpdate() }
                .controlSize(.small)
        case .checking, .installing:
            ProgressView().controlSize(.small)
        default:
            Button("Check Now") { model.checkForUpdates() }
                .controlSize(.small)
                .disabled(!model.isRelease)
                .help(model.isRelease ? "Check GitHub Releases now" : "Development builds don't update")
        }
    }

    private func group<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            SectionHeader(title: title)
            VStack(spacing: 0) { content() }
                .padding(.vertical, 3)
                .cardSurface()
        }
    }

    private func row<Control: View>(_ title: String, @ViewBuilder control: () -> Control) -> some View {
        HStack {
            Text(title).font(.system(size: Theme.label))
            Spacer(minLength: 8)
            control().controlSize(.small)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 6)
    }

    private func toggleRow(_ title: String, isOn: Binding<Bool>) -> some View {
        row(title) {
            Toggle(title, isOn: isOn)
                .labelsHidden()
                .toggleStyle(.switch)
        }
    }
}
