package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeSmartGeoDataFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), testGeoSiteList(
		testGeoSiteEntry("YOUTUBE", testDomainRule(2, "youtube.com")),
		testGeoSiteEntry("YOUTUBE-ADS", testDomainRule(2, "googlevideo.com")),
		testGeoSiteEntry("PLATI", testDomainRule(2, "plati.market")),
	), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geoip.dat"), testGeoIPList(
		testGeoIPEntry("CLOUDFLARE", false, testCIDR(net.ParseIP("1.1.1.0").To4(), 24)),
		testGeoIPEntry("GOOGLE", false, testCIDR(net.ParseIP("8.8.8.0").To4(), 24)),
	), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geoip-extra.dat"), testGeoIPList(
		testGeoIPEntry("EXTRA-NET", false, testCIDR(net.ParseIP("203.0.113.0").To4(), 24)),
	), 0600); err != nil {
		t.Fatal(err)
	}
}

func decodeGeoDataSuggest(t *testing.T, wBody []byte) geoDataSuggestResponse {
	t.Helper()
	var resp geoDataSuggestResponse
	if err := json.Unmarshal(wBody, &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestGeoDataSuggestCategoryPrefixDoesNotUseDNS(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	writeSmartGeoDataFixture(t, dir)

	previous := geoDataLookupIPAddr
	calls := 0
	geoDataLookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		calls++
		return nil, nil
	}
	defer func() { geoDataLookupIPAddr = previous }()

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/suggest?kind=geosite&q=you")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeGeoDataSuggest(t, w.Body.Bytes())
	if !resp.Success || resp.Mutation != "NONE" || resp.Mode != "prefix" {
		t.Fatalf("resp=%+v", resp)
	}
	got := make([]string, 0, len(resp.Suggestions))
	for _, item := range resp.Suggestions {
		got = append(got, item.Category)
		if item.Selector != "geosite:"+item.Category {
			t.Fatalf("selector=%q item=%+v", item.Selector, item)
		}
	}
	if !reflect.DeepEqual(got, []string{"youtube", "youtube-ads"}) {
		t.Fatalf("categories=%v", got)
	}
	if calls != 0 {
		t.Fatalf("prefix search unexpectedly used DNS %d times", calls)
	}
}

func TestGeoDataSuggestCanonicalGeoSiteWinsOverAlternateSources(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)

	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), testGeoSiteList(
		testGeoSiteEntry("INSTAGRAM", testDomainRule(2, "instagram.com")),
		testGeoSiteEntry("META", testDomainRule(2, "instagram.com")),
	), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geosite_v2fly.dat"), testGeoSiteList(
		testGeoSiteEntry("INSTAGRAM", testDomainRule(2, "instagram.com")),
		testGeoSiteEntry("CATEGORY-SOCIAL-MEDIA-!CN", testDomainRule(2, "instagram.com")),
	), 0600); err != nil {
		t.Fatal(err)
	}

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/suggest?kind=geosite&q=instagram.com")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeGeoDataSuggest(t, w.Body.Bytes())
	if !resp.Success || resp.Mode != "domain" || resp.Mutation != "NONE" {
		t.Fatalf("resp=%+v", resp)
	}
	if len(resp.Suggestions) != 2 {
		t.Fatalf("suggestions=%+v", resp.Suggestions)
	}
	for _, item := range resp.Suggestions {
		if item.File != "geosite.dat" {
			t.Fatalf("alternate source leaked ahead of canonical DAT: %+v", item)
		}
		if !strings.HasPrefix(item.Selector, "geosite:") {
			t.Fatalf("canonical GeoSite selector must not use ext: %+v", item)
		}
	}
}

func TestGeoDataSuggestCanonicalGeoIPWinsOverAlternateSources(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)

	if err := os.WriteFile(filepath.Join(dir, "geoip.dat"), testGeoIPList(
		testGeoIPEntry("FACEBOOK", false, testCIDR(net.ParseIP("157.240.223.0").To4(), 24)),
		testGeoIPEntry("RU-BLOCKED", false, testCIDR(net.ParseIP("157.240.223.0").To4(), 24)),
	), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geoip_v2fly.dat"), testGeoIPList(
		testGeoIPEntry("FACEBOOK", false, testCIDR(net.ParseIP("157.240.223.0").To4(), 24)),
		testGeoIPEntry("FI", false, testCIDR(net.ParseIP("157.240.223.0").To4(), 24)),
	), 0600); err != nil {
		t.Fatal(err)
	}

	previous := geoDataLookupIPAddr
	geoDataLookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("157.240.223.174")}}, nil
	}
	defer func() { geoDataLookupIPAddr = previous }()

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/suggest?kind=geoip&q=instagram.com")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeGeoDataSuggest(t, w.Body.Bytes())
	if !resp.Success || resp.Mode != "dns" || resp.Mutation != "NONE" {
		t.Fatalf("resp=%+v", resp)
	}
	if len(resp.Suggestions) != 2 {
		t.Fatalf("suggestions=%+v", resp.Suggestions)
	}
	for _, item := range resp.Suggestions {
		if item.File != "geoip.dat" {
			t.Fatalf("alternate GeoIP source leaked ahead of canonical DAT: %+v", item)
		}
		if !strings.HasPrefix(item.Selector, "geoip:") {
			t.Fatalf("canonical GeoIP selector must not use ext: %+v", item)
		}
	}
}

func TestGeoDataSuggestCanonicalNoMatchDoesNotUseAlternateSources(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)

	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), testGeoSiteList(
		testGeoSiteEntry("YOUTUBE", testDomainRule(2, "youtube.com")),
	), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geosite_v2fly.dat"), testGeoSiteList(
		testGeoSiteEntry("INSTAGRAM", testDomainRule(2, "instagram.com")),
	), 0600); err != nil {
		t.Fatal(err)
	}

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/suggest?kind=geosite&q=instagram.com")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeGeoDataSuggest(t, w.Body.Bytes())
	if !resp.Success || resp.Mode != "domain" || resp.Mutation != "NONE" {
		t.Fatalf("resp=%+v", resp)
	}
	if len(resp.Suggestions) != 0 {
		t.Fatalf("canonical no-match must not leak alternate DAT suggestions: %+v", resp.Suggestions)
	}
}

func TestGeoDataSuggestFallsBackWhenCanonicalIsMissing(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)

	if err := os.WriteFile(filepath.Join(dir, "geosite_v2fly.dat"), testGeoSiteList(
		testGeoSiteEntry("INSTAGRAM", testDomainRule(2, "instagram.com")),
	), 0600); err != nil {
		t.Fatal(err)
	}

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/suggest?kind=geosite&q=instagram.com")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeGeoDataSuggest(t, w.Body.Bytes())
	if len(resp.Suggestions) != 1 {
		t.Fatalf("suggestions=%+v", resp.Suggestions)
	}
	item := resp.Suggestions[0]
	if item.File != "geosite_v2fly.dat" || item.Selector != "ext:geosite_v2fly.dat:instagram" {
		t.Fatalf("missing-canonical fallback suggestion=%+v", item)
	}
}

func TestGeoDataSuggestBrokenCanonicalFallsBackToUsableCompatibleSource(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)

	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), []byte("not-a-geodata-file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "xkeenip.dat"), []byte("also-not-a-geodata-file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geosite_v2fly.dat"), testGeoSiteList(
		testGeoSiteEntry("STEAM", testDomainRule(2, "steam.com")),
		testGeoSiteEntry("CATEGORY-GAMES", testDomainRule(2, "steam.com")),
	), 0600); err != nil {
		t.Fatal(err)
	}

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/suggest?kind=geosite&q=steam")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeGeoDataSuggest(t, w.Body.Bytes())
	if !resp.Success || resp.Mode != "prefix" || resp.Mutation != "NONE" {
		t.Fatalf("resp=%+v", resp)
	}
	if len(resp.Suggestions) != 1 {
		t.Fatalf("broken canonical DAT must fall back to usable compatible source: %+v", resp.Suggestions)
	}
	item := resp.Suggestions[0]
	if item.File != "geosite_v2fly.dat" || item.Category != "steam" || item.Selector != "ext:geosite_v2fly.dat:steam" {
		t.Fatalf("fallback suggestion=%+v", item)
	}
	if len(resp.Warnings) != 0 {
		t.Fatalf("successful compatible fallback must suppress broken-canonical scan noise: %v", resp.Warnings)
	}
}

type countingReadSeeker struct {
	reader *bytes.Reader
	read   int64
	seeks  int
}

func (c *countingReadSeeker) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	c.read += int64(n)
	return n, err
}

func (c *countingReadSeeker) Seek(offset int64, whence int) (int64, error) {
	c.seeks++
	return c.reader.Seek(offset, whence)
}

func TestGeoDataSuggestPrefixSeekSkipsLargeNestedPayloadIO(t *testing.T) {
	largePayload := make([]byte, 2<<20)
	data := testGeoSiteList(
		testGeoSiteEntry("CATEGORY-ENTERTAINMENT", largePayload),
		testGeoSiteEntry("STEAM", largePayload),
	)
	rs := &countingReadSeeker{reader: bytes.NewReader(data)}
	result, err := searchGeoDataCategoryCodePrefixReadSeeker(context.Background(), rs, int64(len(data)), "steam", maxGeoDataSuggestions)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Categories, []string{"steam"}) {
		t.Fatalf("categories=%v", result.Categories)
	}
	// The production regression is specifically about router IO. A streaming
	// skip would read several MiB here. The seek-aware path should only consume
	// protobuf headers/category prefixes plus bounded bufio prefetch.
	if rs.read >= 1<<20 {
		t.Fatalf("prefix scanner read too much nested payload: read=%d total=%d", rs.read, len(data))
	}
	if rs.seeks < 2 {
		t.Fatalf("prefix scanner did not seek across nested payloads: seeks=%d", rs.seeks)
	}
}

func TestGeoDataSuggestLargeCanonicalGeoSiteEntryRemainsUsable(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)

	// A modern GeoSite category can exceed the historical 16 MiB aggregate
	// entry limit while still consisting entirely of small valid Domain messages.
	// The parser is streaming/seek-aware, so rejecting the whole entry by aggregate
	// size is both unnecessary and breaks later canonical categories.
	largeValue := strings.Repeat("x", int(maxGeoDataStreamValueSize)-128)
	domains := make([][]byte, 0, 280)
	for i := 0; i < cap(domains); i++ {
		domains = append(domains, testDomainRule(3, largeValue))
	}
	largeEntry := testGeoSiteEntry("A-LARGE", domains...)
	if int64(len(largeEntry)) <= maxGeoDataStreamEntrySize {
		t.Fatalf("fixture did not exceed historical entry guard: %d", len(largeEntry))
	}
	data := testGeoSiteList(
		largeEntry,
		testGeoSiteEntry("STEAM", testDomainRule(2, "steam.com")),
	)
	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), data, 0600); err != nil {
		t.Fatal(err)
	}

	for _, rawURL := range []string{
		"/api/geodata/suggest?kind=geosite&q=steam&mode=prefix",
		"/api/geodata/suggest?kind=geosite&q=steam.com",
	} {
		w := doGeoDataAPIRequest(mux, cookie, rawURL)
		if w.Code != http.StatusOK {
			t.Fatalf("url=%s code=%d body=%s", rawURL, w.Code, w.Body.String())
		}
		resp := decodeGeoDataSuggest(t, w.Body.Bytes())
		if !resp.Success || resp.Mutation != "NONE" || len(resp.Suggestions) != 1 {
			t.Fatalf("url=%s resp=%+v", rawURL, resp)
		}
		item := resp.Suggestions[0]
		if item.File != "geosite.dat" || item.Category != "steam" || item.Selector != "geosite:steam" {
			t.Fatalf("url=%s item=%+v", rawURL, item)
		}
		if len(resp.Warnings) != 0 {
			t.Fatalf("url=%s warnings=%v", rawURL, resp.Warnings)
		}
	}
}

func TestGeoDataSuggestPrefixFastPathSkipsNestedPayloadOnTypedDAT(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)

	// Real autocomplete needs only category codes. Model a filename-typed V2Fly
	// GeoSite entry whose nested payload is intentionally not parseable as a
	// Domain message. The category-code fast path must skip it instead of decoding
	// every nested rule on each keystroke.
	brokenNested := []byte{0xff, 0xff, 0xff, 0x7f}
	if err := os.WriteFile(filepath.Join(dir, "geosite_v2fly.dat"), testGeoSiteList(
		testGeoSiteEntry("STEAM", brokenNested),
		testGeoSiteEntry("STEAM-TOOLS", brokenNested),
	), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "xkeenip.dat"), []byte("not-geodata"), 0600); err != nil {
		t.Fatal(err)
	}

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/suggest?kind=geosite&q=steam&mode=prefix")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeGeoDataSuggest(t, w.Body.Bytes())
	if !resp.Success || resp.Mode != "prefix" || resp.Mutation != "NONE" {
		t.Fatalf("resp=%+v", resp)
	}
	got := make([]string, 0, len(resp.Suggestions))
	for _, item := range resp.Suggestions {
		got = append(got, item.Category)
		if item.File != "geosite_v2fly.dat" {
			t.Fatalf("unexpected fallback file in fast-path result: %+v", item)
		}
	}
	if !reflect.DeepEqual(got, []string{"steam", "steam-tools"}) {
		t.Fatalf("categories=%v", got)
	}
	if len(resp.Warnings) != 0 {
		t.Fatalf("typed DAT success must not probe/report unknown fallback noise: %v", resp.Warnings)
	}
}

func TestGeoDataSuggestExplicitPrefixModeNeverTreatsCategoryAsHost(t *testing.T) {
	query, err := classifyGeoDataSuggestQueryWithMode(GeoDataSite, "steam", "prefix")
	if err != nil {
		t.Fatal(err)
	}
	if query.Mode != "prefix" || query.Value != "steam" {
		t.Fatalf("query=%+v", query)
	}
	if _, err := classifyGeoDataSuggestQueryWithMode(GeoDataSite, "steam", "bogus"); err == nil {
		t.Fatal("unsupported mode unexpectedly accepted")
	}
}

func TestGeoDataSuggestGeoSiteURLUsesHostLookup(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	writeSmartGeoDataFixture(t, dir)

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/suggest?kind=geosite&q=https%3A%2F%2Fwww.youtube.com%2Fwatch%3Fv%3D1")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeGeoDataSuggest(t, w.Body.Bytes())
	if resp.Mode != "domain" || len(resp.Suggestions) != 1 || resp.Suggestions[0].Category != "youtube" {
		t.Fatalf("resp=%+v", resp)
	}
	if got := resp.Suggestions[0].Evidence; len(got) != 1 || got[0] != "www.youtube.com" {
		t.Fatalf("evidence=%v", got)
	}
}

func TestGeoDataSuggestGeoIPDomainUsesBoundedResolverAndEvidence(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	writeSmartGeoDataFixture(t, dir)

	previous := geoDataLookupIPAddr
	calls := 0
	geoDataLookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
		calls++
		if host != "example.com" {
			t.Fatalf("host=%q", host)
		}
		return []net.IPAddr{
			{IP: net.ParseIP("8.8.8.8")},
			{IP: net.ParseIP("1.1.1.7")},
			{IP: net.ParseIP("8.8.8.8")},
		}, nil
	}
	defer func() { geoDataLookupIPAddr = previous }()

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/suggest?kind=geoip&q=example.com")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeGeoDataSuggest(t, w.Body.Bytes())
	if resp.Mode != "dns" || resp.Mutation != "NONE" || calls != 1 {
		t.Fatalf("resp=%+v calls=%d", resp, calls)
	}
	if !reflect.DeepEqual(resp.Resolved, []string{"1.1.1.7", "8.8.8.8"}) {
		t.Fatalf("resolved=%v", resp.Resolved)
	}
	byCategory := map[string]geoDataSuggestion{}
	for _, item := range resp.Suggestions {
		byCategory[item.Category] = item
	}
	if got := byCategory["cloudflare"].Evidence; !reflect.DeepEqual(got, []string{"1.1.1.7"}) {
		t.Fatalf("cloudflare evidence=%v", got)
	}
	if got := byCategory["google"].Evidence; !reflect.DeepEqual(got, []string{"8.8.8.8"}) {
		t.Fatalf("google evidence=%v", got)
	}
}

func TestGeoDataSuggestGeoIPPrefixAndExtSelector(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	writeSmartGeoDataFixture(t, dir)

	previous := geoDataLookupIPAddr
	calls := 0
	geoDataLookupIPAddr = func(context.Context, string) ([]net.IPAddr, error) {
		calls++
		return nil, nil
	}
	defer func() { geoDataLookupIPAddr = previous }()

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/suggest?kind=geoip&q=extra&file=geoip-extra.dat")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	resp := decodeGeoDataSuggest(t, w.Body.Bytes())
	if calls != 0 || resp.Mode != "prefix" {
		t.Fatalf("resp=%+v calls=%d", resp, calls)
	}
	var found *geoDataSuggestion
	for i := range resp.Suggestions {
		if resp.Suggestions[i].Category == "extra-net" {
			found = &resp.Suggestions[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("suggestions=%+v", resp.Suggestions)
	}
	if found.File != "geoip-extra.dat" || found.Selector != "ext:geoip-extra.dat:extra-net" || found.ExtSelector != "ext:geoip-extra.dat:extra-net" {
		t.Fatalf("item=%+v", *found)
	}
}

func TestGeoDataSuggestRejectsWrongKindLiteralAndBadExt(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	writeSmartGeoDataFixture(t, dir)

	for _, rawURL := range []string{
		"/api/geodata/suggest?kind=geosite&q=1.1.1.1",
		"/api/geodata/suggest?kind=geoip&q=geosite%3Ayout",
		"/api/geodata/suggest?kind=geoip&q=ext%3A..%2Fgeoip.dat%3Apri",
	} {
		w := doGeoDataAPIRequest(mux, cookie, rawURL)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("url=%s code=%d body=%s", rawURL, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "error") {
			t.Fatalf("missing precise error: %s", w.Body.String())
		}
	}
}

func TestGeoDataSuggestRequiresAuthentication(t *testing.T) {
	_, mux, _, dir := testGeoDataAPIApp(t)
	writeSmartGeoDataFixture(t, dir)
	w := doGeoDataAPIRequest(mux, nil, "/api/geodata/suggest?kind=geosite&q=you")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}
