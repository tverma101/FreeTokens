#!/usr/bin/env bash
# Smoke test for the macOS app zip: verify checksum and signature, check both architectures, run the
# bundled engine, and render the menu bar panel offscreen with no Freebuff account.
# Usage: scripts/smoke-app.sh <dist-dir> <zip-name> [panel.png]
# Expects a Desktop fixture in $HOME (see scripts/mkfixture.go).
set -euo pipefail
dist=$1 zip=$2 png=${3:-}
work=$(mktemp -d)
cd "$dist"
grep " ${zip}\$" checksums.txt | shasum -a 256 -c -
ditto -x -k "$zip" "$work"
app="$work/FreeTokens.app"
engine="$app/Contents/Helpers/freetokens"

codesign --verify --deep --strict "$app"
for bin in "$app/Contents/MacOS/FreeTokens" "$engine"; do
  archs=$(lipo -archs "$bin")
  echo "$(basename "$bin"): $archs"
  [[ "$archs" == *arm64* && "$archs" == *x86_64* ]] || { echo "not universal: $bin"; exit 1; }
done
for f in Info.plist Resources/AppIcon.icns Resources/LICENSE Resources/NOTICE; do
  [ -f "$app/Contents/$f" ] || { echo "missing $f"; exit 1; }
done

version=$(/usr/libexec/PlistBuddy -c "Print :CFBundleShortVersionString" "$app/Contents/Info.plist")
echo "app version=$version"
[[ "$zip" == "FreeTokens-$version-macOS.zip" ]] || { echo "zip name and app version differ"; exit 1; }
"$engine" version | tee version.txt
grep -q "$version" version.txt

export FREEBUFF_CONFIG_DIR="$work/no-login"   # absolute and empty: no credentials
export FREETOKENS_CONFIG_DIR="$work/cfg" FREETOKENS_CACHE_DIR="$work/cache"
unset CODEBUFF_API_KEY || true
mkdir -p "$FREEBUFF_CONFIG_DIR"

"$engine" status --json > status.json || true
jq -e '.quotaErrorCode == "signed-out"' status.json > /dev/null
"$engine" update --json --check | jq -e 'has("latest") or has("error")' > /dev/null

# Inside the bundle, autostart opens the app rather than running the engine directly.
"$engine" autostart on | tee autostart.txt
grep -q "/usr/bin/open -a .*/FreeTokens.app" autostart.txt
"$engine" autostart off

# Launch the real app in snapshot mode: it runs the bundled engine and renders the panel.
out=${png:-$work/panel.png}
FREETOKENS_SNAPSHOT="$out" "$app/Contents/MacOS/FreeTokens" &
pid=$!
for _ in $(seq 1 60); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
if kill -0 "$pid" 2>/dev/null; then kill "$pid"; echo "app did not finish its snapshot"; exit 1; fi
[ -s "$out" ] || { echo "no panel snapshot"; exit 1; }
echo "panel snapshot: $out"
echo "APP SMOKE OK"
