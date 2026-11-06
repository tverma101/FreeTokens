#!/usr/bin/env bash
# Install smoke test for a release archive: verify checksum, extract, and exercise every command
# with no Freebuff account. Usage: scripts/smoke.sh <dist-dir> <archive-name>
# Expects a Desktop fixture in $HOME (see scripts/mkfixture.go); runs with a throwaway HOME.
set -euo pipefail
dist=$1 archive=$2
work=$(mktemp -d)
cd "$dist"
grep " ${archive}\$" checksums.txt | sha256sum -c -
case "$archive" in
  *.zip) unzip -q "$archive" -d "$work" ;;
  *) tar -xzf "$archive" -C "$work" ;;
esac
bin="$work/freetokens"
[ -f "$bin.exe" ] && bin="$bin.exe"
cd "$work"
ls -l
for f in LICENSE NOTICE README.md; do [ -f "$f" ] || { echo "missing $f"; exit 1; }; done
if [[ "$archive" == *.zip ]]; then
  # Windows: windowless tray launcher plus icon/version resources on both executables.
  [ -f freetokensw.exe ] || { echo "missing freetokensw.exe"; exit 1; }
  for exe in freetokens.exe freetokensw.exe; do
    product=$(powershell -NoProfile -Command "(Get-Item '$exe').VersionInfo.ProductName")
    echo "$exe product=$product"
    [ "${product//$'\r'/}" = FreeTokens ] || { echo "$exe has no version resource"; exit 1; }
  done
fi

export FREEBUFF_CONFIG_DIR="$work/no-login"   # absolute and empty: no credentials
export FREETOKENS_CONFIG_DIR="$work/cfg" FREETOKENS_CACHE_DIR="$work/cache"
unset CODEBUFF_API_KEY || true
mkdir -p "$FREEBUFF_CONFIG_DIR"

"$bin" version
"$bin" help | grep -q "freetokens status"

set +e
"$bin" status > status.txt; code=$?
set -e
cat status.txt
grep -q "Not signed in to Freebuff" status.txt
grep -q "Today: " status.txt
echo "status exit=$code"

"$bin" status --json > status.json
tokens=$(jq '.periods[] | select(.label=="Last 30 Days") | .tokens' status.json)
today=$(jq '.periods[] | select(.label=="Today") | .tokens' status.json)
cost=$(jq '.periods[] | select(.label=="Today") | .costUsd // 0' status.json)
echo "30-day tokens=$tokens today=$today today-cost=$cost"
[ "$tokens" = 210000 ] && [ "$today" = 150000 ]

"$bin" diagnose | jq -e '.desktopDatabases == 1 and (.credentialsExists | not)' > /dev/null

"$bin" config set auto-update notify
"$bin" config get | jq -e '.autoUpdate == "notify"' > /dev/null
if "$bin" config set auto-update bogus 2>/dev/null; then echo "invalid value accepted"; exit 1; fi

"$bin" autostart on | tee autostart.txt
if [[ "$archive" == *.zip ]]; then grep -q 'freetokensw.exe" tray\|freetokensw.exe tray' autostart.txt; fi
[ "$("$bin" autostart)" = on ]
"$bin" autostart off
[ "$("$bin" autostart)" = off ]

"$bin" update --check
echo "SMOKE OK"
