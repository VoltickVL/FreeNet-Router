package main

import (
	"context"
	"testing"
)

func TestBestServerSharedEndpointUsesActiveFilterForCurrentIdentity(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "0000000000000001", Name: "DE Frankfurt Extra", CountryCode: "de", Address: "5.254.57.129", Port: 443}},
		{Profile: subscriptionProfile{ID: "0000000000000002", Name: "BE Brussels Belgium Extra", CountryCode: "be", Address: "5.254.57.129", Port: 443}},
	}
	tcp := func(context.Context, subscriptionProfile) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Median: 25}
	}
	app := func(_ context.Context, c bestServerInternalCandidate) bestServerProbeResult {
		if c.Profile.CountryCode == "be" {
			return bestServerProbeResult{OK: true, Samples: []int{80, 82}, Median: 81, Jitter: 2}
		}
		return bestServerProbeResult{OK: true, Samples: []int{120, 124}, Median: 122, Jitter: 4}
	}

	result := rankBestServerCandidatesWithFilter(
		context.Background(), candidates, 2, false, "5.254.57.129:443",
		"Frankfurt|Germany|Германия", tcp, app,
	)
	if result.Recommendation == nil || result.Recommendation.ID != "0000000000000002" {
		t.Fatalf("expected Belgium to be recommended by measured quality, got %#v", result.Recommendation)
	}
	if result.Recommendation.Current {
		t.Fatalf("shared endpoint must not make Belgium look current: %#v", result.Recommendation)
	}
	currentCount := 0
	for _, candidate := range result.Candidates {
		if candidate.Current {
			currentCount++
			if candidate.ID != "0000000000000001" {
				t.Fatalf("wrong current profile selected: %#v", candidate)
			}
		}
	}
	if currentCount != 1 {
		t.Fatalf("expected exactly one current profile, got %d: %#v", currentCount, result.Candidates)
	}
}

func TestBestServerSharedEndpointWithoutIdentityIsAmbiguous(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "0000000000000001", Name: "DE Frankfurt Extra", Address: "5.254.57.129", Port: 443}},
		{Profile: subscriptionProfile{ID: "0000000000000002", Name: "BE Brussels Extra", Address: "5.254.57.129", Port: 443}},
	}
	if got := bestServerCurrentCandidateIndex(candidates, "5.254.57.129:443", ""); got != -1 {
		t.Fatalf("ambiguous shared endpoint must not be guessed current, got index %d", got)
	}
}

func TestBestServerInvalidActiveFilterFailsClosed(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "0000000000000001", Name: "DE Frankfurt Extra", Address: "5.254.57.129", Port: 443}},
	}
	if got := bestServerCurrentCandidateIndex(candidates, "5.254.57.129:443", "["); got != -1 {
		t.Fatalf("invalid active filter must fail closed, got index %d", got)
	}
}
