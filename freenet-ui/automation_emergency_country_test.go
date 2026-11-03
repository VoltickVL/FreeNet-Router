package main

import (
	"context"
	"sync"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmergencyManualCountryRequiresExactProfileAndManualOrigin(t *testing.T) {
	dir := t.TempDir()
	filter := filepath.Join(dir, "provider.filter")
	// Exact Extra label is required; a broad regex or stale country from
	// status must never create a manual country pin.
	if err := os.WriteFile(filter, []byte("DE Berlin, Germany, Extra\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manual := map[string]string{"LAST_SWITCH_ORIGIN": "manual"}
	if got := automationEmergencyManualCountry(filter, manual, "de"); got != "de" {
		t.Fatalf("confirmed manual country=%q want de", got)
	}
	for _, tc := range []struct {
		name string
		state map[string]string
		country string
		filter string
	}{
		{"automatic choice", map[string]string{"LAST_SWITCH_ORIGIN": "auto"}, "de", "DE Berlin, Germany, Extra"},
		{"missing marker", nil, "de", "DE Berlin, Germany, Extra"},
		{"country drift", manual, "fr", "DE Berlin, Germany, Extra"},
		{"unknown country", manual, "", "DE Berlin, Germany, Extra"},
		{"broad filter", manual, "de", ".*Germany.*"},
		{"non Extra profile", manual, "de", "Berlin, Germany"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filter, []byte(tc.filter+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if got := automationEmergencyManualCountry(filter, tc.state, tc.country); got != "" {
				t.Fatalf("unverified manual country %q", got)
			}
		})
	}
}

func TestEmergencyManualCountryCohortReservesCrossCountryFallback(t *testing.T) {
	candidates := make([]bestServerInternalCandidate, 0, 16)
	for i := 0; i < 12; i++ {
		candidates = append(candidates, emergencyTestCandidate("de"+string(rune('a'+i)), "de"))
	}
	for _, country := range []string{"fr", "nl", "ch", "gb"} {
		candidates = append(candidates, emergencyTestCandidate(country+"1", country))
	}
	got := automationEmergencyManualCountryCohort(candidates, "de", automationEmergencyCandidateLimit)
	if len(got) != automationEmergencyCandidateLimit {
		t.Fatalf("cohort length=%d want %d", len(got), automationEmergencyCandidateLimit)
	}
	same, other := 0, 0
	for _, candidate := range got {
		if candidate.Profile.CountryCode == "de" {
			same++
		} else {
			other++
		}
	}
	if same != 4 || other != 4 {
		t.Fatalf("manual-country and fallback slots=%d/%d want 4/4", same, other)
	}
	if got := automationEmergencyManualCountryCohort(candidates[:12], "de", 8); len(got) != 8 {
		t.Fatalf("same-country-only cohort=%d want 8", len(got))
	}
	if got := automationEmergencyManualCountryCohort(candidates, "de", 0); got != nil {
		t.Fatalf("zero budget unexpectedly returned candidates")
	}
}

func TestEmergencyManualCountryTierRanksWithinTier(t *testing.T) {
	input := []bestServerInternalCandidate{
		emergencyTestCandidate("fr-fast", "fr"),
		emergencyTestCandidate("de-fast", "de"),
		emergencyTestCandidate("nl-fast", "nl"),
		emergencyTestCandidate("de-slow", "de"),
	}
	got := automationEmergencyCountryFirst(input, "de")
	ids := make([]string, 0, len(got))
	for _, c := range got {
		ids = append(ids, c.Profile.ID)
	}
	if strings.Join(ids, ",") != "de-fast,de-slow,fr-fast,nl-fast" {
		t.Fatalf("manual tier order=%v", ids)
	}
	if len(input) != 4 || input[0].Profile.ID != "fr-fast" {
		t.Fatal("tiering mutated candidate input")
	}
}

// End-to-end read-only candidate scan: a faster foreign node cannot displace
// an application-ready country peer, but it may recover the VPN if peers fail.
func TestEmergencyManualCountryScanFallsBackAfterHTTPSFailure(t *testing.T) {
	resetEmergencyRTTCache(t)
	dir := t.TempDir()
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(dir, "settings.state"))
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "auto.state"))
	if err := os.WriteFile(automationStatePath(), []byte("LAST_SWITCH_ORIGIN=manual\n"), 0600); err != nil {
		t.Fatal(err)
	}
	filter := filepath.Join(dir, "provider.filter")
	if err := os.WriteFile(filter, []byte("DE Berlin, Germany, Extra\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{OutPath: filepath.Join(dir, "outbounds.json"), FilterPath: filter}}
	candidates := []bestServerInternalCandidate{
		emergencyTestCandidate("de1", "de"),
		emergencyTestCandidate("fr1", "fr"),
		emergencyTestCandidate("de2", "de"),
	}
	oldDiscover, oldRTT, oldHTTPS := automationEmergencyDiscoverCandidates, automationEmergencyRTTProbe, automationEmergencyCandidateProbe
	t.Cleanup(func() {
		automationEmergencyDiscoverCandidates, automationEmergencyRTTProbe, automationEmergencyCandidateProbe = oldDiscover, oldRTT, oldHTTPS
	})
	automationEmergencyDiscoverCandidates = func(*app, context.Context) ([]bestServerInternalCandidate, bool, error) {
		return candidates, false, nil
	}
	automationEmergencyRTTProbe = func(_ *app, _ context.Context, c bestServerInternalCandidate) bestServerProbeResult {
		rtt := 120
		if c.Profile.CountryCode == "fr" {
			rtt = 20
		}
		return bestServerProbeResult{OK: true, Samples: []int{rtt}, Median: rtt}
	}
	allowDE := true
	automationEmergencyCandidateProbe = func(_ *app, _ context.Context, c bestServerInternalCandidate) bestServerProbeResult {
		if c.Profile.CountryCode == "de" && !allowDE {
			return bestServerProbeResult{}
		}
		return bestServerProbeResult{OK: true, Samples: []int{100}, Median: 100}
	}
	settings := automationSettings{Mode: automationModeBest, CountryScope: automationCountryRegion}
	first, err := a.scanAutomationEmergencyReplacement(context.Background(), settings, "de")
	if err != nil || first.Candidate.Profile.CountryCode != "de" {
		t.Fatalf("manual country must win before faster foreign HTTPS: %+v, %v", first, err)
	}
	allowDE = false
	second, err := a.scanAutomationEmergencyReplacement(context.Background(), settings, "de")
	if err != nil || second.Candidate.Profile.ID != "fr1" {
		t.Fatalf("foreign HTTPS fallback must recover after country peer failure: %+v, %v", second, err)
	}
}

func TestEmergencyManualCountryFailureRotatesBoundedCohort(t *testing.T) {
	resetEmergencyRTTCache(t)
	dir := t.TempDir()
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(dir, "settings.state"))
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "auto.state"))
	if err := os.WriteFile(automationStatePath(), []byte("LAST_SWITCH_ORIGIN=manual\n"), 0600); err != nil {
		t.Fatal(err)
	}
	filter := filepath.Join(dir, "filter")
	if err := os.WriteFile(filter, []byte("DE Berlin, Germany, Extra\n"), 0600); err != nil {
		t.Fatal(err)
	}
	candidates := make([]bestServerInternalCandidate, 0, 16)
	for i := 0; i < 12; i++ {
		candidates = append(candidates, emergencyTestCandidate("de"+string(rune('a'+i)), "de"))
	}
	for _, country := range []string{"fr", "nl", "ch", "gb"} {
		candidates = append(candidates, emergencyTestCandidate(country+"1", country))
	}
	oldDiscover, oldRTT, oldHTTPS := automationEmergencyDiscoverCandidates, automationEmergencyRTTProbe, automationEmergencyCandidateProbe
	t.Cleanup(func() {
		automationEmergencyDiscoverCandidates, automationEmergencyRTTProbe, automationEmergencyCandidateProbe = oldDiscover, oldRTT, oldHTTPS
	})
	automationEmergencyDiscoverCandidates = func(*app, context.Context) ([]bestServerInternalCandidate, bool, error) {
		return candidates, false, nil
	}
	automationEmergencyRTTProbe = func(_ *app, _ context.Context, c bestServerInternalCandidate) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Samples: []int{100}, Median: 100}
	}
	seen := map[string]bool{}
	var mu sync.Mutex
	automationEmergencyCandidateProbe = func(_ *app, _ context.Context, c bestServerInternalCandidate) bestServerProbeResult {
		mu.Lock()
		seen[c.Profile.ID] = true
		mu.Unlock()
		return bestServerProbeResult{}
	}
	a := &app{cfg: config{OutPath: filepath.Join(dir, "outbounds.json"), FilterPath: filter}}
	settings := automationSettings{Mode: automationModeBest, CountryScope: automationCountryRegion}
	for round := 0; round < 2; round++ {
		scan, err := a.scanAutomationEmergencyReplacement(context.Background(), settings, "de")
		if err != nil || scan.Candidate.Profile.ID != "" || scan.Checked > automationEmergencyCandidateLimit {
			t.Fatalf("no HTTPS accepted in round %d: %+v / %v", round, scan, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) <= automationEmergencyCandidateLimit {
		t.Fatalf("manual country recovery repeated same bounded cohort: distinct=%d", len(seen))
	}
}

// The existing emergency candidate probe still decides acceptance: mere
// same-country membership or RTT must not be equated with working HTTPS.
func TestEmergencyManualCountryNeverBypassesHTTPSGate(t *testing.T) {
	ordered := automationEmergencyCountryFirst([]bestServerInternalCandidate{
		emergencyTestCandidate("fr-ready", "fr"),
		emergencyTestCandidate("de-unready", "de"),
	}, "de")
	if ordered[0].Profile.ID != "de-unready" || ordered[1].Profile.ID != "fr-ready" {
		t.Fatalf("wrong candidate probe priority %+v", ordered)
	}
	// The production scan only accepts automationEmergencyCandidateProbe.OK.
	// Preserve the canonical gates and transactional writer in the file.
	data, err := os.ReadFile("automation_emergency.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, required := range []string{
		"automationEmergencyFreshRTTOrder(scanCtx, a, cohort)",
		"automationEmergencyCandidateProbe(a, probeCtx, batch[i])",
		"if !probe.OK {",
		"storeAutomationEmergencySelectionSnapshot(scan)",
		"executeProviderProfileApply(networkApplyRequest{",
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("lost emergency HTTPS/transaction admission %q", required)
		}
	}
}
