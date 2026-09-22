package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func cascadeProfiles(n int) []bestServerInternalCandidate {
	out := make([]bestServerInternalCandidate, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, bestServerInternalCandidate{Profile: subscriptionProfile{
			ID: fmt.Sprintf("profile-%02d", i),
			Name: fmt.Sprintf("City %02d, Country %02d, Extra", i, i),
			CountryCode: fmt.Sprintf("%c%c", 'a'+rune((i/26)%26), 'a'+rune(i%26)),
			Address: fmt.Sprintf("198.51.100.%d", i+1),
			Port: 443,
		}})
	}
	return out
}

func profileOrdinal(profile subscriptionProfile) int {
	parts := strings.Split(profile.ID, "-")
	if len(parts) != 2 {
		return 999
	}
	n, _ := strconv.Atoi(parts[1])
	return n
}

func TestBestServerExpressScansAll49Profiles(t *testing.T) {
	previous := bestServerExpressProbe
	defer func() { bestServerExpressProbe = previous }()

	var mu sync.Mutex
	calls := map[string]int{}
	bestServerExpressProbe = func(_ context.Context, profile subscriptionProfile) bestServerProbeResult {
		mu.Lock()
		calls[profile.ID]++
		mu.Unlock()
		n := profileOrdinal(profile)
		return bestServerProbeResult{OK: true, Samples: []int{30 + n, 31 + n}, Median: 30 + n, Jitter: 1}
	}

	profiles := cascadeProfiles(49)
	evidence, measured := expressBestServerPool(context.Background(), profiles)
	if measured != 49 || len(evidence) != 49 {
		t.Fatalf("express measured=%d evidence=%d want=49", measured, len(evidence))
	}
	for _, profile := range profiles {
		if calls[profile.Profile.ID] != 1 {
			t.Fatalf("%s express calls=%d want=1", profile.Profile.ID, calls[profile.Profile.ID])
		}
	}
}

func TestBestServerExpressShortlistCanPromoteLateStrongCandidate(t *testing.T) {
	profiles := cascadeProfiles(49)
	evidence := make([]bestServerExpressEvidence, len(profiles))
	for i := range profiles {
		ms := 100 + i
		if i == 48 {
			ms = 5
		}
		evidence[i] = bestServerExpressEvidence{Probe: bestServerProbeResult{OK: true, Median: ms, Jitter: 2}}
	}
	shortlist := bestServerExpressShortlist(profiles, evidence)
	if len(shortlist) != bestServerCascadeShortlist {
		t.Fatalf("shortlist=%d want=%d", len(shortlist), bestServerCascadeShortlist)
	}
	found := false
	for _, candidate := range shortlist {
		if candidate.Profile.ID == "profile-48" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("strong candidate at the end of the subscription order was excluded")
	}
}

func TestBestServerCascadeReturnsQuickTop3WithoutStrictAcceptance(t *testing.T) {
	oldExpress, oldQuick := bestServerExpressProbe, bestServerQuickProbe
	defer func() {
		bestServerExpressProbe = oldExpress
		bestServerQuickProbe = oldQuick
	}()
	bestServerExpressProbe = func(_ context.Context, profile subscriptionProfile) bestServerProbeResult {
		n := profileOrdinal(profile)
		return bestServerProbeResult{OK: true, Samples: []int{20 + n, 21 + n}, Median: 20 + n, Jitter: 1}
	}
	bestServerQuickProbe = func(_ *app, _ context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
		n := profileOrdinal(candidate.Profile)
		return bestServerProbeResult{OK: true, Samples: []int{60 + n, 61 + n}, Median: 60 + n, Jitter: 1}
	}

	profiles := cascadeProfiles(49)
	a := &app{}
	outcome := a.scanBestServerCascade(context.Background(), profiles, 49, false, "", "")
	response := outcome.Response
	if response.ProfilesTotal != 49 || response.ExpressMeasured != 49 || response.ProfilesScanned != 49 {
		t.Fatalf("pool/express telemetry wrong: %+v", response)
	}
	if response.QuickMeasured != bestServerCascadeShortlist || response.StrictTested != 0 {
		t.Fatalf("quick/strict telemetry wrong: quick=%d strict=%d", response.QuickMeasured, response.StrictTested)
	}
	if len(response.Candidates) != bestServerCascadeVisible {
		t.Fatalf("visible candidates=%d want=%d", len(response.Candidates), bestServerCascadeVisible)
	}
	if response.Recommendation == nil || !response.Available {
		t.Fatal("healthy quick cascade should produce a ranking recommendation")
	}
	for _, candidate := range response.Candidates {
		if candidate.Validation != "quick" {
			t.Fatalf("candidate validation=%q want quick", candidate.Validation)
		}
		if candidate.Eligible {
			t.Fatal("quick evidence must never be mutation-eligible")
		}
		if candidate.MediaSamples != 0 || candidate.ServiceTotal != 0 || candidate.DownloadMbps != 0 {
			t.Fatal("quick discovery must not fabricate strict Speedtest/service evidence")
		}
	}
}

func TestBestServerFreshEndpointRetryIsSingleBoundedCatalogRead(t *testing.T) {
	oldExpress, oldQuick, oldFresh := bestServerExpressProbe, bestServerQuickProbe, bestServerFreshCatalog
	defer func() {
		bestServerExpressProbe = oldExpress
		bestServerQuickProbe = oldQuick
		bestServerFreshCatalog = oldFresh
	}()

	initial := []bestServerInternalCandidate{{Profile: subscriptionProfile{
		ID: "de-old", Name: "Frankfurt, Germany, Extra", CountryCode: "de", Address: "198.51.100.10", Port: 443,
	}}}
	quick := []bestServerQualityCandidate{{
		Tested: true, Validation: "quick", ID: "de-old", Name: "Frankfurt, Germany, Extra", CountryCode: "de",
		Endpoint: "198.51.100.10:443", Reachable: true, Available: true, ApplicationMS: 240,
	}}
	bestServerExpressProbe = func(_ context.Context, profile subscriptionProfile) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Samples: []int{30, 31}, Median: 30, Jitter: 1}
	}
	bestServerQuickProbe = func(_ *app, _ context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
		if candidate.Profile.Address == "198.51.100.11" {
			return bestServerProbeResult{OK: true, Samples: []int{80, 82}, Median: 81, Jitter: 2}
		}
		return bestServerProbeResult{OK: true, Samples: []int{240, 245}, Median: 242, Jitter: 5}
	}
	freshReads := 0
	bestServerFreshCatalog = func(_ *app, _ context.Context) ([]bestServerInternalCandidate, int, bool, error) {
		freshReads++
		return []bestServerInternalCandidate{{Profile: subscriptionProfile{
			ID: "de-fresh", Name: "Frankfurt, Germany, Extra", CountryCode: "de", Address: "198.51.100.11", Port: 443,
		}}}, 1, false, nil
	}

	a := &app{}
	retries, updated := a.retryBestServerFreshEndpoints(context.Background(), initial, quick)
	if freshReads != 1 {
		t.Fatalf("fresh catalog reads=%d want=1", freshReads)
	}
	if len(retries) != 1 || len(updated) != 1 {
		t.Fatalf("retry results retries=%d updated=%d", len(retries), len(updated))
	}
	if updated[0].Endpoint != "198.51.100.11:443" || !updated[0].FreshEndpointRetry || updated[0].ApplicationMS != 81 {
		t.Fatalf("fresh endpoint not promoted: %+v", updated[0])
	}
}
