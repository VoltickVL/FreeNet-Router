package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func emergencyTestCandidate(id, country string) bestServerInternalCandidate {
	return bestServerInternalCandidate{
		Profile: subscriptionProfile{
			ID: id, Name: strings.ToUpper(country) + " " + id,
			CountryCode: country, Address: "198.51.100.10", Port: 443,
		},
		Raw: "test-" + id,
	}
}

func resetEmergencyRTTCache(t *testing.T) {
	t.Helper()
	providerProfileRTTCache.Lock()
	old := providerProfileRTTCache.Entries
	providerProfileRTTCache.Entries = nil
	providerProfileRTTCache.Unlock()
	t.Cleanup(func() {
		providerProfileRTTCache.Lock()
		providerProfileRTTCache.Entries = old
		providerProfileRTTCache.Unlock()
	})
}

func TestEmergencyCandidateOrderDiversifiesCountries(t *testing.T) {
	resetEmergencyRTTCache(t)
	in := []bestServerInternalCandidate{
		emergencyTestCandidate("de2", "de"),
		emergencyTestCandidate("de1", "de"),
		emergencyTestCandidate("fr1", "fr"),
		emergencyTestCandidate("nl1", "nl"),
	}
	got := automationEmergencyOrderedCandidates(in)
	if len(got) != 4 {
		t.Fatalf("ordered candidates=%d want 4", len(got))
	}
	countries := []string{
		got[0].Profile.CountryCode,
		got[1].Profile.CountryCode,
		got[2].Profile.CountryCode,
		got[3].Profile.CountryCode,
	}
	if strings.Join(countries, ",") != "de,fr,nl,de" {
		t.Fatalf("country-diverse order=%v", countries)
	}
}

func TestEmergencyScanUsesBoundedApplicationReadyCohort(t *testing.T) {
	resetEmergencyRTTCache(t)
	dir := t.TempDir()
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(dir, "settings.state"))

	oldDiscover := automationEmergencyDiscoverCandidates
	oldProbe := automationEmergencyCandidateProbe
	oldRTTProbe := automationEmergencyRTTProbe
	t.Cleanup(func() {
		automationEmergencyDiscoverCandidates = oldDiscover
		automationEmergencyCandidateProbe = oldProbe
		automationEmergencyRTTProbe = oldRTTProbe
	})

	candidates := []bestServerInternalCandidate{
		emergencyTestCandidate("at1", "at"),
		emergencyTestCandidate("ch1", "ch"),
		emergencyTestCandidate("cz1", "cz"),
		emergencyTestCandidate("de1", "de"),
		emergencyTestCandidate("fr1", "fr"),
		emergencyTestCandidate("it1", "it"),
		emergencyTestCandidate("nl1", "nl"),
		emergencyTestCandidate("pl1", "pl"),
		emergencyTestCandidate("se1", "se"),
		emergencyTestCandidate("uk1", "gb"),
	}
	automationEmergencyDiscoverCandidates = func(*app, context.Context) ([]bestServerInternalCandidate, bool, error) {
		return candidates, false, nil
	}
	automationEmergencyRTTProbe = func(_ *app, _ context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Samples: []int{100}, Median: 100}
	}

	var mu sync.Mutex
	probed := []string{}
	automationEmergencyCandidateProbe = func(_ *app, _ context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
		mu.Lock()
		probed = append(probed, candidate.Profile.ID)
		mu.Unlock()
		if candidate.Profile.ID == "fr1" {
			return bestServerProbeResult{OK: true, Samples: []int{137}, Median: 137}
		}
		return bestServerProbeResult{}
	}

	a := &app{cfg: config{
		OutPath: filepath.Join(dir, "04_outbounds.json"),
		FilterPath: filepath.Join(dir, "provider.filter"),
	}}
	scan, err := a.scanAutomationEmergencyReplacement(context.Background(), automationSettings{
		Mode: automationModeBest, Policy: automationPolicyDegraded, CountryScope: automationCountryRegion,
	}, "de")
	if err != nil {
		t.Fatal(err)
	}
	if scan.Candidate.Profile.ID != "fr1" || scan.ApplicationMS != 137 {
		t.Fatalf("scan candidate=%q app=%d", scan.Candidate.Profile.ID, scan.ApplicationMS)
	}
	if scan.Checked < 1 || scan.Checked > automationEmergencyCandidateLimit {
		t.Fatalf("checked=%d exceeds bounded cohort=%d", scan.Checked, automationEmergencyCandidateLimit)
	}
	if scan.Total != len(candidates) {
		t.Fatalf("pool=%d want %d", scan.Total, len(candidates))
	}
	if scan.RTTChecked < 1 || scan.RTTChecked > automationEmergencyCandidateLimit || scan.RTTReachable < 1 {
		t.Fatalf("fresh RTT prefilter checked=%d reachable=%d", scan.RTTChecked, scan.RTTReachable)
	}
	mu.Lock()
	count := len(probed)
	mu.Unlock()
	if count > automationEmergencyCandidateLimit {
		t.Fatalf("probed=%d exceeds bounded cohort=%d", count, automationEmergencyCandidateLimit)
	}
}

func TestEmergencyScanAdvancesCursorWhenCohortHasNoReplacement(t *testing.T) {
	resetEmergencyRTTCache(t)
	dir := t.TempDir()
	statePath := filepath.Join(dir, "settings.state")
	t.Setenv("FREENET_SETTINGS_V3_STATE", statePath)

	oldDiscover := automationEmergencyDiscoverCandidates
	oldProbe := automationEmergencyCandidateProbe
	oldRTTProbe := automationEmergencyRTTProbe
	t.Cleanup(func() {
		automationEmergencyDiscoverCandidates = oldDiscover
		automationEmergencyCandidateProbe = oldProbe
		automationEmergencyRTTProbe = oldRTTProbe
	})

	candidates := make([]bestServerInternalCandidate, 0, 12)
	countries := []string{"at", "ch", "cz", "de", "fr", "it", "nl", "pl", "se", "gb", "es", "fi"}
	for i, country := range countries {
		candidates = append(candidates, emergencyTestCandidate(country+string(rune('a'+i)), country))
	}
	automationEmergencyDiscoverCandidates = func(*app, context.Context) ([]bestServerInternalCandidate, bool, error) {
		return candidates, false, nil
	}
	automationEmergencyRTTProbe = func(_ *app, _ context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Samples: []int{120}, Median: 120}
	}
	automationEmergencyCandidateProbe = func(*app, context.Context, bestServerInternalCandidate) bestServerProbeResult {
		return bestServerProbeResult{}
	}

	a := &app{cfg: config{
		OutPath: filepath.Join(dir, "04_outbounds.json"),
		FilterPath: filepath.Join(dir, "provider.filter"),
	}}
	scan, err := a.scanAutomationEmergencyReplacement(context.Background(), automationSettings{
		Mode: automationModeBest, Policy: automationPolicyDegraded, CountryScope: automationCountryRegion,
	}, "de")
	if err != nil {
		t.Fatal(err)
	}
	if scan.Candidate.Profile.ID != "" {
		t.Fatalf("unexpected replacement=%q", scan.Candidate.Profile.ID)
	}
	state := v3ParseState(statePath)
	if strings.TrimSpace(state["EMERGENCY_SCAN_CURSOR"]) == "" || state["EMERGENCY_SCAN_CURSOR"] == "0" {
		t.Fatalf("failed cohort did not advance cursor: %v", state)
	}
}

func TestEmergencyFreshVPNPingRanksApplicationReadyCandidates(t *testing.T) {
	resetEmergencyRTTCache(t)
	dir := t.TempDir()
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(dir, "settings.state"))

	oldDiscover := automationEmergencyDiscoverCandidates
	oldProbe := automationEmergencyCandidateProbe
	oldRTTProbe := automationEmergencyRTTProbe
	t.Cleanup(func() {
		automationEmergencyDiscoverCandidates = oldDiscover
		automationEmergencyCandidateProbe = oldProbe
		automationEmergencyRTTProbe = oldRTTProbe
	})

	candidates := []bestServerInternalCandidate{
		emergencyTestCandidate("de1", "de"),
		emergencyTestCandidate("fr1", "fr"),
		emergencyTestCandidate("nl1", "nl"),
	}
	automationEmergencyDiscoverCandidates = func(*app, context.Context) ([]bestServerInternalCandidate, bool, error) {
		return candidates, false, nil
	}
	rtt := map[string]int{"de1": 90, "fr1": 30, "nl1": 60}
	var rttMu sync.Mutex
	rttCalls := 0
	automationEmergencyRTTProbe = func(_ *app, _ context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
		rttMu.Lock()
		rttCalls++
		rttMu.Unlock()
		value := rtt[candidate.Profile.ID]
		return bestServerProbeResult{OK: true, Samples: []int{value}, Median: value}
	}
	automationEmergencyCandidateProbe = func(_ *app, _ context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
		switch candidate.Profile.ID {
		case "fr1", "nl1":
			return bestServerProbeResult{OK: true, Samples: []int{140}, Median: 140}
		default:
			return bestServerProbeResult{}
		}
	}

	a := &app{cfg: config{
		OutPath: filepath.Join(dir, "04_outbounds.json"),
		FilterPath: filepath.Join(dir, "provider.filter"),
	}}
	scan, err := a.scanAutomationEmergencyReplacement(context.Background(), automationSettings{
		Mode: automationModeBest, Policy: automationPolicyDegraded, CountryScope: automationCountryRegion,
	}, "de")
	if err != nil {
		t.Fatal(err)
	}
	if scan.Candidate.Profile.ID != "fr1" {
		t.Fatalf("winner=%q want lowest fresh VPN RTT application-ready fr1", scan.Candidate.Profile.ID)
	}
	if scan.Candidate.VPNRTTMS != 30 || scan.BestRTTMS != 30 {
		t.Fatalf("winner RTT=%d best=%d want 30", scan.Candidate.VPNRTTMS, scan.BestRTTMS)
	}
	if scan.RTTChecked != 3 || scan.RTTReachable != 3 {
		t.Fatalf("fresh RTT evidence checked=%d reachable=%d want 3/3", scan.RTTChecked, scan.RTTReachable)
	}
	rttMu.Lock()
	calls := rttCalls
	rttMu.Unlock()
	if calls != 3 {
		t.Fatalf("fresh RTT calls=%d want 3", calls)
	}
}

func TestEmergencyRecoveryDoesNotUseFullQualityPipeline(t *testing.T) {
	data, err := os.ReadFile("automation_emergency.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "func (a *app) runAutomationBestEmergencyCycleLocked")
	end := strings.Index(text[start:], "func newAutomationRecoveryIncident")
	if start < 0 || end <= 0 {
		t.Fatal("emergency recovery function not found")
	}
	body := text[start : start+end]
	for _, forbidden := range []string{
		"scanBestServerForeignForAutomation",
		"rankMeasuredBestServerBatches",
		"probeBestServerMediaQuality",
		"DownloadMbps",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("emergency recovery still depends on full quality pipeline: %s", forbidden)
		}
	}
	for _, required := range []string{
		"scanAutomationEmergencyReplacement",
		"automationEmergencyFreshRTTOrder",
		"storeAutomationEmergencySelectionSnapshot",
		"executeProviderProfileApply",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("emergency recovery missing %s", required)
		}
	}
}

func TestHealthRecoveryAcquiresSingleFlightBestFenceBeforeEndpointMutation(t *testing.T) {
	data, err := os.ReadFile("automation_health.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "func (a *app) runAutomationHealthWatch")
	if start < 0 {
		t.Fatal("health watch missing")
	}
	body := text[start:]
	bestFence := strings.Index(body, "releaseBest, bestLockErr := acquireAutomationBestLock()")
	endpoint := strings.Index(body, "a.runAutomationEndpointEmergency")
	lockedReplacement := strings.Index(body, "a.runAutomationBestEmergencyCycleLocked")
	if bestFence < 0 || endpoint < 0 || lockedReplacement < 0 {
		t.Fatal("single-flight recovery contract is incomplete")
	}
	if bestFence > endpoint || endpoint > lockedReplacement {
		t.Fatalf("recovery fence/order invalid: bestFence=%d endpoint=%d replacement=%d", bestFence, endpoint, lockedReplacement)
	}
}

func TestRecoveryIncidentContextCarriesSafeCorrelationFields(t *testing.T) {
	start := time.Date(2026, 10, 7, 6, 51, 25, 0, time.UTC)
	incident := automationRecoveryIncident{
		ID: "20261007T065125Z-deadbeef", StartedAt: start,
		Profile: "DE Berlin, Germany, Extra", Endpoint: "198.51.100.20:443", Xray: "running",
	}
	got := incident.context(start.Add(17 * time.Second))
	for _, want := range []string{
		"incident=20261007T065125Z-deadbeef",
		"elapsed=17s",
		"profile=DE Berlin, Germany, Extra",
		"endpoint=198.51.100.20:443",
		"xray=running",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("incident context missing %q: %s", want, got)
		}
	}
}
