#!/usr/bin/env bash
# Fingerprint check: fetches https://tls.peet.ws/api/all with the tool in http
# mode and prints what the server saw: JA4, Akamai HTTP/2 fingerprint,
# User-Agent and header order.
#
# With a reference JSON (the same endpoint saved from a real Chrome) it also
# prints a field-by-field MATCH/MISMATCH comparison of JA4 and the HTTP/2
# fingerprint. JA3 is shown for information only: Chrome randomizes TLS
# extension order, so JA3 differs on every connection.
#
# Usage: scripts/fingerprint.sh [reference.json]
# Env:   OUT_DIR=verify-out   where outputs are written
#        OURS_JSON=<file>     skip the fetch and use this saved JSON as "ours"
# Needs: go, python3 (standard library only). Run it WITHOUT a TLS-intercepting
# proxy, otherwise the server sees the proxy's fingerprint, not this tool's.
set -u
cd "$(dirname "$0")/.."

REF="${1:-}"
OUT_DIR="${OUT_DIR:-verify-out}"
OURS="${OURS_JSON:-$OUT_DIR/fingerprint.json}"
mkdir -p "$OUT_DIR"

if [ -n "$REF" ] && [ ! -f "$REF" ]; then
  echo "reference file not found: $REF" >&2
  exit 2
fi

if [ -z "${OURS_JSON:-}" ]; then
  if [ -n "${HTTPS_PROXY:-}${https_proxy:-}" ]; then
    echo "WARNING: HTTPS_PROXY is set. If the proxy re-terminates TLS, the server" >&2
    echo "         reports the proxy's fingerprint and this check is meaningless." >&2
  fi
  go build -o "$OUT_DIR/simulate-browser" . || { echo "build failed" >&2; exit 1; }
  # -expect '"ja4"' because this endpoint returns JSON, not a product page.
  "$OUT_DIR/simulate-browser" -url https://tls.peet.ws/api/all -mode http \
    -expect '"ja4"' -out "$OURS" || { echo "fetch failed; see output above" >&2; exit 1; }
  echo
fi

python3 - "$OURS" "$REF" <<'PY'
import json, sys

def load(path):
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError) as e:
        sys.exit(f"{path}: cannot read as JSON ({e}). Is it the tls.peet.ws/api/all response?")

def extract(d, path):
    tls = d.get("tls") or {}
    h2 = d.get("http2") or {}
    ja4 = tls.get("ja4")
    akamai = h2.get("akamai_fingerprint")
    if not ja4 or not akamai:
        sys.exit(f"{path}: missing tls.ja4 / http2.akamai_fingerprint. "
                 "Expected the JSON from https://tls.peet.ws/api/all.")
    names, pseudo = [], []
    for fr in h2.get("sent_frames") or []:
        if fr.get("frame_type") == "HEADERS":
            for line in fr.get("headers") or []:
                name = line.split(":", 1)[0] if not line.startswith(":") else ":" + line[1:].split(":", 1)[0]
                (pseudo if name.startswith(":") else names).append(name.lower())
            break
    return {
        "ja4": ja4,
        "akamai": akamai,
        "akamai_hash": h2.get("akamai_fingerprint_hash", ""),
        "ja3": tls.get("ja3", ""),
        "ua": d.get("user_agent", ""),
        "http_version": d.get("http_version", ""),
        "pseudo": pseudo,
        "headers": names,
    }

def show(title, v):
    print(f"== {title}")
    print(f"HTTP version:        {v['http_version']}")
    print(f"JA4:                 {v['ja4']}")
    print(f"Akamai HTTP/2:       {v['akamai']}")
    print(f"Pseudo-header order: {','.join(v['pseudo'])}")
    print(f"User-Agent:          {v['ua']}")
    print(f"Header order:        {', '.join(v['headers'])}")
    print(f"JA3 (info only):     {v['ja3']}")

ours = extract(load(sys.argv[1]), sys.argv[1])
show("this tool (http mode)", ours)
if not sys.argv[2]:
    print("\nNo reference JSON given: nothing to compare. See README for how to save one.")
    sys.exit(0)

ref = extract(load(sys.argv[2]), sys.argv[2])
print()
show("reference (real Chrome)", ref)

def parts(ja4):
    p = ja4.split("_")
    return p + [""] * (3 - len(p))

common = [h for h in ours["headers"] if h in ref["headers"]]
common_ref = [h for h in ref["headers"] if h in ours["headers"]]
rows = [
    ("JA4 (full)",                ours["ja4"], ref["ja4"]),
    ("JA4 part a (proto/ver/counts)", parts(ours["ja4"])[0], parts(ref["ja4"])[0]),
    ("JA4 part b (ciphers)",      parts(ours["ja4"])[1], parts(ref["ja4"])[1]),
    ("JA4 part c (exts+sigalgs)", parts(ours["ja4"])[2], parts(ref["ja4"])[2]),
    ("HTTP/2 Akamai fingerprint", ours["akamai"], ref["akamai"]),
    ("HTTP/2 pseudo-header order", ",".join(ours["pseudo"]), ",".join(ref["pseudo"])),
    ("Header order (strict)",     ",".join(ours["headers"]), ",".join(ref["headers"])),
    ("Header order (common headers only)", ",".join(common), ",".join(common_ref)),
]
print("\n== comparison (ours vs reference)")
bad = 0
for name, a, b in rows:
    ok = a == b
    bad += not ok
    print(f"{'MATCH   ' if ok else 'MISMATCH'}  {name}")
    if not ok:
        print(f"            ours: {a}\n            ref:  {b}")
print(f"\nUser-Agent (info): {'same' if ours['ua'] == ref['ua'] else 'differs (expected if Chrome versions differ)'}")
print(f"JA3 (info):        {'same' if ours['ja3'] == ref['ja3'] else 'differs (expected: Chrome randomizes extension order)'}")
print(f"\n{'ALL CHECKED FIELDS MATCH' if not bad else str(bad) + ' MISMATCH(ES)'}")
sys.exit(1 if bad else 0)
PY
