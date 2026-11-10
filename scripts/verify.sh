#!/usr/bin/env bash
# Runs simulate-browser against a test URL in http, browser and auto mode and
# prints a summary: status code, final URL, HTML size, <title>, which mode
# succeeded and whether the output is a challenge/block page.
#
# Usage: scripts/verify.sh <url>        (or: TEST_URL=<url> scripts/verify.sh)
# Extra flags can be passed via VERIFY_FLAGS, e.g. VERIFY_FLAGS='-timeout 90s'.
# Run it from a network that can reach the site (e.g. an Iranian residential IP).
set -u
cd "$(dirname "$0")/.."

URL="${1:-${TEST_URL:-}}"
if [ -z "$URL" ]; then
  echo "usage: scripts/verify.sh <url>   (or set TEST_URL)" >&2
  exit 2
fi

OUT_DIR="${OUT_DIR:-verify-out}"
mkdir -p "$OUT_DIR"

echo "== environment"
echo "date:   $(date -u +%FT%TZ)"
echo "go:     $(go version 2>&1)"
for b in "${CHROME_PATH:-}" google-chrome google-chrome-stable chromium chromium-browser; do
  if [ -n "$b" ] && command -v "$b" >/dev/null 2>&1; then echo "chrome: $("$b" --version 2>&1)"; break; fi
done
echo "proxy:  ${HTTPS_PROXY:-<none>}"
echo "url:    $URL"

echo "== build"
go build -o "$OUT_DIR/simulate-browser" . || { echo "build failed" >&2; exit 1; }

declare -A RC
for mode in http browser auto; do
  echo
  echo "================ mode=$mode ================"
  # shellcheck disable=SC2086
  "$OUT_DIR/simulate-browser" -url "$URL" -mode "$mode" -out "$OUT_DIR/$mode.html" ${VERIFY_FLAGS:-} 2>&1
  RC[$mode]=$?
done

echo
echo "================ summary ================"
for mode in http browser auto; do
  if [ "${RC[$mode]}" -eq 0 ]; then verdict="SUCCESS (real content)"; else verdict="FAILED (challenge/block page, error or no expected content)"; fi
  echo "mode=$mode exit=${RC[$mode]} -> $verdict"
done
echo "HTML saved under $OUT_DIR/ (http.html, browser.html, auto.html)"
# Exit non-zero unless auto mode produced real content.
[ "${RC[auto]}" -eq 0 ]
