package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

const productHTML = `<html><head><title>Test Phone</title></head><body>` +
	`<script type="application/ld+json">{"@type":"Product","offers":{"price":"100"}}</script></body></html>`

func testConfig(t *testing.T, url, mode string) config {
	return config{url: url, mode: mode, out: filepath.Join(t.TempDir(), "o.html"), lang: "en",
		expect: regexp.MustCompile(defaultExpect), timeout: 30 * time.Second}
}

func fake(res *Result, err error) fetcher {
	return func(context.Context, string, string, string, time.Duration) (*Result, error) { return res, err }
}

func TestAutoFallsBackToBrowser(t *testing.T) {
	blocked := &Result{Mode: "http", Status: 403, Body: "<title>Access Denied</title>"}
	good := &Result{Mode: "browser", Status: 200, FinalURL: "u", Body: productHTML}
	c := testConfig(t, "u", "auto")
	if !run(context.Background(), c, fake(blocked, nil), fake(good, nil)) {
		t.Fatal("expected success via browser fallback")
	}
	if b, _ := os.ReadFile(c.out); !strings.Contains(string(b), "Test Phone") {
		t.Fatal("saved HTML missing product title")
	}
}

func TestAutoStopsAtHTTPWhenGood(t *testing.T) {
	good := &Result{Mode: "http", Status: 200, Body: productHTML}
	boom := func(context.Context, string, string, string, time.Duration) (*Result, error) {
		t.Fatal("browser must not run when http succeeded")
		return nil, nil
	}
	if !run(context.Background(), testConfig(t, "u", "auto"), fake(good, nil), boom) {
		t.Fatal("expected success")
	}
}

func TestAutoFailsWhenBothBlocked(t *testing.T) {
	blocked := &Result{Status: 403, Body: "x"}
	if run(context.Background(), testConfig(t, "u", "auto"), fake(blocked, nil), fake(nil, errors.New("nope"))) {
		t.Fatal("must not report success")
	}
}

func TestMissingExpectedContentIsFailure(t *testing.T) {
	plain := &Result{Mode: "http", Status: 200, Body: "<title>Hello</title>"}
	if run(context.Background(), testConfig(t, "u", "http"), fake(plain, nil), nil) {
		t.Fatal("page without price data must not count as success")
	}
}

// testServer exercises redirects and cookies: /start sets a cookie and redirects
// to /product, which only serves content when the cookie came back.
func testServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "1", Path: "/"})
		http.Redirect(w, r, "/product", http.StatusFound)
	})
	mux.HandleFunc("/product", func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("sid"); err != nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if !strings.Contains(r.UserAgent(), "Chrome/") || r.Header.Get("Sec-Fetch-Mode") != "navigate" ||
			r.Header.Get("Priority") != "u=0, i" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(productHTML))
	})
	mux.HandleFunc("/blocked", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	// JS challenge: initial page is a challenge; JS swaps in the real content.
	mux.HandleFunc("/js", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><head><title>Just a moment...</title></head><body>checking your browser` +
			`<script>setTimeout(function(){document.title="JS Phone";` +
			`document.body.innerHTML='<script type="application/ld+json">{"offers":{"price":"5"}}<\/script>ok'},1500)</script></body></html>`))
	})
	return httptest.NewServer(mux)
}

func TestHTTPModeRedirectCookiesHeaders(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	res, err := fetchHTTP(context.Background(), srv.URL+"/start", "en", "", 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 200 || !strings.HasSuffix(res.FinalURL, "/product") || !strings.Contains(res.Body, "Test Phone") {
		t.Fatalf("unexpected result: status=%d final=%s", res.Status, res.FinalURL)
	}
}

func TestHTTPModeReports403(t *testing.T) {
	srv := testServer()
	defer srv.Close()
	res, err := fetchHTTP(context.Background(), srv.URL+"/blocked", "en", "", 20*time.Second)
	if err != nil || res.Status != 403 {
		t.Fatalf("err=%v res=%+v", err, res)
	}
}

func TestBrowserModeRunsJSChallenge(t *testing.T) {
	if _, err := findChrome(); err != nil {
		t.Skip(err)
	}
	srv := testServer()
	defer srv.Close()
	res, err := fetchBrowser(context.Background(), srv.URL+"/js", "en", "", 40*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 200 || pageTitle(res.Body) != "JS Phone" || !strings.Contains(res.Body, `"price"`) {
		t.Fatalf("browser did not render real content: status=%d title=%q", res.Status, pageTitle(res.Body))
	}
}

// Auto mode end to end: the plain-HTTP path is blocked, the browser path passes.
func TestAutoEndToEndFallback(t *testing.T) {
	if _, err := findChrome(); err != nil {
		t.Skip(err)
	}
	srv := testServer()
	defer srv.Close()
	// /js serves a challenge title to the HTTP client (no JS), so auto must fall back.
	c := testConfig(t, srv.URL+"/js", "auto")
	if !run(context.Background(), c, fetchHTTP, fetchBrowser) {
		t.Fatal("auto mode should succeed through the browser")
	}
}
