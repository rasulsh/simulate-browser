# simulate-browser

A small Go CLI that fetches a web page the way a real Chrome would, to get past
bot detection. It has two strategies behind one flag:

| `--mode` | What it does |
|----------|--------------|
| `http`    | Chrome-impersonating HTTP client: Chrome TLS ClientHello (JA3/JA4), Chrome HTTP/2 SETTINGS and pseudo-header order, Chrome header order, cookie jar, redirects. Built on [`bogdanfinn/tls-client`](https://github.com/bogdanfinn/tls-client) (utls + fhttp), profile `Chrome_152`. |
| `browser` | Real headless Chrome driven over CDP ([chromedp](https://github.com/chromedp/chromedp)). Runs JavaScript challenges and renders JS-built pages; polls until the page stops looking like a challenge. |
| `auto` (default) | Tries `http` first, falls back to `browser` when the response is a challenge page, 403/429/503, or lacks the expected content. |

## Usage

```bash
go build -o simulate-browser .
./simulate-browser -url https://example.com/product/123 -mode auto -out page.html
```

Flags: `-url` (required), `-mode auto|http|browser`, `-out` (default `page.html`),
`-lang` (Accept-Language, default Persian), `-proxy` (default `$HTTPS_PROXY`),
`-expect` (regexp the HTML must match to count as real content; default matches
`"offers"`, `"price"`, toman/rial), `-timeout` (per strategy, default 60s).

It prints evidence (mode, status code, final URL, HTML size, `<title>`, challenge
flag) and exits 0 only if the saved HTML is real content, not a challenge/block page.

Requirements: Go (the `go` directive in `go.mod` may make Go download a newer
toolchain automatically) and, for `browser`/`auto`, Chrome or Chromium
(`google-chrome`, `chromium`, or `CHROME_PATH`).

## Layout

```
main.go           CLI, mode orchestration, evidence output
fetch_http.go     Chrome-impersonating HTTP client
fetch_browser.go  headless Chrome via CDP
detect.go         challenge/block detection, <title> extraction
*_test.go         unit tests + local-server integration tests
scripts/verify.sh runs all three modes against a URL and prints a summary
```

## Tests

```bash
go test ./...
```

Covers challenge detection, auto-mode fallback logic, redirects, cookies,
Chrome-like request headers, 403 handling, and a JavaScript challenge solved by
real headless Chrome (skipped if no Chrome is installed).

## Verifying against a real site

```bash
scripts/verify.sh https://torob.com/<product-page>
```

It runs `http`, `browser` and `auto` and prints status code, final URL, HTML size,
`<title>`, which mode succeeded and whether the output is a challenge page.
Run it from a network the target accepts (for torob.com: an Iranian IP).

## Fingerprint check

`scripts/fingerprint.sh` fetches `https://tls.peet.ws/api/all` with the tool in
`http` mode and prints what the server saw: JA4, the Akamai HTTP/2 fingerprint,
the pseudo-header order, the User-Agent and the header order. Given a reference
JSON saved from a real Chrome, it also prints a MATCH/MISMATCH comparison.

```bash
# 1. What does the tool look like to the server?
scripts/fingerprint.sh

# 2. Compare against a real Chrome (reference saved as described below)
scripts/fingerprint.sh chrome-ref.json
```

Needs Go and `python3` (standard library only). Run it **without** a
TLS-intercepting proxy: behind one, the server sees the proxy's fingerprint, not
the tool's (the script warns when `HTTPS_PROXY` is set). Exit code is 0 only if
every compared field matches. `OURS_JSON=file.json` skips the fetch and uses a
saved response instead, which also lets you compare two saved files offline.

**Saving the reference JSON from real Chrome**

1. Use a normal desktop Chrome, ideally the same major version as the tool's
   profile (`Chrome_152`, see `fetch_http.go`), with no proxy or VPN and no
   extensions that touch requests (a private window is fine).
2. Open `https://tls.peet.ws/api/all` in the address bar. It must be a page
   navigation, not `fetch()` from the DevTools console, because a `fetch()` call
   sends different headers (`sec-fetch-mode: cors`, no `upgrade-insecure-requests`).
3. Save it: press Ctrl+S (Cmd+S on macOS), choose "Webpage, HTML only" and name it
   `chrome-ref.json`. Alternatively select all, copy the JSON text and paste it into
   that file. The file must start with `{`.

**What is compared**

| Field | Compared | Why |
|-------|----------|-----|
| JA4 (full, and parts a/b/c separately) | MATCH/MISMATCH | JA4 sorts extensions, so it is stable across connections |
| HTTP/2 Akamai fingerprint (SETTINGS, WINDOW_UPDATE, priority, pseudo-header order) | MATCH/MISMATCH | |
| Pseudo-header order | MATCH/MISMATCH | |
| Header order, strict and common-headers-only | MATCH/MISMATCH | strict also flags extra/missing headers |
| JA3 | info only | Chrome randomizes extension order, so JA3 differs per connection |
| User-Agent | info only | differs legitimately when Chrome versions differ |

If your Chrome is newer than the tool's profile, a JA4 or HTTP/2 mismatch can just
reflect that version gap rather than a bug. Compare with a Chrome of the profile's
major version when you can.

## Verification status

### Verified

- **Sandbox (development environment):** build, unit/integration tests, redirects,
  cookies, header handling, JavaScript challenge solving with headless Chrome, and
  real fetches of a reachable HTTPS site in all three modes.
- **Real target, torob.com (2026-11-10):** `scripts/verify.sh` was run from a
  residential connection in Germany with no proxy (Go 1.27.0, Chrome 157), against
  a torob.com product page. All three modes succeeded:

  | mode | status | HTML size | challenge | result |
  |------|--------|-----------|-----------|--------|
  | `http`    | 200 | 174,298 bytes | no | OK |
  | `browser` | 200 | 358,613 bytes | no | OK |
  | `auto`    | 200 | 174,298 bytes | no | OK, succeeded in `http` mode (no fallback needed) |

  The final URL matched the requested URL and the `<title>` was the real product
  title ("خرید و قیمت دیسک بازی FC 26 برای پلی استیشن 5 | ترب"), not a block page.
  The saved HTML matched the default price/offer pattern (`"offers"`, `"price"`,
  toman/rial).

- **Fingerprint: verified for the Chrome 152 profile vs Chrome 157 on Windows,
  single run (2026-11-10).** `scripts/fingerprint.sh` was run with no proxy against
  `tls.peet.ws/api/all` and compared with a reference saved from a real Chrome 157
  on Windows (page navigation). The tool's profile is `Chrome_152`, the newest
  profile in tls-client v1.18.0.

  | Field | Result |
  |-------|--------|
  | JA4 (full and parts a/b/c): `t13d1517h2_8daaf6152771_cb7bf5808d99` | MATCH |
  | HTTP/2 Akamai fingerprint: `1:65536;2:0;4:6291456;6:262144\|15663105\|0\|m,a,s,p` | MATCH |
  | HTTP/2 pseudo-header order (`:method,:authority,:scheme,:path`) | MATCH |
  | Header order, common headers only | MATCH |
  | Header order, strict | MATCH after the `priority` fix (first run: MISMATCH, `priority` missing) |
  | JA3, User-Agent | differ as expected (extension shuffling; Chrome 152 vs 157) |

  The 152 vs 157 version gap did not change JA4 or the HTTP/2 fingerprint. The one
  mismatch was a real defect, not a version or OS effect: `priority` was in the
  header order list but never given a value, so it was never sent. It now sends
  `priority: u=0, i`, as Chrome does on navigations. A re-run after the fix exited 0
  with no MISMATCH (reported by the author; the log was not attached to the PR).

### Not independently verified

- **Fingerprint: limits of the verification above.**
  - One run against one reference (Chrome 157 on Windows); the OS the tool itself
    ran on was not recorded.
  - Other operating systems (macOS, Linux, Android) are untested.
  - Other Chrome versions are untested: the profile is pinned to `Chrome_152`, the
    newest in tls-client v1.18.0, so a Chrome that has moved on may diverge
    (JA4 and HTTP/2 settings do change between Chrome releases).
- **Price extraction is not checked.** Success means "real page, not a challenge,
  and price/offer-like content present". The tool does not parse or validate the
  actual price value.
- **Single run, single IP.** One run from one residential German IP says nothing
  about stability over time, rate limits, or other networks. Challenge behavior
  can change; `auto` mode exists for that reason.
- **Iranian IPs were not tested.**

### Known environment limitation

From the cloud development sandbox (a datacenter IP behind a TLS-intercepting egress
proxy), `torob.com` resets the connection during the TLS handshake. The German
residential run above shows the site is not blocked for non-Iranian IPs in general,
so that failure is specific to the sandbox's IP or network (most likely
datacenter-IP reputation). It is not a defect in this tool. Run
`scripts/verify.sh` from a residential or otherwise accepted network.
