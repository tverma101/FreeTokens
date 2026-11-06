import Foundation

enum Format {
    /// 20.7M, 1.2K, 3.4B.
    static func tokens(_ value: Int64) -> String {
        let v = Double(value)
        switch abs(v) {
        case 1_000_000_000...: return trimmed(v / 1_000_000_000) + "B"
        case 1_000_000...: return trimmed(v / 1_000_000) + "M"
        case 1_000...: return trimmed(v / 1_000) + "K"
        default: return String(value)
        }
    }

    private static func trimmed(_ v: Double) -> String {
        let text = v >= 100 ? String(format: "%.0f", v) : String(format: "%.1f", v)
        return text.hasSuffix(".0") ? String(text.dropLast(2)) : text
    }

    /// $4.65, <$0.01, $1,234.
    static func usd(_ value: Double, lowerBound: Bool = false) -> String {
        let prefix = lowerBound ? "≥" : ""
        if value > 0 && value < 0.01 { return prefix + "<$0.01" }
        let formatter = NumberFormatter()
        formatter.numberStyle = .currency
        formatter.currencyCode = "USD"
        formatter.locale = Locale(identifier: "en_US")
        formatter.maximumFractionDigits = value >= 1000 ? 0 : 2
        return prefix + (formatter.string(from: NSNumber(value: value)) ?? String(format: "$%.2f", value))
    }

    /// 3.5, 12, 0.25 without trailing zeros.
    static func number(_ value: Double) -> String {
        let formatter = NumberFormatter()
        formatter.maximumFractionDigits = 2
        formatter.minimumFractionDigits = 0
        return formatter.string(from: NSNumber(value: value)) ?? String(value)
    }

    static func hours(_ value: Double) -> String {
        "\(number(value)) \(value == 1 ? "hr" : "hrs")"
    }

    /// 2d 3h, 6h 12m, 42m, <1m.
    static func duration(_ interval: TimeInterval) -> String {
        let minutes = Int((max(interval, 0) / 60).rounded(.up))
        if minutes < 1 { return "<1m" }
        let days = minutes / 1440, hours = (minutes % 1440) / 60, mins = minutes % 60
        if days > 0 { return hours > 0 ? "\(days)d \(hours)h" : "\(days)d" }
        if hours > 0 { return mins > 0 ? "\(hours)h \(mins)m" : "\(hours)h" }
        return "\(mins)m"
    }

    static func ago(_ date: Date, now: Date = Date()) -> String {
        let seconds = now.timeIntervalSince(date)
        switch seconds {
        case ..<60: return "just now"
        case ..<3600: return "\(Int(seconds / 60)) min ago"
        case ..<86400: return "\(Int(seconds / 3600)) hr ago"
        default: return "\(Int(seconds / 86400)) days ago"
        }
    }

    static func time(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.timeStyle = .short
        formatter.dateStyle = Calendar.current.isDateInToday(date) ? .none : .short
        formatter.doesRelativeDateFormatting = true
        return formatter.string(from: date)
    }

    /// "deepseek/deepseek-v4-flash" → "deepseek-v4-flash".
    static func modelName(_ route: String) -> String {
        route.split(separator: "/").last.map(String.init) ?? route
    }

    /// "2026-11-03" → "Tue, Nov 3".
    static func day(_ key: String) -> String {
        let parser = DateFormatter()
        parser.calendar = Calendar(identifier: .gregorian)
        parser.dateFormat = "yyyy-MM-dd"
        guard let date = parser.date(from: key) else { return key }
        let out = DateFormatter()
        out.setLocalizedDateFormatFromTemplate("EEE MMM d")
        return out.string(from: date)
    }
}
