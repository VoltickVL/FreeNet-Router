package main

import (
	"context"
	"sync"
	"testing"
)

func TestMeasureProviderProfileRTTMeasuresSharedEndpointPerLogicalProfile(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "aaaaaaaaaaaaaaaa", Name: "DE Frankfurt, Germany, Extra", CountryCode: "de", Address: "203.0.113.10", Port: 443}, Raw: "de"},
		{Profile: subscriptionProfile{ID: "bbbbbbbbbbbbbbbb", Name: "US New York, United States, Extra", CountryCode: "us", Address: "203.0.113.10", Port: 443}, Raw: "us"},
		{Profile: subscriptionProfile{ID: "cccccccccccccccc", Name: "FR Paris, France, Extra", CountryCode: "fr", Address: "203.0.113.20", Port: 443}, Raw: "fr"},
	}
	var mu sync.Mutex
	calls := map[string]int{}
	probe := func(_ context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
		mu.Lock()
		calls[candidate.Profile.ID]++
		mu.Unlock()
		switch candidate.Profile.ID {
		case "aaaaaaaaaaaaaaaa":
			return bestServerProbeResult{OK: true, Median: 82, Jitter: 4}
		case "bbbbbbbbbbbbbbbb":
			return bestServerProbeResult{OK: true, Median: 238, Jitter: 9}
		default:
			return bestServerProbeResult{OK: true, Median: 116, Jitter: 5}
		}
	}

	got := measureProviderProfileRTT(context.Background(), candidates, probe)
	for _, candidate := range candidates {
		if calls[candidate.Profile.ID] != 1 {
			t.Fatalf("logical profile %s calls=%d want=1; shared IP:port must not deduplicate proxy ping", candidate.Profile.ID, calls[candidate.Profile.ID])
		}
	}
	if len(got) != 3 {
		t.Fatalf("results=%d want=3", len(got))
	}
	if got[0].ProfileID != "aaaaaaaaaaaaaaaa" || got[0].RTTMS != 82 {
		t.Fatalf("fastest logical profile not first: %#v", got)
	}
	if got[2].ProfileID != "bbbbbbbbbbbbbbbb" || got[2].RTTMS != 238 {
		t.Fatalf("shared-ingress US profile must keep its own slower proxy RTT: %#v", got)
	}
}

func TestMeasureProviderProfileRTTPutsUnreachableLast(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "aaaaaaaaaaaaaaaa", Name: "DE Frankfurt, Germany, Extra", CountryCode: "de", Address: "203.0.113.10", Port: 443}},
		{Profile: subscriptionProfile{ID: "bbbbbbbbbbbbbbbb", Name: "FR Paris, France, Extra", CountryCode: "fr", Address: "203.0.113.20", Port: 443}},
	}
	probe := func(_ context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
		if candidate.Profile.CountryCode == "fr" {
			return bestServerProbeResult{}
		}
		return bestServerProbeResult{OK: true, Median: 180, Jitter: 4}
	}
	got := measureProviderProfileRTT(context.Background(), candidates, probe)
	if len(got) != 2 || !got[0].Reachable || got[1].Reachable {
		t.Fatalf("unreachable result must be last: %#v", got)
	}
}

func TestUkraineExcludedFromReplacementPolicy(t *testing.T) {
	if !isUserExcludedVPNCountry("UA") {
		t.Fatal("Ukraine must be excluded from user replacement pool")
	}
	if got := normalizeAutomationCountries([]string{"de", "UA", "pl"}); len(got) != 2 || got[0] != "de" || got[1] != "pl" {
		t.Fatalf("automation country normalization kept Ukraine: %#v", got)
	}
	settings := automationSettings{CountryScope: automationCountryAllowlist, Countries: []string{"de", "ua", "pl"}}
	if automationCountryAllowed(settings, "de", "ua") {
		t.Fatal("AUTO VPN must not allow Ukraine as replacement")
	}
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "aaaaaaaaaaaaaaaa", Name: "UA Kyiv, Ukraine, Extra", CountryCode: "ua", Address: "203.0.113.1", Port: 443}},
		{Profile: subscriptionProfile{ID: "bbbbbbbbbbbbbbbb", Name: "DE Berlin, Germany, Extra", CountryCode: "de", Address: "203.0.113.2", Port: 443}},
	}
	filtered := filterForeignBestServerCandidates(candidates)
	if len(filtered) != 1 || filtered[0].Profile.CountryCode != "de" {
		t.Fatalf("Best Server replacement pool kept Ukraine: %#v", filtered)
	}
	options := settingsCountryOptions([]subscriptionProfile{
		{ID: "aaaaaaaaaaaaaaaa", Name: "UA Kyiv, Ukraine, Extra", CountryCode: "ua", Address: "203.0.113.1", Port: 443},
		{ID: "bbbbbbbbbbbbbbbb", Name: "DE Berlin, Germany, Extra", CountryCode: "de", Address: "203.0.113.2", Port: 443},
	}, []string{"ua", "de"})
	for _, option := range options {
		if option.Code == "ua" {
			t.Fatalf("settings country catalog kept Ukraine: %#v", options)
		}
	}
}
