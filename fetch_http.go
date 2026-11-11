package main

import (
	"context"
	"fmt"
	"io"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

// Result is the outcome of one fetch strategy.
type Result struct {
	Mode     string
	Status   int
	FinalURL string
	Body     string
	client   tls.HttpClient // http mode only: reused (same cookies) for follow-up API calls
}

// ---- Chrome profile: the ONLY place that defines the impersonated Chrome. ----
// To move to another Chrome version, change this block (and bump tls-client if
// it ships a newer profile). TestChromeProfileConsistent fails if the pieces
// disagree. See README, "Updating the Chrome profile".
const chromeMajor = "152"

var chromeProfile = profiles.Chrome_152

const chromeUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" +
	chromeMajor + ".0.0.0 Safari/537.36"

const chromeSecChUA = `"Chromium";v="` + chromeMajor + `", "Google Chrome";v="` + chromeMajor + `", "Not/A)Brand";v="24"`

// ---- end of Chrome profile block ----

// newHTTPClient builds the Chrome-impersonating client: Chrome TLS
// ClientHello, HTTP/2 settings, cookie jar, redirect following.
func newHTTPClient(proxy string, timeout time.Duration) (tls.HttpClient, error) {
	opts := []tls.HttpClientOption{
		tls.WithTimeoutSeconds(int(timeout.Seconds())),
		tls.WithClientProfile(chromeProfile),
		tls.WithCookieJar(tls.NewCookieJar()),
		tls.WithRandomTLSExtensionOrder(), // Chrome shuffles extensions per connection
	}
	if proxy != "" {
		opts = append(opts, tls.WithProxyUrl(proxy))
	}
	return tls.NewHttpClient(tls.NewNoopLogger(), opts...)
}

// fetchHTTP downloads url with a Chrome-impersonating client: Chrome TLS
// ClientHello, Chrome HTTP/2 SETTINGS/pseudo-header order and Chrome header
// order, with a cookie jar and redirect following.
func fetchHTTP(ctx context.Context, url, lang, proxy string, timeout time.Duration) (*Result, error) {
	client, err := newHTTPClient(proxy, timeout)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{
		"sec-ch-ua":                 {chromeSecChUA},
		"sec-ch-ua-mobile":          {"?0"},
		"sec-ch-ua-platform":        {`"Windows"`},
		"upgrade-insecure-requests": {"1"},
		"user-agent":                {chromeUA},
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"},
		"sec-fetch-site":            {"none"},
		"sec-fetch-mode":            {"navigate"},
		"sec-fetch-user":            {"?1"},
		"sec-fetch-dest":            {"document"},
		"accept-encoding":           {"gzip, deflate, br, zstd"},
		"accept-language":           {lang},
		"priority":                  {"u=0, i"}, // Chrome sends this on top-level navigations
		http.HeaderOrderKey: {
			"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform", "upgrade-insecure-requests",
			"user-agent", "accept", "sec-fetch-site", "sec-fetch-mode", "sec-fetch-user",
			"sec-fetch-dest", "accept-encoding", "accept-language", "cookie", "priority",
		},
		http.PHeaderOrderKey: {":method", ":authority", ":scheme", ":path"},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	final := url
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	return &Result{Mode: "http", Status: resp.StatusCode, FinalURL: final, Body: string(body), client: client}, nil
}
