import Foundation

/// Runs the bundled Go engine. The app never talks to Freebuff itself.
enum Engine {
    struct Failure: LocalizedError {
        var message: String
        var errorDescription: String? { message }
    }

    struct Output {
        var stdout: Data
        var stderr: String
        var status: Int32
    }

    /// `FREETOKENS_ENGINE` overrides the bundled engine (development and screenshots).
    static var executableURL: URL? {
        if let override = ProcessInfo.processInfo.environment["FREETOKENS_ENGINE"], !override.isEmpty {
            return URL(fileURLWithPath: override)
        }
        let bundled = Bundle.main.bundleURL.appendingPathComponent("Contents/Helpers/freetokens")
        return FileManager.default.isExecutableFile(atPath: bundled.path) ? bundled : nil
    }

    static func run(_ arguments: [String], timeout: TimeInterval = 180) async throws -> Output {
        guard let url = executableURL else {
            throw Failure(message: "FreeTokens is missing its engine. Download FreeTokens again from the Releases page.")
        }
        return try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global(qos: .userInitiated).async {
                let process = Process()
                process.executableURL = url
                process.arguments = arguments
                let out = Pipe(), err = Pipe()
                process.standardOutput = out
                process.standardError = err
                process.standardInput = FileHandle.nullDevice
                do {
                    try process.run()
                } catch {
                    continuation.resume(throwing: Failure(message: "Couldn't start the FreeTokens engine: \(error.localizedDescription)"))
                    return
                }
                let timer = DispatchWorkItem { if process.isRunning { process.terminate() } }
                DispatchQueue.global().asyncAfter(deadline: .now() + timeout, execute: timer)
                // Drain stderr on its own queue so a chatty engine can never fill the pipe and stall.
                var stderrData = Data()
                let group = DispatchGroup()
                group.enter()
                DispatchQueue.global().async {
                    stderrData = err.fileHandleForReading.readDataToEndOfFile()
                    group.leave()
                }
                let stdoutData = out.fileHandleForReading.readDataToEndOfFile()
                process.waitUntilExit()
                group.wait()
                timer.cancel()
                let stderr = String(decoding: stderrData, as: UTF8.self)
                continuation.resume(returning: Output(stdout: stdoutData, stderr: stderr, status: process.terminationStatus))
            }
        }
    }

    /// Runs the engine and decodes its JSON stdout.
    static func json<T: Decodable>(_ type: T.Type, _ arguments: [String], timeout: TimeInterval = 180) async throws -> T {
        let output = try await run(arguments, timeout: timeout)
        if let value = try? JSONDecoder.engine.decode(T.self, from: output.stdout) {
            return value
        }
        throw Failure(message: lastLine(output.stderr) ?? "The FreeTokens engine exited with status \(output.status).")
    }

    /// Runs a command that prints text, failing with its last error line.
    @discardableResult
    static func command(_ arguments: [String]) async throws -> String {
        let output = try await run(arguments, timeout: 60)
        let text = String(decoding: output.stdout, as: UTF8.self)
        guard output.status == 0 else {
            throw Failure(message: lastLine(output.stderr) ?? "The FreeTokens engine exited with status \(output.status).")
        }
        return text.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private static func lastLine(_ text: String) -> String? {
        let line = text.split(whereSeparator: \.isNewline).last.map(String.init)?
            .replacingOccurrences(of: "freetokens: ", with: "")
        guard let line, !line.isEmpty else { return nil }
        return line
    }
}
