#!/usr/bin/env bash
# Runs the Torob seller extraction in http mode and prints a summary: seller
# count, expected_count, complete flag, first and last seller, and warnings.
#
# Usage: scripts/verify-sellers.sh <torob product url>   (or TEST_URL=<url>)
# Extra flags can be passed via VERIFY_FLAGS, e.g. VERIFY_FLAGS='-timeout 90s'.
# Needs Go and python3 (standard library only). Run it from a network that can
# reach torob.com. The full JSON is kept in $OUT_DIR/sellers.json.
set -u
cd "$(dirname "$0")/.."

URL="${1:-${TEST_URL:-}}"
if [ -z "$URL" ]; then
  echo "usage: scripts/verify-sellers.sh <url>   (or set TEST_URL)" >&2
  exit 2
fi

OUT_DIR="${OUT_DIR:-verify-out}"
mkdir -p "$OUT_DIR"
JSON="$OUT_DIR/sellers.json"
rm -f "$JSON"

go build -o simulate-browser . || exit 1
# shellcheck disable=SC2086
./simulate-browser -url "$URL" -mode http -extract-torob-sellers -out "$JSON" ${VERIFY_FLAGS:-}
rc=$?
echo "exit code: $rc"

if [ -f "$JSON" ]; then
  python3 - "$JSON" <<'PY'
import json, sys
d = json.load(open(sys.argv[1], encoding="utf-8"))
s = d["sellers"]
def show(x):
    return "%s — %s (price=%s, available=%s)" % (x["name"], x["price_text"], x["price"], x["available"])
print("---- summary ----")
print("mode:           ", d["mode"])
print("product:        ", d["product"]["title"], d["product"]["id"])
print("currency:       ", d["currency"])
print("seller_count:   ", d["seller_count"])
print("expected_count: ", d["expected_count"])
print("complete:       ", d["complete"])
if s:
    print("first seller:   ", show(s[0]))
    print("last seller:    ", show(s[-1]))
print("warnings:       ", d["warnings"] or "none")
PY
fi
exit $rc
