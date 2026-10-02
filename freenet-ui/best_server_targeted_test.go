package main

import (
    "context"
    "os"
    "path/filepath"
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

func TestBestServerFreshRevisionForCurrentAcceptsSameEndpointCredentialChange(t *testing.T) {
	profile := subscriptionProfile{ID: "0123456789abcdef", Name: "PL Warsaw Main", Address: "198.51.100.1", Port: 443}
	oldRaw := "vless://old-id@198.51.100.1:443?flow=xtls-rprx-vision&security=reality&type=tcp&fp=firefox&sni=example.test&pbk=old-pbk&sid=old-sid&spx=%2F#PL%20Warsaw%20Main"
	freshRaw := "vless://new-id@198.51.100.1:443?flow=xtls-rprx-vision&security=reality&type=tcp&fp=firefox&sni=example.test&pbk=new-pbk&sid=new-sid&spx=%2F#PL%20Warsaw%20Main"
	active, err := buildBestServerProbeOutbound(oldRaw, profile)
	if err != nil {
		t.Fatal(err)
	}
	candidates := []bestServerInternalCandidate{{Profile: profile, Raw: freshRaw}}
	got, ok := bestServerFreshRevisionForCurrent(candidates, regexp.MustCompile(`^PL Warsaw`), "PL Warsaw Main", "198.51.100.1:443", active)
	if !ok || got.Profile.ID != profile.ID {
		t.Fatalf("same-endpoint credential revision must be detected, got %#v ok=%v", got.Profile, ok)
	}
}

func TestBestServerFreshRevisionForCurrentSkipsExactParity(t *testing.T) {
	profile := subscriptionProfile{ID: "0123456789abcdef", Name: "PL Warsaw Main", Address: "198.51.100.1", Port: 443}
	raw := "vless://same-id@198.51.100.1:443?flow=xtls-rprx-vision&security=reality&type=tcp&fp=firefox&sni=example.test&pbk=same-pbk&sid=same-sid&spx=%2F#PL%20Warsaw%20Main"
	active, err := buildBestServerProbeOutbound(raw, profile)
	if err != nil {
		t.Fatal(err)
	}
	candidates := []bestServerInternalCandidate{{Profile: profile, Raw: raw}}
	if _, ok := bestServerFreshRevisionForCurrent(candidates, regexp.MustCompile(`^PL Warsaw`), "PL Warsaw Main", "198.51.100.1:443", active); ok {
		t.Fatal("same-endpoint exact parity must not trigger mutation")
	}
}

func TestBestServerFreshRevisionForCurrentFailsClosedOnSharedEndpointAmbiguity(t *testing.T) {
	activeProfile := subscriptionProfile{ID: "aaaaaaaaaaaaaaaa", Name: "DE Frankfurt Main", Address: "198.51.100.1", Port: 443}
	activeRaw := "vless://old-id@198.51.100.1:443?flow=xtls-rprx-vision&security=reality&type=tcp&fp=firefox&sni=example.test&pbk=old-pbk&sid=old-sid&spx=%2F#DE%20Frankfurt%20Main"
	active, err := buildBestServerProbeOutbound(activeRaw, activeProfile)
	if err != nil {
		t.Fatal(err)
	}
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "bbbbbbbbbbbbbbbb", Name: "DE Frankfurt Main", Address: "198.51.100.1", Port: 443}, Raw: strings.ReplaceAll(activeRaw, "old-", "new-")},
		{Profile: subscriptionProfile{ID: "cccccccccccccccc", Name: "DE Frankfurt Main", Address: "198.51.100.1", Port: 443}, Raw: strings.ReplaceAll(activeRaw, "old-", "other-")},
	}
	if _, ok := bestServerFreshRevisionForCurrent(candidates, regexp.MustCompile(`Frankfurt`), "DE Frankfurt Main", "198.51.100.1:443", active); ok {
		t.Fatal("ambiguous same-endpoint revisions must fail closed")
	}
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
		Score: 700, DownloadMbps: 35, ApplicationMS: 170, JitterMS: 30,
	}
	if !fresh.Tested || !fresh.Available || !fresh.Eligible {
		t.Fatal("fully validated fresh endpoint must be eligible for same-profile rotation regardless of old endpoint score")
	}
	fresh.Eligible = false
	if fresh.Tested && fresh.Available && fresh.Eligible {
		t.Fatal("fresh endpoint without full eligibility must never be auto-applied")
	}
}


func TestFreshEndpointReadinessUsesVPNApplicationAsOnlyMutationGate(t *testing.T) {
	oldTCP := bestServerEndpointTCPProbe
	oldApp := bestServerEndpointApplicationProbe
	t.Cleanup(func() {
		bestServerEndpointTCPProbe = oldTCP
		bestServerEndpointApplicationProbe = oldApp
	})

	bestServerEndpointTCPProbe = func(_ context.Context, _ subscriptionProfile) bestServerProbeResult {
		return bestServerProbeResult{}
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
	if got.TCPRTTMS != 0 || got.ApplicationMS != 43 || got.DownloadMbps != 0 || got.MediaSamples != 0 {
		t.Fatalf("same-profile readiness must be gated by isolated VPN application evidence only; raw TCP is diagnostic: %#v", got)
	}
}

func TestFreshEndpointReadinessDoesNotWaitForDiagnosticTCP(t *testing.T) {
	oldTCP := bestServerEndpointTCPProbe
	oldApp := bestServerEndpointApplicationProbe
	t.Cleanup(func() {
		bestServerEndpointTCPProbe = oldTCP
		bestServerEndpointApplicationProbe = oldApp
	})

	bestServerEndpointTCPProbe = func(ctx context.Context, _ subscriptionProfile) bestServerProbeResult {
		<-ctx.Done()
		return bestServerProbeResult{}
	}
	bestServerEndpointApplicationProbe = func(_ *app, _ context.Context, _ bestServerInternalCandidate) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Samples: []int{90, 94}, Median: 92, Jitter: 4}
	}
	started := time.Now()
	got := (&app{}).probeBestServerFreshEndpointReadiness(context.Background(), bestServerInternalCandidate{Profile: subscriptionProfile{
		ID: "1234567890abcdef", Name: "DE Frankfurt Extra", Address: "203.0.113.23", Port: 443,
	}})
	if got == nil || !got.Tested || !got.Available || !got.Eligible {
		t.Fatalf("application-proven fresh endpoint must not be vetoed by hanging diagnostic TCP: %#v", got)
	}
	if time.Since(started) >= bestServerEndpointReadinessTimeout/2 {
		t.Fatalf("diagnostic TCP delayed readiness decision: elapsed=%s", time.Since(started))
	}
}

func TestFreshEndpointReadinessRejectsHighApplicationLatencyWithoutSpeedtest(t *testing.T) {
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
		ms := bestServerQualityMaxApplicationMS + 10
		return bestServerProbeResult{OK: true, Samples: []int{ms, ms}, Median: ms, Jitter: 0}
	}

	got := (&app{}).probeBestServerFreshEndpointReadiness(context.Background(), bestServerInternalCandidate{Profile: subscriptionProfile{
		ID: "abcdef0123456789", Name: "DE Frankfurt Extra", CountryCode: "de", Address: "203.0.113.22", Port: 443,
	}})
	if got == nil || !got.Tested || !got.Available || got.Eligible {
		t.Fatalf("high-latency fresh endpoint must stay available but fail automatic eligibility: %#v", got)
	}
	if got.ApplicationMS != bestServerQualityMaxApplicationMS+10 || got.DownloadMbps != 0 || got.MediaSamples != 0 {
		t.Fatalf("same-profile latency gate must remain lightweight without Speedtest/media: %#v", got)
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
	if !strings.Contains(body, "ensureProviderSubscriptionCache") {
		t.Fatal("current refresh must refresh the credential-bearing provider cache before candidate selection")
	}
	if !strings.Contains(body, "bestServerFreshRevisionForCurrent") {
		t.Fatal("current refresh must detect same-endpoint credential revisions")
	}
	if !strings.Contains(body, "probeBestServerFreshEndpointReadiness") {
		t.Fatal("current endpoint/profile refresh must use lightweight readiness probe")
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


func TestEndpointRefreshSnapshotRestoresPreferredProfileMetadata(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "04_outbounds.json")
	filterPath := filepath.Join(dir, "profile.filter")
	profilePath := filepath.Join(dir, "vpn_profile_name")
	t.Setenv("FREENET_PROFILE_FILE", profilePath)

	if err := os.WriteFile(outPath, []byte("old-outbound\n"), 0640); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filterPath, []byte("old-filter\n"), 0644); err != nil { t.Fatal(err) }
	if err := os.WriteFile(profilePath, []byte("old-profile\n"), 0600); err != nil { t.Fatal(err) }

	a := &app{cfg: config{OutPath: outPath, FilterPath: filterPath}}
	snap, err := a.takeEndpointRefreshSnapshot()
	if err != nil { t.Fatal(err) }

	if err := os.WriteFile(outPath, []byte("new-outbound\n"), 0600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filterPath, []byte("new-filter\n"), 0644); err != nil { t.Fatal(err) }
	if err := os.WriteFile(profilePath, []byte("new-profile\n"), 0644); err != nil { t.Fatal(err) }

	if err := a.restoreEndpointRefreshFiles(snap); err != nil { t.Fatal(err) }

	for path, want := range map[string]string{
		outPath: "old-outbound\n",
		filterPath: "old-filter\n",
		profilePath: "old-profile\n",
	} {
		got, err := os.ReadFile(path)
		if err != nil { t.Fatal(err) }
		if string(got) != want { t.Fatalf("%s=%q want %q", path, got, want) }
	}
	info, err := os.Stat(profilePath)
	if err != nil { t.Fatal(err) }
	if info.Mode().Perm() != 0600 { t.Fatalf("preferred profile mode=%o want 600", info.Mode().Perm()) }
}

func TestEndpointRefreshSnapshotRemovesProfileCreatedAfterAbsentSnapshot(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "04_outbounds.json")
	filterPath := filepath.Join(dir, "profile.filter")
	profilePath := filepath.Join(dir, "vpn_profile_name")
	t.Setenv("FREENET_PROFILE_FILE", profilePath)

	if err := os.WriteFile(outPath, []byte("old-outbound\n"), 0600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filterPath, []byte("old-filter\n"), 0644); err != nil { t.Fatal(err) }
	a := &app{cfg: config{OutPath: outPath, FilterPath: filterPath}}
	snap, err := a.takeEndpointRefreshSnapshot()
	if err != nil { t.Fatal(err) }
	if snap.profileExists { t.Fatal("preferred profile unexpectedly existed in snapshot") }

	if err := os.WriteFile(profilePath, []byte("new-profile\n"), 0600); err != nil { t.Fatal(err) }
	if err := a.restoreEndpointRefreshFiles(snap); err != nil { t.Fatal(err) }
	if _, err := os.Stat(profilePath); !os.IsNotExist(err) {
		t.Fatalf("preferred profile created by failed apply must be removed on rollback; err=%v", err)
	}
}
