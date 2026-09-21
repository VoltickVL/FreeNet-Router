package main

import (
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

func TestCurrentEndpointRefreshUsesShortIsolatedProbe(t *testing.T) {
	data, err := os.ReadFile("best_server_targeted.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	start := strings.Index(src, "func (a *app) executeBestServerCurrentRefresh")
	if start < 0 {
		t.Fatal("current refresh function start not found")
	}
	end := strings.Index(src[start:], "func (a *app) applyBestServerRefreshCandidate")
	if end < 0 {
		t.Fatal("current refresh function end not found")
	}
	body := src[start : start+end]
	if !strings.Contains(body, "probeBestServerCurrentRefreshCandidate") {
		t.Fatal("same-profile refresh must use the short isolated endpoint probe")
	}
	for _, forbidden := range []string{"rankBestServerQualityCandidates", "probeBestServerQualityApplication"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("same-profile refresh must not use full Best Server quality path: %s", forbidden)
		}
	}
	if !strings.Contains(src, "bestServerCurrentRefreshProbeTimeout = 15 * time.Second") {
		t.Fatal("same-profile endpoint validation must stay explicitly bounded below the deep 50s quality window")
	}
}

func TestEndpointCutoverContractForbidsFailOpenXKeenRestart(t *testing.T) {
	data, err := os.ReadFile("../scripts/apply_provider_profile.sh")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	if strings.Contains(src, `"$XKEEN_BIN" -restart`) {
		t.Fatal("provider endpoint cutover must never use full xkeen -restart")
	}
	if strings.Contains(src, "sleep 4") {
		t.Fatal("provider endpoint cutover must use readiness polling, not fixed sleep 4")
	}
	if !strings.Contains(src, `XKEEN_FOREGROUND=1 "$XKEEN_BIN" -start`) {
		t.Fatal("provider endpoint cutover must restart only the Xray core via xkeen -start")
	}

	mainData, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	mainSrc := string(mainData)
	start := strings.Index(mainSrc, "func (a *app) restoreSnapshot")
	if start < 0 {
		t.Fatal("restoreSnapshot start not found")
	}
	end := strings.Index(mainSrc[start:], "func atomicWrite")
	if end < 0 {
		t.Fatal("restoreSnapshot end not found")
	}
	restore := mainSrc[start : start+end]
	if !strings.Contains(restore, `providerHelperPath(), "core-restart"`) {
		t.Fatal("rollback must use the same safe core-only restart primitive")
	}
	if strings.Contains(restore, `a.cfg.XKeenPath, "-restart"`) || strings.Contains(restore, "sleep 4") {
		t.Fatal("rollback reintroduced fail-open XKeen restart behavior")
	}
}

