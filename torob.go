package main

// Torob seller extraction.
//
// How Torob delivers sellers (verified against testdata/):
//   - The product page embeds a __NEXT_DATA__ JSON script. Its
//     props.pageProps.baseProduct.products_info holds only the first few
//     sellers ("result", 6 of 146 in the samples), the total ("count") and
//     "sellers_url", the API the page itself calls for the full list.
//   - GET sellers_url returns {"count": N, "results": [...]} with ALL sellers
//     in one response (no pagination; there is no "show more" request).
//   - Prices are integers in Toman (price_text ends in "تومان").
//   - The first API row can be a promoted duplicate (is_adv) of an organic
//     row: same "prk" (offer id). Duplicates by prk are collapsed.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
)

// torobExpect is the "real content" check used in extraction mode.
const torobExpect = `id="__NEXT_DATA__"`

const (
	torobOrigin  = "https://torob.com"
	torobAPIHost = "api.torob.com"
)

// Output is the JSON document written by -extract-torob-sellers.
//
//	currency:       "toman" (Torob shows prices in Toman).
//	expected_count: seller total reported by Torob (products_info.count), or null.
//	complete:       true when every seller Torob reports was extracted.
//	sellers[].price: integer Toman, or null (out of stock / unparseable);
//	                 price_text is always the raw text shown by Torob.
type Output struct {
	URL           string   `json:"url"`
	FetchedAt     string   `json:"fetched_at"`
	Mode          string   `json:"mode"`
	Product       Product  `json:"product"`
	Currency      string   `json:"currency"`
	SellerCount   int      `json:"seller_count"`
	ExpectedCount *int     `json:"expected_count"`
	Complete      bool     `json:"complete"`
	Sellers       []Seller `json:"sellers"`
	Warnings      []string `json:"warnings"`
}

type Product struct {
	Title string `json:"title"`
	ID    string `json:"id"`
}

type Seller struct {
	Name      string  `json:"name"`
	Price     *int64  `json:"price"`
	PriceText string  `json:"price_text"`
	Available bool    `json:"available"`
	City      *string `json:"city"`
	URL       *string `json:"url"`
}

// rawSeller mirrors the fields we use from both the embedded JSON and the API.
// Pointers distinguish "missing" from zero values.
type rawSeller struct {
	Name         string          `json:"shop_name"`
	City         string          `json:"shop_name2"`
	Prk          string          `json:"prk"`
	Availability *bool           `json:"availability"`
	Price        json.RawMessage `json:"price"`
	PriceText    string          `json:"price_text"`
	PageURL      string          `json:"page_url"`
}

type productsInfo struct {
	Result     []rawSeller `json:"result"`
	Count      *int        `json:"count"`
	SellersURL string      `json:"sellers_url"`
}

type nextData struct {
	Props struct {
		PageProps struct {
			BaseProduct struct {
				RandomKey    string       `json:"random_key"`
				Name1        string       `json:"name1"`
				ProductsInfo productsInfo `json:"products_info"`
			} `json:"baseProduct"`
		} `json:"pageProps"`
	} `json:"props"`
}

var nextDataRe = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__"[^>]*>(.*?)</script>`)

func parseNextData(page string) (*nextData, error) {
	m := nextDataRe.FindStringSubmatch(page)
	if m == nil {
		return nil, fmt.Errorf("__NEXT_DATA__ script not found")
	}
	var d nextData
	if err := json.Unmarshal([]byte(m[1]), &d); err != nil {
		return nil, fmt.Errorf("parse __NEXT_DATA__: %w", err)
	}
	return &d, nil
}

// parseSellersAPI parses the sellers API body: {"count": N, "results": [...]}.
func parseSellersAPI(body []byte) ([]rawSeller, *int, error) {
	var r struct {
		Count   *int        `json:"count"`
		Results []rawSeller `json:"results"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, nil, fmt.Errorf("parse sellers API response: %w", err)
	}
	return r.Results, r.Count, nil
}

var digitFolder = strings.NewReplacer(
	"۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4", "۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9",
	"٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4", "٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9",
	"٫", "", "٬", "", ",", "", "،", "", ".", "", "‌", "", " ", "",
)

var digitRun = regexp.MustCompile(`\d+`)

// parseToman converts text such as "۳٫۵۹۰٫۰۰۰ تومان" to 3590000. Persian and
// Arabic-Indic digits are converted and thousands separators dropped. It
// returns false unless the text holds exactly one number.
func parseToman(text string) (int64, bool) {
	runs := digitRun.FindAllString(digitFolder.Replace(text), -1)
	if len(runs) != 1 {
		return 0, false
	}
	n, err := strconv.ParseInt(runs[0], 10, 64)
	return n, err == nil
}

// toSeller converts one raw offer without ever dropping it for missing fields.
func toSeller(r rawSeller) Seller {
	s := Seller{Name: strings.TrimSpace(r.Name), PriceText: r.PriceText}
	if r.City != "" {
		c := r.City
		s.City = &c
	}
	if r.PageURL != "" {
		u := r.PageURL
		s.URL = &u
	}
	var price *int64
	if len(r.Price) > 0 {
		var n json.Number
		if json.Unmarshal(r.Price, &n) == nil {
			if v, err := n.Int64(); err == nil && v > 0 {
				price = &v
			}
		}
	}
	if price == nil && (r.Availability == nil || *r.Availability) {
		if v, ok := parseToman(r.PriceText); ok && v > 0 {
			price = &v
		}
	}
	// Availability: explicit flag wins; otherwise "has a price" means available.
	if r.Availability != nil {
		s.Available = *r.Availability
	} else {
		s.Available = price != nil
	}
	if !s.Available {
		price = nil // "ناموجود (۵٫۴۰۰٫۰۰۰ تومان)" is a last price, not an offer
	}
	s.Price = price
	return s
}

// buildSellers converts raw offers, collapsing repeats of the same offer (same
// prk, e.g. a promoted slot repeating an organic row). It returns how many
// repeats were dropped.
func buildSellers(raw []rawSeller) ([]Seller, int) {
	seen := map[string]bool{}
	out := make([]Seller, 0, len(raw))
	dups := 0
	for _, r := range raw {
		if r.Prk != "" {
			if seen[r.Prk] {
				dups++
				continue
			}
			seen[r.Prk] = true
		}
		out = append(out, toSeller(r))
	}
	return out, dups
}

// validSellersURL only lets us follow the API URL the page names if it is the
// Torob API over https, so a tampered page cannot steer the client elsewhere.
func validSellersURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() != torobAPIHost {
		return fmt.Errorf("refusing sellers_url %q (want https://%s/...)", raw, torobAPIHost)
	}
	return nil
}

// buildOutput assembles the JSON document from the page HTML and, when
// available, the sellers API body (apiBody) or the error that prevented it.
func buildOutput(pageURL, mode, page string, apiBody []byte, apiErr error, now time.Time) (*Output, error) {
	nd, err := parseNextData(page)
	if err != nil {
		return nil, err
	}
	bp := nd.Props.PageProps.BaseProduct
	out := &Output{
		URL:       pageURL,
		FetchedAt: now.UTC().Format(time.RFC3339),
		Mode:      mode,
		Product:   Product{Title: bp.Name1, ID: bp.RandomKey},
		Currency:  "toman",
		Warnings:  []string{},
	}
	raw, expected := bp.ProductsInfo.Result, bp.ProductsInfo.Count
	switch {
	case apiErr != nil:
		out.Warnings = append(out.Warnings, "sellers API not used, only the sellers embedded in the page: "+apiErr.Error())
	case apiBody != nil:
		r, c, err := parseSellersAPI(apiBody)
		if err != nil {
			out.Warnings = append(out.Warnings, err.Error()+"; using only the sellers embedded in the page")
		} else {
			raw = r
			if c != nil {
				expected = c
			}
		}
	}
	var dups int
	out.Sellers, dups = buildSellers(raw)
	out.SellerCount = len(out.Sellers)
	out.ExpectedCount = expected
	if dups > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("collapsed %d repeated offer(s) (same offer id, e.g. promoted slot)", dups))
	}
	noName, noPrice := 0, 0
	for _, s := range out.Sellers {
		if s.Name == "" {
			noName++
		}
		if s.Available && s.Price == nil {
			noPrice++
		}
	}
	if noName > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("%d seller(s) without a name", noName))
	}
	if noPrice > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("%d available seller(s) with an unparseable price (see price_text)", noPrice))
	}
	switch {
	case expected == nil:
		out.Complete = apiErr == nil && apiBody != nil
		out.Warnings = append(out.Warnings, "page reports no seller total; cannot verify completeness")
	case out.SellerCount+dups == *expected:
		out.Complete = true
	default:
		out.Warnings = append(out.Warnings, fmt.Sprintf("seller_count %d differs from expected_count %d", out.SellerCount, *expected))
	}
	return out, nil
}

// fetchSellersAPI GETs the sellers API the way Chrome's page script does: same
// client (cookies), same-site CORS fetch headers and Referer https://torob.com/.
func fetchSellersAPI(ctx context.Context, res *Result, apiURL, lang, proxy string, timeout time.Duration) ([]byte, error) {
	if err := validSellersURL(apiURL); err != nil {
		return nil, err
	}
	client := res.client
	if client == nil { // browser-mode page: no http client yet
		var err error
		if client, err = newHTTPClient(proxy, timeout); err != nil {
			return nil, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{
		"accept":             {"*/*"},
		"accept-encoding":    {"gzip, deflate, br, zstd"},
		"accept-language":    {lang},
		"cache-control":      {"no-cache"},
		"origin":             {torobOrigin},
		"pragma":             {"no-cache"},
		"priority":           {"u=1, i"},
		"referer":            {torobOrigin + "/"},
		"sec-ch-ua":          {chromeSecChUA},
		"sec-ch-ua-mobile":   {"?0"},
		"sec-ch-ua-platform": {`"Windows"`},
		"sec-fetch-dest":     {"empty"},
		"sec-fetch-mode":     {"cors"},
		"sec-fetch-site":     {"same-site"},
		"user-agent":         {chromeUA},
		http.HeaderOrderKey: {
			"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform", "user-agent", "accept",
			"origin", "sec-fetch-site", "sec-fetch-mode", "sec-fetch-dest", "referer",
			"accept-encoding", "accept-language", "cookie", "priority", "cache-control", "pragma",
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
		return nil, fmt.Errorf("read sellers API body: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("sellers API returned HTTP %d", resp.StatusCode)
	}
	if bad, why := isChallenge(resp.StatusCode, string(body)); bad {
		return nil, fmt.Errorf("sellers API returned a challenge page (%s)", why)
	}
	return body, nil
}

// runExtract fetches the product page, follows the page's own sellers API and
// writes the JSON to c.out. It returns false (exit 1) on a challenge page or
// when no seller was extracted; a partial list still returns true.
func runExtract(ctx context.Context, c config, httpF, browserF fetcher) bool {
	res, ok, why := fetchPage(ctx, c, httpF, browserF)
	if res == nil || !ok {
		if res != nil {
			fmt.Println("RESULT: FAIL -", why)
		} else {
			fmt.Println("RESULT: FAIL - no strategy returned a response")
		}
		return false
	}
	nd, err := parseNextData(res.Body)
	if err != nil {
		fmt.Println("RESULT: FAIL -", err)
		return false
	}
	var apiBody []byte
	var apiErr error
	if u := nd.Props.PageProps.BaseProduct.ProductsInfo.SellersURL; u == "" {
		apiErr = fmt.Errorf("page has no sellers_url")
	} else {
		apiBody, apiErr = fetchSellersAPI(ctx, res, u, c.lang, c.proxy, c.timeout)
	}
	out, err := buildOutput(c.url, res.Mode, res.Body, apiBody, apiErr, time.Now())
	if err != nil {
		fmt.Println("RESULT: FAIL -", err)
		return false
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	if err := os.WriteFile(c.out, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write output:", err)
		return false
	}
	exp := "unknown"
	if out.ExpectedCount != nil {
		exp = strconv.Itoa(*out.ExpectedCount)
	}
	fmt.Printf("sellers=%d expected=%s complete=%v saved to %s\n", out.SellerCount, exp, out.Complete, c.out)
	for _, w := range out.Warnings {
		fmt.Println("warning:", w)
	}
	if out.SellerCount == 0 {
		fmt.Println("RESULT: FAIL - no sellers extracted")
		return false
	}
	fmt.Println("RESULT: OK")
	return true
}
