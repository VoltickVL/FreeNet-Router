package main

import "testing"

func TestBestServerProfileFromEndpoint(t *testing.T) {
	profile, ok := bestServerProfileFromEndpoint("143.20.254.191:443")
	if !ok || profile.Address != "143.20.254.191" || profile.Port != 443 {
		t.Fatalf("unexpected endpoint parse: ok=%v profile=%+v", ok, profile)
	}
	if _, ok := bestServerProfileFromEndpoint("broken"); ok {
		t.Fatal("invalid endpoint must fail closed")
	}
}

func TestRemainingUntestedBestServerCandidates(t *testing.T) {
	internal := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "a"}},
		{Profile: subscriptionProfile{ID: "b"}},
		{Profile: subscriptionProfile{ID: "c"}},
	}
	results := []bestServerQualityCandidate{
		{ID: "a", Tested: true},
		{ID: "b", Tested: false},
	}
	remaining := remainingUntestedBestServerCandidates(internal, results)
	if len(remaining) != 2 || remaining[0].Profile.ID != "b" || remaining[1].Profile.ID != "c" {
		t.Fatalf("unexpected remaining candidates: %+v", remaining)
	}
}

func TestFinalizeMeasuredBestServerResponsePrefersEligibleMeasured(t *testing.T) {
	response := bestServerQualityResponse{Candidates: []bestServerQualityCandidate{
		{ID: "slow", Tested: true, DownloadMbps: 90, MediaSamples: bestServerMediaRequiredRuns, Eligible: true, Score: 100},
		{ID: "fast", Tested: true, DownloadMbps: 120, MediaSamples: bestServerMediaRequiredRuns, Eligible: true, Score: 200},
		{ID: "missing", Tested: true, DownloadMbps: 0, MediaSamples: 0, Eligible: false},
	}}
	final := finalizeMeasuredBestServerResponse(response)
	if len(final.Candidates) != 2 {
		t.Fatalf("expected only measured candidates, got %+v", final.Candidates)
	}
	if !final.Available || final.Recommendation == nil || final.Recommendation.ID != "fast" {
		t.Fatalf("unexpected recommendation: %+v", final.Recommendation)
	}
}
