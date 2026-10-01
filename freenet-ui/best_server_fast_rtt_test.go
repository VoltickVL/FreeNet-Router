package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

func fastRTTCandidate(id, country, address string) bestServerInternalCandidate {
	return bestServerInternalCandidate{Profile: subscriptionProfile{
		ID: id, Name: id + ", Extra", CountryCode: country, Address: address, Port: 443,
	}}
}

func TestBestServerFastRTTDeduplicatesEndpointsAndReportsProgress(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		fastRTTCandidate("de-a", "de", "203.0.113.10"),
		fastRTTCandidate("de-b", "de", "203.0.113.10"),
		fastRTTCandidate("pl-a", "pl", "203.0.113.20"),
		fastRTTCandidate("fr-a", "fr", "203.0.113.30"),
	}
	var mu sync.Mutex
	calls := map[string]int{}
	progressStage := ""
	progressCompleted := 0
	progressTotal := 0
	ctx := context.WithValue(context.Background(), bestServerProgressKey{}, func(stage string, completed, total int) {
		mu.Lock()
		defer mu.Unlock()
		if stage == "fast_rtt" {
			progressStage = stage
			if completed >= progressCompleted {
				progressCompleted = completed
			}
			progressTotal = total
		}
	})
	probe := func(_ context.Context, profile subscriptionProfile) bestServerProbeResult {
		endpoint := profileEndpoint(profile)
		mu.Lock()
		calls[endpoint]++
		mu.Unlock()
		return bestServerProbeResult{OK: true, Median: 80, Jitter: 4}
	}

	got := bestServerFastRTTShortlist(ctx, candidates, "", "", probe)
	if len(got) != len(candidates) {
		t.Fatalf("shortlist len=%d want=%d", len(got), len(candidates))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 3 {
		t.Fatalf("unique endpoint probes=%d want=3: %#v", len(calls), calls)
	}
	for endpoint, count := range calls {
		if count != 1 {
			t.Fatalf("endpoint %s measured %d times; want exactly once", endpoint, count)
		}
	}
	if progressStage != "fast_rtt" || progressCompleted != 3 || progressTotal != 3 {
		t.Fatalf("progress=%s %d/%d want fast_rtt 3/3", progressStage, progressCompleted, progressTotal)
	}
}

func TestSelectBestServerFastRTTPrioritizesUnder200AndKeepsCountryDiversity(t *testing.T) {
	candidates := make([]bestServerInternalCandidate, 0, 25)
	measured := map[string]bestServerProbeResult{}
	attempted := map[string]bool{}
	for i := 0; i < 22; i++ {
		c := fastRTTCandidate(fmt.Sprintf("de-%02d", i), "de", fmt.Sprintf("203.0.113.%d", i+1))
		candidates = append(candidates, c)
		endpoint := profileEndpoint(c.Profile)
		measured[endpoint] = bestServerProbeResult{OK: true, Median: 60 + i, Jitter: 3}
		attempted[endpoint] = true
	}
	for _, tc := range []struct {
		id, country, address string
		rtt                  int
	}{
		{"pl-a", "pl", "198.51.100.10", 245},
		{"fr-a", "fr", "198.51.100.20", 275},
		{"be-slow", "be", "198.51.100.30", 360},
	} {
		c := fastRTTCandidate(tc.id, tc.country, tc.address)
		candidates = append(candidates, c)
		endpoint := profileEndpoint(c.Profile)
		measured[endpoint] = bestServerProbeResult{OK: true, Median: tc.rtt, Jitter: 7}
		attempted[endpoint] = true
	}

	indexes := selectBestServerFastRTTIndexes(candidates, measured, attempted, -1)
	if len(indexes) != bestServerFastRTTShortlistLimit {
		t.Fatalf("selected=%d want=%d", len(indexes), bestServerFastRTTShortlistLimit)
	}
	seenCountry := map[string]bool{}
	seenID := map[string]bool{}
	for _, index := range indexes {
		p := candidates[index].Profile
		seenCountry[p.CountryCode] = true
		seenID[p.ID] = true
	}
	if !seenCountry["pl"] || !seenCountry["fr"] {
		t.Fatalf("<=300 ms country diversity lost: countries=%#v", seenCountry)
	}
	if seenID["be-slow"] {
		t.Fatal(">300 ms candidate displaced enough <=300 ms alternatives")
	}
}

func TestSelectBestServerFastRTTUnknownRanksBeforeConfirmedSlowOrFailed(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		fastRTTCandidate("fast", "de", "203.0.113.1"),
		fastRTTCandidate("unknown", "pl", "203.0.113.2"),
		fastRTTCandidate("slow", "fr", "203.0.113.3"),
		fastRTTCandidate("failed", "be", "203.0.113.4"),
	}
	measured := map[string]bestServerProbeResult{
		profileEndpoint(candidates[0].Profile): {OK: true, Median: 90, Jitter: 2},
		profileEndpoint(candidates[2].Profile): {OK: true, Median: 420, Jitter: 12},
		profileEndpoint(candidates[3].Profile): {OK: false},
	}
	attempted := map[string]bool{
		profileEndpoint(candidates[0].Profile): true,
		profileEndpoint(candidates[2].Profile): true,
		profileEndpoint(candidates[3].Profile): true,
	}

	indexes := selectBestServerFastRTTIndexes(candidates, measured, attempted, -1)
	position := map[string]int{}
	for i, index := range indexes {
		position[candidates[index].Profile.ID] = i
	}
	if position["unknown"] >= position["slow"] {
		t.Fatalf("UNKNOWN must rank ahead of confirmed slow: %#v", position)
	}
	if position["slow"] >= position["failed"] {
		t.Fatalf("confirmed slow must rank ahead of explicit TCP failure: %#v", position)
	}
}

func TestSelectBestServerFastRTTPreservesCurrentCandidate(t *testing.T) {
	candidates := make([]bestServerInternalCandidate, 0, 25)
	measured := map[string]bestServerProbeResult{}
	attempted := map[string]bool{}
	for i := 0; i < 25; i++ {
		c := fastRTTCandidate(fmt.Sprintf("p-%02d", i), "de", fmt.Sprintf("192.0.2.%d", i+1))
		candidates = append(candidates, c)
		endpoint := profileEndpoint(c.Profile)
		measured[endpoint] = bestServerProbeResult{OK: true, Median: 50 + i, Jitter: 1}
		attempted[endpoint] = true
	}
	currentIndex := 24
	indexes := selectBestServerFastRTTIndexes(candidates, measured, attempted, currentIndex)
	found := false
	for _, index := range indexes {
		if index == currentIndex {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("generic Fast RTT shortlist dropped the current candidate")
	}
}


func TestRawIngressFastRTTIsNotUsedForLogicalProfileSelection(t *testing.T) {
	for _, path := range []string{"best_server_ux.go", "automation_v2.go"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		if strings.Contains(src, "bestServerFastRTTShortlist(ctx") {
			t.Fatalf("%s must not shortlist logical VPN profiles by shared ingress TCP RTT", path)
		}
		if !strings.Contains(src, "applicationAwareBestServerShortlist") {
			t.Fatalf("%s must retain real per-profile proxy-path preflight", path)
		}
	}
}
