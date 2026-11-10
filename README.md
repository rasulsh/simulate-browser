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

### Not independently verified

- **The TLS / JA3 / JA4 / HTTP/2 fingerprint layer is still not validated in
  isolation.** The `http` mode getting through unproxied is good indirect
  evidence that the fingerprint is accepted, but the exact fingerprint was never
  compared against a reference (for example a fingerprint echo service against a
  real Chrome capture).
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
