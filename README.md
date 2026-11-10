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

- **Verified in the development sandbox:** build, unit/integration tests,
  redirects, cookies, header handling, JS challenge solving with headless Chrome,
  and real fetches of a reachable HTTPS site in all three modes.
- **TLS / JA3 / JA4 / HTTP/2 fingerprint layer: implemented but unverified.**
  The sandbox re-terminates TLS in an egress proxy (certificates are issued by
  `CCR agent-proxy interception CA`), so the target never sees this tool's
  ClientHello and the fingerprint cannot be validated there. Check it against a
  fingerprint echo service from a direct connection.
- **torob.com: NOT verified.** From the sandbox, `torob.com` is allowed by the
  network policy but the connection is reset during the TLS handshake
  (`502 upstream request failed` through the proxy), consistent with IP/geo
  blocking of datacenter or non-Iranian addresses. This is an environment
  limitation, not a result. Run `scripts/verify.sh` from an accepted network.
