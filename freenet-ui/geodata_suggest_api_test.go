package main

import (
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

func TestGeoDataSuggestSuccessfulMixedDirectorySuppressesGenericScanNoise(t *testing.T) {
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
	if len(resp.Suggestions) != 1 || resp.Suggestions[0].Category != "steam" || resp.Suggestions[0].File != "geosite_v2fly.dat" {
		t.Fatalf("suggestions=%+v", resp.Suggestions)
	}
	if resp.Suggestions[0].Selector != "ext:geosite_v2fly.dat:steam" {
		t.Fatalf("selector=%q", resp.Suggestions[0].Selector)
	}
	if len(resp.Warnings) != 0 {
		t.Fatalf("successful mixed-DAT lookup must suppress unrelated generic scan warnings: %v", resp.Warnings)
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

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/suggest?kind=geoip&q=extra")
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
