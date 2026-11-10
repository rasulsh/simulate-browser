// simulate-browser fetches a page the way a real Chrome would: either with a
// Chrome-impersonating HTTP client or with real headless Chrome (CDP).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"time"
)

type fetcher func(ctx context.Context, url, lang, proxy string, timeout time.Duration) (*Result, error)

type config struct {
	url, mode, out, lang, proxy string
	expect                      *regexp.Regexp
	timeout                     time.Duration
}

func main() {
	var c config
	var expect string
	flag.StringVar(&c.url, "url", "", "page URL to fetch (required)")
	flag.StringVar(&c.mode, "mode", "auto", "fetch strategy: auto|http|browser")
	flag.StringVar(&c.out, "out", "page.html", "file to save the HTML to")
	flag.StringVar(&c.lang, "lang", "fa-IR,fa;q=0.9,en-US;q=0.8,en;q=0.7", "Accept-Language value")
	flag.StringVar(&c.proxy, "proxy", os.Getenv("HTTPS_PROXY"), "proxy URL (default: $HTTPS_PROXY)")
	flag.StringVar(&expect, "expect", defaultExpect, "regexp the HTML must match to count as real content")
	flag.DurationVar(&c.timeout, "timeout", 60*time.Second, "per-strategy timeout")
	flag.Parse()

	if c.url == "" || (c.mode != "auto" && c.mode != "http" && c.mode != "browser") {
		flag.Usage()
		os.Exit(2)
	}
	re, err := regexp.Compile(expect)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invalid -expect:", err)
		os.Exit(2)
	}
	c.expect = re

	if !run(context.Background(), c, fetchHTTP, fetchBrowser) {
		os.Exit(1)
	}
}

// run executes the selected mode(s), saves the HTML and prints evidence.
// It reports whether the saved page is real content (not a challenge/block page).
func run(ctx context.Context, c config, httpF, browserF fetcher) bool {
	var steps []struct {
		name string
		f    fetcher
	}
	add := func(n string, f fetcher) {
		steps = append(steps, struct {
			name string
			f    fetcher
		}{n, f})
	}
	switch c.mode {
	case "http":
		add("http", httpF)
	case "browser":
		add("browser", browserF)
	default:
		add("http", httpF)
		add("browser", browserF)
	}

	var last *Result
	for i, s := range steps {
		res, err := s.f(ctx, c.url, c.lang, c.proxy, c.timeout)
		if err != nil {
			fmt.Printf("[%s] error: %v\n", s.name, err)
			continue
		}
		bad, why := isChallenge(res.Status, res.Body)
		ok := !bad && c.expect.MatchString(res.Body)
		fmt.Printf("[%s] status=%d size=%d challenge=%v ok=%v\n", s.name, res.Status, len(res.Body), bad, ok)
		last = res
		if ok {
			report(c, res, true, "")
			return true
		}
		reason := failReason(res, bad, why)
		if i < len(steps)-1 {
			fmt.Printf("[%s] not usable (%s); falling back\n", s.name, reason)
		}
	}
	if last == nil {
		fmt.Println("RESULT: FAIL - no strategy returned a response")
		return false
	}
	bad, why := isChallenge(last.Status, last.Body)
	report(c, last, false, failReason(last, bad, why))
	return false
}

func report(c config, r *Result, ok bool, why string) {
	if err := os.WriteFile(c.out, []byte(r.Body), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write output:", err)
	}
	bad, _ := isChallenge(r.Status, r.Body)
	fmt.Println("---- evidence ----")
	fmt.Printf("mode:       %s\n", r.Mode)
	fmt.Printf("status:     %d\n", r.Status)
	fmt.Printf("final URL:  %s\n", r.FinalURL)
	fmt.Printf("HTML size:  %d bytes\n", len(r.Body))
	fmt.Printf("title:      %s\n", pageTitle(r.Body))
	fmt.Printf("challenge:  %v\n", bad)
	fmt.Printf("saved to:   %s\n", c.out)
	if ok {
		fmt.Printf("RESULT: OK - real content found (mode %s)\n", r.Mode)
	} else {
		fmt.Printf("RESULT: FAIL - %s (last mode %s)\n", why, r.Mode)
	}
}

// failReason explains why a response was rejected.
func failReason(r *Result, challenge bool, why string) string {
	switch {
	case challenge:
		return "challenge/block page (" + why + ")"
	case r.Status >= 400:
		msg := fmt.Sprintf("HTTP status %d", r.Status)
		if len(r.Body) > 0 && len(r.Body) < 200 {
			msg += fmt.Sprintf(", body %q", r.Body)
		}
		return msg
	default:
		return "expected content not found"
	}
}
