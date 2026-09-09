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
