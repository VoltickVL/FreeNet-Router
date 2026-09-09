package main

import "testing"

func TestMeasuredBestServerForeignCount(t *testing.T) {
	candidates := []bestServerQualityCandidate{
		{ID: "a", Tested: true, DownloadMbps: 120, MediaSamples: bestServerMediaRequiredRuns},
		{ID: "b", Tested: true, DownloadMbps: 90, MediaSamples: bestServerMediaRequiredRuns},
		{ID: "dash", Tested: true},
		{ID: "current", Current: true, Tested: true, DownloadMbps: 140, MediaSamples: bestServerMediaRequiredRuns},
	}
	if got := measuredBestServerForeignCount(candidates); got != 2 {
		t.Fatalf("expected two measured foreign candidates, got %d", got)
	}
}

func TestRemainingBestServerCandidatesExcludesFirstBatch(t *testing.T) {
	all := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "a"}},
		{Profile: subscriptionProfile{ID: "b"}},
		{Profile: subscriptionProfile{ID: "c"}},
	}
	selected := []bestServerInternalCandidate{{Profile: subscriptionProfile{ID: "a"}}, {Profile: subscriptionProfile{ID: "c"}}}
	remaining := remainingBestServerCandidates(all, selected)
	if len(remaining) != 1 || remaining[0].Profile.ID != "b" {
		t.Fatalf("unexpected remaining candidates: %#v", remaining)
	}
}

func TestMergeBestServerQualityResponsesSortsByScore(t *testing.T) {
	first := bestServerQualityResponse{Candidates: []bestServerQualityCandidate{{ID: "a", Available: true, Score: 100}}}
	second := bestServerQualityResponse{Candidates: []bestServerQualityCandidate{{ID: "b", Available: true, Score: 200}}}
	merged := mergeBestServerQualityResponses(first, second)
	if len(merged.Candidates) != 2 || merged.Candidates[0].ID != "b" || merged.Candidates[1].ID != "a" {
		t.Fatalf("unexpected merged ordering: %#v", merged.Candidates)
	}
}
