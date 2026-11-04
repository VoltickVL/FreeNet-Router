package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEmergencyQuarantineRequiresConfirmedAppFailure(t *testing.T) {
	now := time.Date(2026, 11, 4, 2, 0, 0, 0, time.UTC)
	candidate := emergencyTestCandidate("de1", "de")
	var memory automationEmergencyFailureMemory
	for _, result := range []bestServerProbeResult{
		{}, {TransportOnly: true}, {ConfirmedApplicationFailure: false},
	} {
		memory.observe(candidate, result, now)
		if memory.active(candidate, now) {
			t.Fatal("UNKNOWN, RTT or transport-only evidence must not quarantine an endpoint")
		}
	}
	memory.observe(candidate, bestServerProbeResult{ConfirmedApplicationFailure: true}, now)
	if !memory.active(candidate, now) {
		t.Fatal("two completed application failures should hold a candidate briefly")
	}
	memory.observe(candidate, bestServerProbeResult{OK: true}, now)
	if memory.active(candidate, now) {
		t.Fatal("fresh successful application check must clear quarantine")
	}
}

func TestEmergencyQuarantineTTLAndRawIdentity(t *testing.T) {
	now := time.Date(2026, 11, 4, 2, 0, 0, 0, time.UTC)
	candidate := emergencyTestCandidate("de1", "de")
	differentRaw := candidate
	differentRaw.Raw = "revised-credential-" + candidate.Raw
	sameIngress := emergencyTestCandidate("de2", "de")
	var memory automationEmergencyFailureMemory
	memory.observe(candidate, bestServerProbeResult{ConfirmedApplicationFailure: true}, now)
	if !memory.active(candidate, now.Add(automationEmergencyFailureTTL-time.Nanosecond)) {
		t.Fatal("quarantine expired early")
	}
	if memory.active(candidate, now.Add(automationEmergencyFailureTTL)) {
		t.Fatal("quarantine must expire exactly at TTL boundary")
	}
	memory.observe(candidate, bestServerProbeResult{ConfirmedApplicationFailure: true}, now)
	if memory.active(differentRaw, now) || memory.active(sameIngress, now) {
		t.Fatal("same profile label or shared ingress must not condemn another raw VLESS identity")
	}
	var empty automationEmergencyFailureMemory
	empty.observe(bestServerInternalCandidate{}, bestServerProbeResult{ConfirmedApplicationFailure: true}, now)
	if empty.active(bestServerInternalCandidate{}, now) {
		t.Fatal("unknown raw must not be classified as a confirmed failed node")
	}
}

func TestEmergencyQuarantineCohortPrefersFreshAndKeepsFallback(t *testing.T) {
	now := time.Date(2026, 11, 4, 2, 0, 0, 0, time.UTC)
	bad := emergencyTestCandidate("de1", "de")
	good := emergencyTestCandidate("fr1", "fr")
	other := emergencyTestCandidate("nl1", "nl")
	var memory automationEmergencyFailureMemory
	memory.observe(bad, bestServerProbeResult{ConfirmedApplicationFailure: true}, now)
	cohort, cooling, fallback := memory.cohort([]bestServerInternalCandidate{bad, good, other}, "", 2, now)
	if len(cohort) != 2 || cohort[0].Profile.ID != "fr1" || cohort[1].Profile.ID != "nl1" || cooling != 1 || fallback != 0 {
		t.Fatalf("fresh endpoints should consume budget before failed one: ids=%v cooling=%d fallback=%d", []string{cohort[0].Profile.ID, cohort[1].Profile.ID}, cooling, fallback)
	}
	cohort, cooling, fallback = memory.cohort([]bestServerInternalCandidate{bad, good}, "", 2, now)
	if len(cohort) != 2 || cohort[0].Profile.ID != "fr1" || cohort[1].Profile.ID != "de1" || cooling != 1 || fallback != 1 {
		t.Fatalf("fallback must remain available when too few fresh choices: %+v %d/%d", cohort, cooling, fallback)
	}
	memory.observe(good, bestServerProbeResult{ConfirmedApplicationFailure: true}, now)
	cohort, cooling, fallback = memory.cohort([]bestServerInternalCandidate{bad, good}, "", 2, now)
	if len(cohort) != 2 || cooling != 2 || fallback != 2 {
		t.Fatalf("all quarantined candidates must still be retried through full admission: n=%d cooling=%d fallback=%d", len(cohort), cooling, fallback)
	}
}

func TestEmergencyQuarantineCohortPreservesManualCountryAndBudget(t *testing.T) {
	now := time.Date(2026, 11, 4, 2, 0, 0, 0, time.UTC)
	var memory automationEmergencyFailureMemory
	items := []bestServerInternalCandidate{
		emergencyTestCandidate("de1", "de"),
		emergencyTestCandidate("de2", "de"),
		emergencyTestCandidate("de3", "de"),
		emergencyTestCandidate("fr1", "fr"),
		emergencyTestCandidate("nl1", "nl"),
	}
	memory.observe(items[0], bestServerProbeResult{ConfirmedApplicationFailure: true}, now)
	got, _, fallback := memory.cohort(items, "de", 3, now)
	if len(got) != 3 || got[0].Profile.ID != "de2" || got[1].Profile.ID != "fr1" || got[2].Profile.ID != "nl1" || fallback != 0 {
		t.Fatalf("manual preferred country + independent fallback must survive quarantine: %+v", got)
	}
	got, _, fallback = memory.cohort(items, "de", 2, now)
	if len(got) != 2 || got[0].Profile.ID != "de2" || got[1].Profile.ID != "fr1" || fallback != 0 {
		t.Fatalf("bounded manual country cohort: %+v", got)
	}
}

func TestEmergencyDiagnosticFailureEvidenceRequiresCompleteTwoRounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		rounds int
		result bestServerProbeResult
		err    error
		want   bool
	}{
		{"two named failures", 2, bestServerProbeResult{}, nil, true},
		{"only one completed", 1, bestServerProbeResult{}, nil, false},
		{"one named success", 2, bestServerProbeResult{OK: true}, nil, false},
		{"context deadline", 2, bestServerProbeResult{}, context.DeadlineExceeded, false},
		{"canceled", 2, bestServerProbeResult{}, context.Canceled, false},
		{"unknown", 2, bestServerProbeResult{}, errors.New("incomplete"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bestServerConfirmedDiagnosticFailure(tc.rounds, tc.result, tc.err); got != tc.want {
				t.Fatalf("confirmed=%v, want %v", got, tc.want)
			}
		})
	}
}

func TestEmergencySelectionSummaryDoesNotClaimZeroPing(t *testing.T) {
	scan := automationEmergencyScanResult{Candidate: emergencyTestCandidate("it1", "it"), ApplicationMS: 210}
	summary := automationEmergencySelectionSummary(scan)
	if strings.Contains(summary, "VPN 0 мс") || !strings.Contains(summary, "VPN не измерен") {
		t.Fatalf("unknown RTT must not appear as measured zero: %q", summary)
	}
	scan.Candidate.VPNRTTMS = 78
	summary = automationEmergencySelectionSummary(scan)
	if !strings.Contains(summary, "VPN 78 мс") {
		t.Fatalf("measured RTT must remain visible: %q", summary)
	}
}

func TestEmergencyQuarantineIntegrationDoesNotSpendHTTPSOnRecentFailures(t *testing.T) {
	resetEmergencyRTTCache(t)
	dir := t.TempDir()
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(dir, "settings.state"))

	oldDiscover := automationEmergencyDiscoverCandidates
	oldProbe := automationEmergencyCandidateProbe
	oldRTT := automationEmergencyRTTProbe
	automationEmergencyRecentFailures.Lock()
	oldEntries := automationEmergencyRecentFailures.until
	automationEmergencyRecentFailures.until = nil
	automationEmergencyRecentFailures.Unlock()
	t.Cleanup(func() {
		automationEmergencyDiscoverCandidates = oldDiscover
		automationEmergencyCandidateProbe = oldProbe
		automationEmergencyRTTProbe = oldRTT
		automationEmergencyRecentFailures.Lock()
		automationEmergencyRecentFailures.until = oldEntries
		automationEmergencyRecentFailures.Unlock()
	})

	bad1 := emergencyTestCandidate("bad1", "de")
	bad2 := emergencyTestCandidate("bad2", "fr")
	good := emergencyTestCandidate("good", "nl")
	round := 0
	automationEmergencyDiscoverCandidates = func(*app, context.Context) ([]bestServerInternalCandidate, bool, error) {
		round++
		if round == 1 || round == 3 {
			return []bestServerInternalCandidate{bad1, bad2}, false, nil
		}
		return []bestServerInternalCandidate{bad1, bad2, good}, false, nil
	}
	automationEmergencyRTTProbe = func(_ *app, _ context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
		ms := 15
		if candidate.Profile.ID == "good" {
			ms = 300
		}
		return bestServerProbeResult{OK: true, Median: ms, Samples: []int{ms}}
	}
	var probes []string
	var mu sync.Mutex
	automationEmergencyCandidateProbe = func(_ *app, _ context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
		mu.Lock()
		probes = append(probes, candidate.Profile.ID)
		mu.Unlock()
		if candidate.Profile.ID == "good" {
			return bestServerProbeResult{OK: true, Median: 350, Samples: []int{350}}
		}
		return bestServerProbeResult{ConfirmedApplicationFailure: true}
	}
	a := &app{cfg: config{
		OutPath: filepath.Join(dir, "04_outbounds.json"),
		FilterPath: filepath.Join(dir, "provider.filter"),
	}}
	settings := automationSettings{Mode: automationModeBest, Policy: automationPolicyDegraded, CountryScope: automationCountryRegion}

	first, err := a.scanAutomationEmergencyReplacement(context.Background(), settings, "se")
	if err != nil || first.Checked != 2 || first.Candidate.Profile.ID != "" {
		t.Fatalf("first failure cohort: %+v %v", first, err)
	}
	mu.Lock()
	probes = nil
	mu.Unlock()
	second, err := a.scanAutomationEmergencyReplacement(context.Background(), settings, "se")
	if err != nil || second.Candidate.Profile.ID != "good" || second.Checked != 1 || second.Quarantined != 2 {
		t.Fatalf("fresh candidate must win without retrying failed application exits: %+v %v", second, err)
	}
	mu.Lock()
	gotProbes := append([]string(nil), probes...)
	probes = nil
	mu.Unlock()
	if len(gotProbes) != 1 || gotProbes[0] != "good" {
		t.Fatalf("unexpected repeated failed exit in fresh search: %v", gotProbes)
	}
	third, err := a.scanAutomationEmergencyReplacement(context.Background(), settings, "se")
	if err != nil || third.Checked != 2 || third.Candidate.Profile.ID != "" || third.Quarantined != 2 {
		t.Fatalf("quarantine alone must not prevent recovery attempt when all alternatives were bad: %+v %v", third, err)
	}
}
