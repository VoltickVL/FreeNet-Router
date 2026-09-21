package main

import (
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

func TestBestServerFreshEndpointAcceptsEligibleRotationUnlessOldEndpointIsMateriallyBetter(t *testing.T) {
	current := bestServerQualityCandidate{
		Current: true, Tested: true, Available: true, Eligible: true,
		Score: 1000, DownloadMbps: 100, ApplicationMS: 160, JitterMS: 10,
	}
	fresh := bestServerQualityCandidate{
		Tested: true, Available: true, Eligible: true,
		Score: 1005, DownloadMbps: 98, ApplicationMS: 162, JitterMS: 11,
	}
	if !bestServerFreshEndpointAcceptable(current, fresh) {
		t.Fatal("fully eligible equivalent fresh endpoint should be accepted for current logical profile rotation")
	}

	current.Score = 1500
	current.DownloadMbps = 130
	current.ApplicationMS = 120
	current.JitterMS = 5
	fresh.Score = 900
	fresh.DownloadMbps = 60
	fresh.ApplicationMS = 210
	fresh.JitterMS = 30
	if bestServerFreshEndpointAcceptable(current, fresh) {
		t.Fatal("fresh endpoint must be rejected when the active endpoint is materially better")
	}
}
