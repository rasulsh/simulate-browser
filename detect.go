package main

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

// defaultExpect matches price/offer data in a product page: structured data
// ("offers"/"price") or Persian currency words (toman/rial).
const defaultExpect = `(?i)"offers"|"price"|"lowprice"|تومان|ریال`

var titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// pageTitle returns the trimmed, unescaped <title> text, or "".
func pageTitle(body string) string {
	m := titleRe.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return strings.Join(strings.Fields(html.UnescapeString(m[1])), " ")
}

// Markers that identify anti-bot challenge or block pages.
var challengeMarkers = []string{
	"just a moment", "cf-chl", "challenge-platform", "attention required",
	"access denied", "_incapsula_", "checking your browser", "verify you are human",
	"are you a robot", "captcha", "arvancloud", "request blocked",
	"enable javascript and cookies", "ddos protection",
}

// isChallenge reports whether a response looks like a challenge/block page
// instead of real content, with a short reason when it does.
func isChallenge(status int, body string) (bool, string) {
	switch status {
	case 403, 429, 503:
		return true, fmt.Sprintf("HTTP status %d", status)
	}
	low := strings.ToLower(body)
	title := strings.ToLower(pageTitle(body))
	for _, m := range challengeMarkers {
		// A marker in <title> is decisive; elsewhere it only counts in small pages,
		// because large real pages may mention these words in scripts.
		if strings.Contains(title, m) || (len(body) < 20000 && strings.Contains(low, m)) {
			return true, fmt.Sprintf("marker %q", m)
		}
	}
	return false, ""
}
