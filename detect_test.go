package main

import "testing"

func TestPageTitle(t *testing.T) {
	got := pageTitle("<html><head><title>\n  Foo &amp; Bar  </title></head>")
	if got != "Foo & Bar" {
		t.Fatalf("got %q", got)
	}
	if pageTitle("<html></html>") != "" {
		t.Fatal("expected empty title")
	}
}

func TestIsChallenge(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{"ok page", 200, "<title>Phone X</title><script>{\"price\":1}</script>", false},
		{"403", 403, "<title>Phone</title>", true},
		{"429", 429, "", true},
		{"503", 503, "", true},
		{"cloudflare title", 200, "<title>Just a moment...</title>", true},
		{"small captcha page", 200, "<body>Please solve the CAPTCHA</body>", true},
		{"large page mentioning captcha in script", 200, "<title>Shop</title>" + string(make([]byte, 30000)) + "captcha", false},
	}
	for _, c := range cases {
		if got, _ := isChallenge(c.status, c.body); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
