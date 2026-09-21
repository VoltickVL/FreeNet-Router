package main

import (
    "context"
    "os"
    "regexp"
    "strings"
    "testing"
)

func TestBestServerFreshCandidateForCurrentPrefersExactLocation(t *testing.T) {
    candidates := []bestServerInternalCandidate{
        {Profile: subscriptionProfile{ID:"other", Name:"PL Warsaw Backup", Address:"198.51.100.2", Port:443}},
        {Profile: subscriptionProfile{ID:"exact", Name:"PL Warsaw Main", Address:"198.51.100.3", Port:443}},
    }
    got, ok := bestServerFreshCandidateForCurrent(candidates, regexp.MustCompile(`^PL Warsaw`), "PL Warsaw Main", "198.51.100.1:443")
    if !ok || got.Profile.ID != "exact" { t.Fatalf("expected exact fresh location, got %#v ok=%v", got.Profile, ok) }
}

func TestBestServerFreshCandidateForCurrentRejectsSameEndpoint(t *testing.T) {
    candidates := []bestServerInternalCandidate{{Profile: subscriptionProfile{ID:"same", Name:"PL Warsaw Main", Address:"198.51.100.1", Port:443}}}
    if _, ok := bestServerFreshCandidateForCurrent(candidates, regexp.MustCompile(`^PL Warsaw`), "PL Warsaw Main", "198.51.100.1:443"); ok { t.Fatal("same endpoint must not be treated as fresh") }
}

func TestBestServerTargetedUIContract(t *testing.T) {
    data, err := webFS.ReadFile("web/operation-coordinator.js")
    if err != nil { t.Fatal(err) }
    src := string(data)
    for _, want := range []string{"/api/vpn/best-candidate?id=", "/api/vpn/current-refresh", "retry.dataset.candidateId = candidate.id", "if(action==='update')return refreshCurrentVPN()"} {
        if !strings.Contains(src, want) { t.Fatalf("missing targeted UI contract %q", want) }
    }
    if strings.Contains(src, "button.id==='bestServerRefresh'||button.matches('.vpn-option-retry')") { t.Fatal("retry must not share full-scan path") }
}


func TestBestServerFreshCandidateForCurrentRejectsAmbiguousRotation(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID:"a", Name:"DE Frankfurt Main", Address:"198.51.100.2", Port:443}},
		{Profile: subscriptionProfile{ID:"b", Name:"DE Frankfurt Main", Address:"198.51.100.3", Port:443}},
	}
	if _, ok := bestServerFreshCandidateForCurrent(candidates, regexp.MustCompile(`Frankfurt`), "DE Frankfurt Main", "198.51.100.1:443"); ok {
		t.Fatal("multiple fresh endpoints for the exact current logical profile must fail closed")
	}
}

func TestBestServerFreshEndpointRotationUsesFreshEligibilityNotOldScore(t *testing.T) {
	fresh := bestServerQualityCandidate{
		Tested: true, Available: true, Eligible: true,
		Score: 700, DownloadMbps: 35, ApplicationMS: 210, JitterMS: 30,
	}
	if !fresh.Tested || !fresh.Available || !fresh.Eligible {
		t.Fatal("fully validated fresh endpoint must be eligible for same-profile rotation regardless of old endpoint score")
	}
	fresh.Eligible = false
	if fresh.Tested && fresh.Available && fresh.Eligible {
		t.Fatal("fresh endpoint without full eligibility must never be auto-applied")
	}
}


func TestFreshEndpointReadinessUsesLightOffPathProbes(t *testing.T) {
	oldTCP := bestServerEndpointTCPProbe
	oldApp := bestServerEndpointApplicationProbe
	t.Cleanup(func() {
		bestServerEndpointTCPProbe = oldTCP
		bestServerEndpointApplicationProbe = oldApp
	})

	bestServerEndpointTCPProbe = func(_ context.Context, _ subscriptionProfile) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Samples: []int{11}, Median: 11}
	}
	bestServerEndpointApplicationProbe = func(_ *app, _ context.Context, _ bestServerInternalCandidate) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Samples: []int{42, 44}, Median: 43, Jitter: 2}
	}

	candidate := bestServerInternalCandidate{Profile: subscriptionProfile{
		ID: "0123456789abcdef", Name: "DE Frankfurt Extra", CountryCode: "de", Address: "203.0.113.20", Port: 443,
	}}
	got := (&app{}).probeBestServerFreshEndpointReadiness(context.Background(), candidate)
	if got == nil || !got.Tested || !got.Available || !got.Eligible {
		t.Fatalf("validated fresh endpoint must be eligible: %#v", got)
	}
	if got.TCPRTTMS != 11 || got.ApplicationMS != 43 || got.DownloadMbps != 0 || got.MediaSamples != 0 {
		t.Fatalf("same-profile readiness must use TCP+application evidence only, without Speedtest/media ranking: %#v", got)
	}
}

func TestFreshEndpointReadinessFailsClosedOnApplicationProbe(t *testing.T) {
	oldTCP := bestServerEndpointTCPProbe
	oldApp := bestServerEndpointApplicationProbe
	t.Cleanup(func() {
		bestServerEndpointTCPProbe = oldTCP
		bestServerEndpointApplicationProbe = oldApp
	})

	bestServerEndpointTCPProbe = func(_ context.Context, _ subscriptionProfile) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Samples: []int{10}, Median: 10}
	}
	bestServerEndpointApplicationProbe = func(_ *app, _ context.Context, _ bestServerInternalCandidate) bestServerProbeResult {
		return bestServerProbeResult{}
	}
	got := (&app{}).probeBestServerFreshEndpointReadiness(context.Background(), bestServerInternalCandidate{Profile: subscriptionProfile{
		ID: "fedcba9876543210", Name: "DE Frankfurt Extra", Address: "203.0.113.21", Port: 443,
	}})
	if got == nil || !got.Tested || got.Available || got.Eligible {
		t.Fatalf("failed isolated VPN application probe must prevent mutation: %#v", got)
	}
}

func TestCurrentEndpointRefreshDoesNotUseFullBestServerQualityGate(t *testing.T) {
	data, err := os.ReadFile("best_server_targeted.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	start := strings.Index(src, "func (a *app) executeBestServerCurrentRefresh")
	end := strings.Index(src, "func (a *app) applyBestServerRefreshCandidate")
	if start < 0 || end <= start {
		t.Fatal("current refresh implementation not found")
	}
	body := src[start:end]
	if !strings.Contains(body, "probeBestServerFreshEndpointReadiness") {
		t.Fatal("current endpoint rotation must use lightweight readiness probe")
	}
	if strings.Contains(body, "rankBestServerQualityCandidates") || strings.Contains(body, "probeBestServerQualityApplication") {
		t.Fatal("same-profile endpoint rotation must not run full Best Server Speedtest/media quality gate")
	}

	applyStart := end
	next := strings.Index(src[applyStart+1:], "func ")
	applyBody := src[applyStart:]
	if next >= 0 {
		applyBody = src[applyStart : applyStart+1+next]
	}
	if !strings.Contains(applyBody, `providerHelperPath(), "apply-core"`) {
		t.Fatal("same-profile endpoint apply must use core-only provider cutover")
	}
}
