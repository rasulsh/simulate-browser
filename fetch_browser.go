package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// findChrome returns the Chrome/Chromium binary to use.
func findChrome() (string, error) {
	if p := os.Getenv("CHROME_PATH"); p != "" {
		return p, nil
	}
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	return "", errors.New("no Chrome/Chromium binary found (set CHROME_PATH)")
}

// fetchBrowser loads url in headless Chrome over CDP so JavaScript challenges
// run and JS-rendered content appears. It polls until the page stops looking
// like a challenge (or the timeout expires) and returns the rendered DOM.
func fetchBrowser(ctx context.Context, url, lang, proxy string, timeout time.Duration) (*Result, error) {
	bin, err := findChrome()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	flags := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(bin),
		chromedp.Flag("headless", "new"),
		chromedp.Flag("no-sandbox", true), // required when running as root in containers
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("lang", lang),
		chromedp.UserAgent(chromeUA), // drop the "HeadlessChrome" token
	)
	if proxy != "" {
		flags = append(flags, chromedp.ProxyServer(proxy))
	}
	actx, acancel := chromedp.NewExecAllocator(ctx, flags...)
	defer acancel()
	bctx, bcancel := chromedp.NewContext(actx)
	defer bcancel()

	// Track the main document response for status code and final URL.
	var (
		mu       sync.Mutex
		status   int
		finalURL = url
	)
	chromedp.ListenTarget(bctx, func(ev any) {
		if e, ok := ev.(*network.EventResponseReceived); ok && e.Type == network.ResourceTypeDocument {
			mu.Lock()
			status, finalURL = int(e.Response.Status), e.Response.URL
			mu.Unlock()
		}
	})
	snapshot := func(dom string) *Result {
		mu.Lock()
		defer mu.Unlock()
		return &Result{Mode: "browser", Status: status, FinalURL: finalURL, Body: dom}
	}

	if err := chromedp.Run(bctx, network.Enable(), chromedp.Navigate(url)); err != nil {
		return nil, err
	}

	var dom string
	for {
		if err := chromedp.Run(bctx, chromedp.OuterHTML("html", &dom, chromedp.ByQuery)); err != nil {
			return nil, err
		}
		mu.Lock()
		st := status
		mu.Unlock()
		if bad, _ := isChallenge(st, dom); !bad {
			break
		}
		select {
		case <-bctx.Done():
			// Timed out while still on a challenge page: return what we have.
			return snapshot(dom), nil
		case <-time.After(1500 * time.Millisecond):
		}
	}
	// Give late JS rendering (prices, offers) a moment to settle, then re-read.
	time.Sleep(1500 * time.Millisecond)
	_ = chromedp.Run(bctx, chromedp.OuterHTML("html", &dom, chromedp.ByQuery))
	return snapshot(dom), nil
}
