import Foundation

/// `freetokens status --json`.
struct Snapshot: Decodable, Equatable {
    var refreshedAt: Date
    var quota: Quota?
    var quotaError: String?
    var quotaErrorCode: String?
    var historyWarning: String?
    var periods: [Period]?
    var days: [Day]?
    var sourceNote: String?
}

struct Quota: Decodable, Equatable {
    var status: String
    var plan: String?
    var meters: [Meter]?
    var hoursBalance: Double?
    var monthlySpendUsd: Double?
    var activeSessions: [Date]?
    var warning: String?
}

struct Meter: Decodable, Equatable, Identifiable {
    var label: String
    var used: Double
    var limit: Double
    var unit: String
    var resetsAt: Date?
    var detail: String?

    var id: String { label }
    var remaining: Double { max(limit - used, 0) }
    /// Share of the limit still available, 0...1.
    var remainingFraction: Double { limit > 0 ? min(max(remaining / limit, 0), 1) : 0 }
}

struct Period: Decodable, Equatable {
    var label: String
    var tokens: Int64
    var costUsd: Double?
    var costLowerBound: Bool
    var partial: Bool
    var models: [ModelRow]?
    var hasData: Bool
}

struct ModelRow: Decodable, Equatable, Identifiable {
    var name: String
    var tokens: Int64
    var costUsd: Double?
    var costComplete: Bool

    var id: String { name }
}

struct Day: Decodable, Equatable, Identifiable {
    var date: String
    var tokens: Int64
    var costUsd: Double?
    var costComplete: Bool
    var partial: Bool

    var id: String { date }
}

/// `freetokens config get`.
struct EngineSettings: Decodable, Equatable {
    var autoUpdate: String = "off"
    var refreshMinutes: Int = 5
    var showTimers: Bool = true
}

/// `freetokens update --json`.
struct UpdateResult: Decodable, Equatable {
    var current: String?
    var latest: String?
    var available: Bool?
    var url: String?
    var installed: Bool?
    var skipped: String?
    var error: String?
}

extension JSONDecoder {
    /// Go writes RFC 3339 times with optional fractional seconds.
    static let engine: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .custom { decoder in
            let text = try decoder.singleValueContainer().decode(String.self)
            if let date = DateParsing.parse(text) { return date }
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "Bad date \(text)"))
        }
        return decoder
    }()
}

enum DateParsing {
    static func parse(_ text: String) -> Date? {
        let fractional = ISO8601DateFormatter()
        fractional.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        if let date = fractional.date(from: text) { return date }
        let plain = ISO8601DateFormatter()
        plain.formatOptions = [.withInternetDateTime]
        return plain.date(from: text)
    }
}
