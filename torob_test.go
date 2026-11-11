package main

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/bogdanfinn/tls-client/profiles"
)

var now = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Both fixtures embed only the first few sellers; the full list is in the API.
func TestFixturesEmbeddedOnly(t *testing.T) {
	for _, f := range []string{"torob_product_http.html", "torob_product_browser.html"} {
		out, err := buildOutput("u", "http", readFixture(t, f), nil, errors.New("offline"), now)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if out.SellerCount != 6 || out.Complete {
			t.Errorf("%s: want 6 sellers and complete=false, got %d complete=%v", f, out.SellerCount, out.Complete)
		}
		if out.ExpectedCount == nil || *out.ExpectedCount != 146 {
			t.Errorf("%s: expected_count = %v, want 146", f, out.ExpectedCount)
		}
		if out.Product.ID != "204280a0-fe34-419b-9b6c-9b2ecf8dfd1c" || out.Product.Title == "" {
			t.Errorf("%s: product = %+v", f, out.Product)
		}
	}
}

func TestFixturesWithAPI(t *testing.T) {
	api := []byte(readFixture(t, "torob_sellers_api.json"))
	for _, f := range []string{"torob_product_http.html", "torob_product_browser.html"} {
		out, err := buildOutput("u", "http", readFixture(t, f), api, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		// 146 API rows; one is a promoted repeat of another offer (same prk).
		if out.SellerCount != 145 || out.ExpectedCount == nil || *out.ExpectedCount != 146 || !out.Complete {
			t.Fatalf("%s: count=%d expected=%v complete=%v warnings=%v", f, out.SellerCount, out.ExpectedCount, out.Complete, out.Warnings)
		}
		if out.SellerCount <= 6 {
			t.Fatal("must exceed the initially embedded sellers")
		}
		for i, s := range out.Sellers {
			if s.Name == "" {
				t.Errorf("seller %d has no name", i)
			}
			if s.Available && s.Price == nil {
				t.Errorf("seller %d (%s) available but price missing: %q", i, s.Name, s.PriceText)
			}
			if !s.Available && s.Price != nil {
				t.Errorf("seller %d unavailable but has price", i)
			}
		}
		first, last := out.Sellers[0], out.Sellers[len(out.Sellers)-1]
		if first.Name != "همراه رایانه بهسان" || first.Price == nil || *first.Price != 3590000 || first.PriceText != "۳٫۵۹۰٫۰۰۰ تومان" {
			t.Errorf("first seller = %+v", first)
		}
		if last.Name != "گوشی شاپ" || last.Available || last.Price != nil || last.PriceText != "ناموجود" {
			t.Errorf("last seller = %+v", last)
		}
		if out.Currency != "toman" || out.Warnings == nil {
			t.Errorf("currency=%q warnings=%v", out.Currency, out.Warnings)
		}
		if _, err := json.Marshal(out); err != nil {
			t.Fatal(err)
		}
	}
}

func TestParseToman(t *testing.T) {
	cases := map[string]struct {
		n  int64
		ok bool
	}{
		"۳٫۵۹۰٫۰۰۰ تومان":    {3590000, true},
		"از ۳٫۱۹۷٫۰۰۰ تومان": {3197000, true},
		"3,590,000 تومان":    {3590000, true},
		"٣٬٥٩٠٬٠٠٠":          {3590000, true}, // Arabic-Indic digits and separator
		"ناموجود":            {0, false},
		"تماس بگیرید":        {0, false},
		"":                   {0, false},
		"ناموجود (۵٫۴۰۰٫۰۰۰ تومان) ۱۲۳": {0, false}, // two numbers: ambiguous
	}
	for in, want := range cases {
		n, ok := parseToman(in)
		if n != want.n || ok != want.ok {
			t.Errorf("parseToman(%q) = %d,%v want %d,%v", in, n, ok, want.n, want.ok)
		}
	}
}

func TestToSellerMissingFields(t *testing.T) {
	// Nothing but a name: kept, with nulls.
	s := toSeller(rawSeller{Name: "Shop"})
	if s.Name != "Shop" || s.Price != nil || s.City != nil || s.URL != nil || s.Available {
		t.Errorf("%+v", s)
	}
	// No numeric price, no availability flag: price comes from Persian text.
	s = toSeller(rawSeller{Name: "A", PriceText: "۱۲٫۵۰۰ تومان"})
	if s.Price == nil || *s.Price != 12500 || !s.Available {
		t.Errorf("%+v", s)
	}
	// Available but price text unparseable: kept, price null, raw text kept.
	yes := true
	s = toSeller(rawSeller{Name: "B", Availability: &yes, PriceText: "تماس بگیرید"})
	if s.Price != nil || s.PriceText != "تماس بگیرید" || !s.Available {
		t.Errorf("%+v", s)
	}
	// Unavailable with a leftover price: price null.
	no := false
	s = toSeller(rawSeller{Name: "C", Availability: &no, Price: json.RawMessage("5400000"), PriceText: "ناموجود (۵٫۴۰۰٫۰۰۰ تومان)"})
	if s.Price != nil || s.Available {
		t.Errorf("%+v", s)
	}
}

func TestBuildSellersDuplicates(t *testing.T) {
	yes := true
	raw := []rawSeller{
		{Name: "A", Prk: "1", Availability: &yes, Price: json.RawMessage("100")},
		{Name: "A", Prk: "1", Availability: &yes, Price: json.RawMessage("100")}, // same offer
		{Name: "A", Prk: "2", Availability: &yes, Price: json.RawMessage("120")}, // distinct offer, same shop
		{Name: "B"}, // no prk: never collapsed
		{Name: "B"},
	}
	got, dups := buildSellers(raw)
	if len(got) != 4 || dups != 1 {
		t.Fatalf("len=%d dups=%d", len(got), dups)
	}
}

func page(json string) string {
	return `<html><script id="__NEXT_DATA__" type="application/json">` + json + `</script></html>`
}

func TestBuildOutputMalformed(t *testing.T) {
	for name, p := range map[string]string{
		"no script":    "<html></html>",
		"bad json":     page(`{"props":`),
		"empty object": page(`{}`),
	} {
		out, err := buildOutput("u", "http", p, nil, nil, now)
		if name != "empty object" {
			if err == nil {
				t.Errorf("%s: want error", name)
			}
			continue
		}
		if err != nil || out.SellerCount != 0 || out.Sellers == nil {
			t.Errorf("%s: %v %+v", name, err, out)
		}
	}
	// Malformed API body falls back to embedded sellers with a warning.
	out, err := buildOutput("u", "http", readFixture(t, "torob_product_http.html"), []byte("not json"), nil, now)
	if err != nil || out.SellerCount != 6 || out.Complete || len(out.Warnings) == 0 {
		t.Errorf("malformed API: %v %+v", err, out)
	}
}

func TestIncompleteWhenCountDiffers(t *testing.T) {
	api := `{"count":3,"results":[{"shop_name":"A","prk":"1","price":10,"availability":true}]}`
	out, _ := buildOutput("u", "http", page(`{}`), []byte(api), nil, now)
	if out.Complete || out.SellerCount != 1 || *out.ExpectedCount != 3 ||
		!strings.Contains(strings.Join(out.Warnings, "|"), "differs") {
		t.Errorf("%+v", out)
	}
}

func TestValidSellersURL(t *testing.T) {
	if err := validSellersURL("https://api.torob.com/v4/base-product/sellers/?x=1"); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{"http://api.torob.com/x", "https://evil.example/x", "https://api.torob.com.evil.example/x", "//api.torob.com/x", ""} {
		if validSellersURL(bad) == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

// The impersonated Chrome is defined in one block; its parts must agree.
func TestChromeProfileConsistent(t *testing.T) {
	uaMajor := regexp.MustCompile(`Chrome/(\d+)\.`).FindStringSubmatch(chromeUA)
	if uaMajor == nil {
		t.Fatalf("no Chrome major in UA %q", chromeUA)
	}
	brands := regexp.MustCompile(`"(?:Chromium|Google Chrome)";v="(\d+)"`).FindAllStringSubmatch(chromeSecChUA, -1)
	if len(brands) != 2 {
		t.Fatalf("want Chromium and Google Chrome brands in %q", chromeSecChUA)
	}
	for _, b := range brands {
		if b[1] != uaMajor[1] {
			t.Errorf("sec-ch-ua major %s != User-Agent major %s", b[1], uaMajor[1])
		}
	}
	if uaMajor[1] != chromeMajor {
		t.Errorf("UA major %s != chromeMajor %s", uaMajor[1], chromeMajor)
	}
	if p, ok := profiles.MappedTLSClients["chrome_"+chromeMajor]; !ok || p.GetClientHelloStr() != chromeProfile.GetClientHelloStr() {
		t.Errorf("chromeProfile is not the tls-client profile chrome_%s", chromeMajor)
	}
}
