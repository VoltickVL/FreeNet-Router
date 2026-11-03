package main

import (
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
	if err := os.WriteFile(filter, []byte("Berlin, Germany, Extra\n"), 0600); err != nil {
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
		{"automatic choice", map[string]string{"LAST_SWITCH_ORIGIN": "auto"}, "de", "Berlin, Germany, Extra"},
		{"missing marker", nil, "de", "Berlin, Germany, Extra"},
		{"country drift", manual, "fr", "Berlin, Germany, Extra"},
		{"unknown country", manual, "", "Berlin, Germany, Extra"},
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
