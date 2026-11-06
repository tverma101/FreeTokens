#!/usr/bin/env bash
# Builds FreeTokens.app: the SwiftUI menu bar app with the Go engine inside it at
# Contents/Helpers/freetokens. Writes dist-app/FreeTokens.app and dist-app/FreeTokens-<version>-macOS.zip.
#
#   scripts/build-macos-app.sh [version]        # universal (arm64 + x86_64)
#   ARCHS=arm64 scripts/build-macos-app.sh dev  # faster local build
set -euo pipefail

VERSION="${1:-dev}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${OUT:-$ROOT/dist-app}"
ARCHS="${ARCHS:-arm64 x86_64}"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo none)"
DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
APP="$OUT/FreeTokens.app"
WORK="$OUT/.work"

rm -rf "$APP" "$WORK"
rm -f "$OUT"/FreeTokens-*-macOS.zip  # the release globs this folder, so keep only this build's zip
mkdir -p "$WORK" "$APP/Contents/MacOS" "$APP/Contents/Helpers" "$APP/Contents/Resources"

# Engine: one cgo build per architecture (menu bar + silent Keychain reads), merged with lipo.
engines=()
for arch in $ARCHS; do
  goarch="$arch"; [ "$arch" = x86_64 ] && goarch=amd64
  (cd "$ROOT" && CGO_ENABLED=1 GOOS=darwin GOARCH="$goarch" MACOSX_DEPLOYMENT_TARGET=12.0 go build -trimpath \
    -ldflags "-s -w -X main.version=$VERSION -X main.commit=$COMMIT -X main.date=$DATE" \
    -o "$WORK/freetokens-$arch" ./cmd/freetokens)
  engines+=("$WORK/freetokens-$arch")
done
lipo -create "${engines[@]}" -output "$APP/Contents/Helpers/freetokens"

# Menu bar app.
swift_archs=()
for arch in $ARCHS; do swift_archs+=(--arch "$arch"); done
swift build -c release --package-path "$ROOT/macos" "${swift_archs[@]}"
bin="$(swift build -c release --package-path "$ROOT/macos" "${swift_archs[@]}" --show-bin-path)/FreeTokens"
cp "$bin" "$APP/Contents/MacOS/FreeTokens"

build="$(printf '%s' "$VERSION" | sed -E 's/^v//; s/[^0-9.].*$//')"
[ -n "$build" ] || build=0
sed -e "s/__VERSION__/${VERSION#v}/" -e "s/__BUILD__/$build/" "$ROOT/macos/Resources/Info.plist" > "$APP/Contents/Info.plist"
plutil -lint "$APP/Contents/Info.plist" >/dev/null
cp "$ROOT/macos/Resources/AppIcon.icns" "$APP/Contents/Resources/AppIcon.icns"
cp "$ROOT/LICENSE" "$ROOT/NOTICE" "$APP/Contents/Resources/"

# Ad-hoc signature (inside out), so the bundle is sealed and runs on Apple silicon. Releases aren't
# Developer ID-signed or notarized yet.
codesign --force --sign - --identifier io.github.tverma101.freetokens.engine "$APP/Contents/Helpers/freetokens"
codesign --force --sign - --identifier io.github.tverma101.FreeTokens "$APP"
codesign --verify --deep --strict "$APP"

rm -rf "$WORK"
zip="$OUT/FreeTokens-${VERSION#v}-macOS.zip"
ditto -c -k --sequesterRsrc --keepParent "$APP" "$zip"
echo "$zip"
