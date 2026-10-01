package main

import (
	"context"
	"sync"
	"testing"
)

func TestMeasureProviderProfileRTTDeduplicatesEndpointsAndSorts(t *testing.T) {
	profiles := []subscriptionProfile{
		{ID: "aaaaaaaaaaaaaaaa", Name: "DE Frankfurt, Germany, Extra", CountryCode: "de", Address: "203.0.113.10", Port: 443},
		{ID: "bbbbbbbbbbbbbbbb", Name: "DE Berlin, Germany, Extra", CountryCode: "de", Address: "203.0.113.10", Port: 443},
		{ID: "cccccccccccccccc", Name: "FR Paris, France, Extra", CountryCode: "fr", Address: "203.0.113.20", Port: 443},
	}
	var mu sync.Mutex
	calls := map[string]int{}
	probe := func(_ context.Context, profile subscriptionProfile) bestServerProbeResult {
		endpoint := profileEndpoint(profile)
		mu.Lock()
		calls[endpoint]++
		mu.Unlock()
		if profile.Address == "203.0.113.20" {
			return bestServerProbeResult{OK: true, Median: 45, Jitter: 3}
		}
		return bestServerProbeResult{OK: true, Median: 95, Jitter: 7}
	}

	got, unique := measureProviderProfileRTT(context.Background(), profiles, probe)
	if unique != 2 {
		t.Fatalf("unique endpoints=%d want=2", unique)
	}
	if calls["203.0.113.10:443"] != 1 || calls["203.0.113.20:443"] != 1 {
		t.Fatalf("duplicate endpoint was probed more than once: %#v", calls)
	}
	if len(got) != 3 {
		t.Fatalf("results=%d want=3", len(got))
	}
	if got[0].ProfileID != "cccccccccccccccc" || got[0].RTTMS != 45 || !got[0].Reachable {
		t.Fatalf("fastest result not first: %#v", got)
	}
	if got[1].RTTMS != 95 || got[2].RTTMS != 95 {
		t.Fatalf("shared endpoint RTT not reused: %#v", got)
	}
}

func TestMeasureProviderProfileRTTPutsUnreachableLast(t *testing.T) {
	profiles := []subscriptionProfile{
		{ID: "aaaaaaaaaaaaaaaa", Name: "DE Frankfurt, Germany, Extra", CountryCode: "de", Address: "203.0.113.10", Port: 443},
		{ID: "bbbbbbbbbbbbbbbb", Name: "FR Paris, France, Extra", CountryCode: "fr", Address: "203.0.113.20", Port: 443},
	}
	probe := func(_ context.Context, profile subscriptionProfile) bestServerProbeResult {
		if profile.CountryCode == "fr" {
			return bestServerProbeResult{}
		}
		return bestServerProbeResult{OK: true, Median: 180, Jitter: 4}
	}
	got, _ := measureProviderProfileRTT(context.Background(), profiles, probe)
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
