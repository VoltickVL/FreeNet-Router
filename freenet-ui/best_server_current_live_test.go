package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadBestServerActiveOutbound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "04_outbounds.json")
	body := `{"outbounds":[{"tag":"direct","protocol":"freedom"},{"tag":"vless-reality","protocol":"vless","settings":{"vnext":[{"address":"143.20.254.191","port":443,"users":[{"id":"00000000-0000-0000-0000-000000000000"}]}]},"streamSettings":{"security":"reality"}}]}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	outbound, endpoint, ok := readBestServerActiveOutbound(path)
	if !ok {
		t.Fatal("expected active vless-reality outbound")
	}
	if endpoint != "143.20.254.191:443" {
		t.Fatalf("unexpected endpoint %q", endpoint)
	}
	if outbound["tag"] != "vless-reality" {
		t.Fatalf("wrong outbound selected: %#v", outbound)
	}
}

func TestBestServerProfileFromEndpoint(t *testing.T) {
	profile, ok := bestServerProfileFromEndpoint("143.20.254.191:443")
	if !ok || profile.Address != "143.20.254.191" || profile.Port != 443 {
		t.Fatalf("unexpected profile: %#v ok=%v", profile, ok)
	}
	if _, ok := bestServerProfileFromEndpoint("broken-endpoint"); ok {
		t.Fatal("invalid endpoint must fail closed")
	}
}

func TestBestServerCountryCodeFromLabel(t *testing.T) {
	if got := bestServerCountryCodeFromLabel("PL Варшава, Польша, Extra"); got != "pl" {
		t.Fatalf("expected pl, got %q", got)
	}
	if got := bestServerCountryCodeFromLabel("Текущий VPN"); got != "" {
		t.Fatalf("unexpected code %q", got)
	}
}

func TestFilterForeignBestServerCandidatesExcludesRussianAndWhitelist(t *testing.T) {
	in := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "1", Name: "SK Bratislava, Slovakia, Extra", CountryCode: "sk"}},
		{Profile: subscriptionProfile{ID: "2", Name: "NL Netherlands, Extra Whitelist", CountryCode: "nl"}},
		{Profile: subscriptionProfile{ID: "3", Name: "RU Moscow, Extra", CountryCode: "ru"}},
	}
	got := filterForeignBestServerCandidates(in)
	if len(got) != 1 || got[0].Profile.ID != "1" {
		t.Fatalf("unexpected automatic candidates: %#v", got)
	}
}

func TestFilterMeasuredBestServerResultsDropsDashSpeedRows(t *testing.T) {
	in := []bestServerQualityCandidate{
		{ID: "good", Tested: true, DownloadMbps: 127, MediaSamples: bestServerMediaRequiredRuns},
		{ID: "dash", Tested: true, DownloadMbps: 0, MediaSamples: 0},
		{ID: "current", Current: true},
	}
	got := filterMeasuredBestServerResults(in)
	if len(got) != 2 || got[0].ID != "good" || got[1].ID != "current" {
		t.Fatalf("unexpected measured results: %#v", got)
	}
}

func TestCountMeasuredBestServerAlternatives(t *testing.T) {
	in := []bestServerQualityCandidate{
		{ID: "one", Tested: true, DownloadMbps: 100, MediaSamples: bestServerMediaRequiredRuns},
		{ID: "two", Tested: true, DownloadMbps: 90, MediaSamples: bestServerMediaRequiredRuns},
		{ID: "current", Current: true, Tested: true, DownloadMbps: 120, MediaSamples: bestServerMediaRequiredRuns},
		{ID: "dash", Tested: true},
	}
	if got := countMeasuredBestServerAlternatives(in); got != 2 {
		t.Fatalf("expected 2 measured alternatives, got %d", got)
	}
}

func TestHasMeasuredBestServerCurrentRequiresCompleteBaseline(t *testing.T) {
	complete := bestServerQualityCandidate{
		Current: true, ApplicationMS: 180, MediaSamples: bestServerMediaRequiredRuns,
		ServiceOK: 4, ServiceTotal: 4, DownloadMbps: 120,
	}
	if !hasMeasuredBestServerCurrent([]bestServerQualityCandidate{complete}) {
		t.Fatal("complete current baseline must be recognized")
	}
	incomplete := complete
	incomplete.MediaSamples = 0
	if hasMeasuredBestServerCurrent([]bestServerQualityCandidate{incomplete}) {
		t.Fatal("incomplete current baseline must not allow early stop")
	}
}
