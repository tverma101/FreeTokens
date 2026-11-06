// swift-tools-version: 5.9
// The native macOS menu bar app. It is UI only: the bundled Go engine (Contents/Helpers/freetokens)
// reads Freebuff and prints JSON. Build the .app with scripts/build-macos-app.sh.
import PackageDescription

let package = Package(
    name: "FreeTokensMac",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(name: "FreeTokens", path: "Sources/FreeTokens")
    ]
)
