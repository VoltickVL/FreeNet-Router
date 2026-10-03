package main

import (
	"os"
	"path/filepath"
	"strings"
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
	cases := []struct {
		label string
		want  string
	}{
		{label: "PL Варшава, Польша, Extra", want: "pl"},
		{label: "🇧🇪 Belgium, Brussels, Extra", want: "be"},
		{label: "🇮🇹 Italy, Milan, Extra", want: "it"},
		{label: "be Belgium, Brussels, Extra", want: "be"},
		{label: "Текущий VPN", want: ""},
	}
	for _, tc := range cases {
		if got := bestServerCountryCodeFromLabel(tc.label); got != tc.want {
			t.Fatalf("label %q: expected %q, got %q", tc.label, tc.want, got)
		}
	}
}

func TestMarkBestServerCurrentProbeFailureIsExplicit(t *testing.T) {
	candidate := bestServerQualityCandidate{
		Available: true, Eligible: true, DownloadMbps: 125.4,
		FallbackDownloadMbps: 44.2, ThroughputSource: bestServerThroughputCurrentFallback,
	}
	markBestServerCurrentProbeFailure(&candidate)
	if candidate.Available || candidate.Eligible || candidate.DownloadMbps != 0 ||
		candidate.FallbackDownloadMbps != 0 || candidate.ThroughputSource != "" {
		t.Fatalf("dead current VPN must not retain healthy or fallback throughput evidence: %#v", candidate)
	}
	if candidate.DownloadIssue == "" || candidate.Reason == "" {
		t.Fatalf("dead current VPN must expose an explicit diagnostic: %#v", candidate)
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

func TestFilterMeasuredBestServerResultsKeepsDeepTestedNearMiss(t *testing.T) {
	in := []bestServerQualityCandidate{
		{ID: "good", Tested: true, Available: true, Eligible: true, DownloadMbps: 127, MediaSamples: bestServerMediaRequiredRuns},
		{ID: "dash", Tested: true, Available: true, DownloadMbps: 0, MediaSamples: 0, Rejections: []string{"Скорость не измерена"}},
		{ID: "current", Current: true},
		{ID: "untested", Reachable: true, Tested: false},
	}
	got := filterMeasuredBestServerResults(in)
	if len(got) != 3 || got[0].ID != "good" || got[1].ID != "dash" || got[2].ID != "current" {
		t.Fatalf("unexpected visible results: %#v", got)
	}
}


func TestCurrentVPNUsesSameCanonicalPingAndApplicationRTTAsBestServer(t *testing.T) {
	currentData, err := os.ReadFile("best_server_current_live.go")
	if err != nil {
		t.Fatal(err)
	}
	current := string(currentData)
	for _, want := range []string{
		"vpnResult := probeBestServerCanonicalVPNPing(ctx, curlPath, socks)",
		"httpResult := probeBestServerCanonicalApplicationRTT(ctx, curlPath, socks)",
		"candidate.VPNRTTMS = probe.VPN.Median",
	} {
		if !strings.Contains(current, want) {
			t.Fatalf("current VPN canonical measurement contract missing %q", want)
		}
	}

	preflightData, err := os.ReadFile("best_server_preflight.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(preflightData), "withBestServerCandidateSOCKS(ctx, candidate, probeBestServerCanonicalVPNPing)") {
		t.Fatal("Best Server Stage-0 must use the same canonical VPN-ping owner as Current VPN")
	}

	qualityData, err := os.ReadFile("best_server_quality.go")
	if err != nil {
		t.Fatal(err)
	}
	quality := string(qualityData)
	if !strings.Contains(quality, "httpResult := probeBestServerCanonicalApplicationRTT(ctx, curlPath, socks)") {
		t.Fatal("Best Server deep quality must use the same canonical application RTT helper as Current VPN")
	}

	uiData, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(uiData)
	if !strings.Contains(ui, "metricPill('VPN-пинг', metric(candidate, 'vpn')") {
		t.Fatal("Overview must render canonical VPN-ping")
	}
	if strings.Contains(ui, "hasVPNPing ? 'VPN-пинг' : 'Связь с сервером'") {
		t.Fatal("Overview must never substitute raw TCP RTT for canonical VPN-ping")
	}
}
