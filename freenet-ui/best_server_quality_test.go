package main

import (
	"context"
	"testing"
)

func stableTestMedia(mbps float64) bestServerMediaQualityResult {
	return bestServerMediaQualityResult{
		OK: true, Samples: 6, MedianMbps: mbps, MinMbps: mbps * 0.9,
		Stalls: 0, ServiceOK: 3, ServiceTotal: 3, Grade: "excellent", Penalty: 0,
	}
}

func TestBestServerQualityBalancesLatencyAndThroughput(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "de", Name: "Germany Frankfurt Extra", Address: "de.example", Port: 443}},
		{Profile: subscriptionProfile{ID: "pl", Name: "Poland Warsaw Extra", Address: "pl.example", Port: 443}},
		{Profile: subscriptionProfile{ID: "nl", Name: "Netherlands Amsterdam Extra", Address: "nl.example", Port: 443}},
	}
	tcp := func(_ context.Context, p subscriptionProfile) bestServerProbeResult {
		switch p.ID {
		case "de":
			return bestServerProbeResult{OK: true, Samples: []int{115, 120, 125}, Median: 120, Jitter: 10}
		case "pl":
			return bestServerProbeResult{OK: true, Samples: []int{106, 110, 114}, Median: 110, Jitter: 8}
		default:
			return bestServerProbeResult{OK: true, Samples: []int{88, 90, 94}, Median: 90, Jitter: 6}
		}
	}
	app := func(_ context.Context, c bestServerInternalCandidate) bestServerQualityApplicationResult {
		switch c.Profile.ID {
		case "de":
			return bestServerQualityApplicationResult{OK: true, HTTP: bestServerProbeResult{OK: true, Samples: []int{210, 220, 230}, Median: 220, Jitter: 20}, DownloadOK: true, DownloadMbps: 120, Media: stableTestMedia(115)}
		case "pl":
			return bestServerQualityApplicationResult{OK: true, HTTP: bestServerProbeResult{OK: true, Samples: []int{174, 180, 188}, Median: 180, Jitter: 14}, DownloadOK: true, DownloadMbps: 25, Media: stableTestMedia(24)}
		default:
			return bestServerQualityApplicationResult{OK: true, HTTP: bestServerProbeResult{OK: true, Samples: []int{250, 260, 270}, Median: 260, Jitter: 20}, DownloadOK: true, DownloadMbps: 80, Media: stableTestMedia(78)}
		}
	}

	result := rankBestServerQualityCandidates(context.Background(), candidates, 3, false, "de.example:443", "", tcp, app)
	if !result.Available || result.Recommendation == nil {
		t.Fatalf("expected recommendation, got %#v", result)
	}
	if result.Recommendation.ID != "de" {
		t.Fatalf("expected balanced quality to keep high-throughput DE, got %#v", result.Recommendation)
	}
	if result.Recommendation.DownloadMbps != 120 {
		t.Fatalf("expected bounded throughput metric, got %#v", result.Recommendation)
	}
	if result.Recommendation.Confidence != "high" {
		t.Fatalf("expected high confidence from stable HTTP + throughput + media probes, got %q", result.Recommendation.Confidence)
	}
}

func TestBestServerQualityPenalizesMissingThroughput(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "fast-latency", Name: "Low latency no speed", Address: "a.example", Port: 443}},
		{Profile: subscriptionProfile{ID: "balanced", Name: "Balanced", Address: "b.example", Port: 443}},
	}
	tcp := func(_ context.Context, p subscriptionProfile) bestServerProbeResult {
		if p.ID == "fast-latency" {
			return bestServerProbeResult{OK: true, Samples: []int{78, 80, 82}, Median: 80, Jitter: 4}
		}
		return bestServerProbeResult{OK: true, Samples: []int{96, 100, 104}, Median: 100, Jitter: 8}
	}
	app := func(_ context.Context, c bestServerInternalCandidate) bestServerQualityApplicationResult {
		if c.Profile.ID == "fast-latency" {
			return bestServerQualityApplicationResult{OK: true, HTTP: bestServerProbeResult{OK: true, Samples: []int{145, 150, 155}, Median: 150, Jitter: 10}}
		}
		return bestServerQualityApplicationResult{OK: true, HTTP: bestServerProbeResult{OK: true, Samples: []int{210, 220, 230}, Median: 220, Jitter: 20}, DownloadOK: true, DownloadMbps: 70, Media: stableTestMedia(68)}
	}

	result := rankBestServerQualityCandidates(context.Background(), candidates, 2, false, "", "", tcp, app)
	if result.Recommendation == nil || result.Recommendation.ID != "balanced" {
		t.Fatalf("speed-unverified candidate must not beat a well-balanced measured candidate: %#v", result.Recommendation)
	}
}

func TestBestServerQualityFailsClosedWhenSpeedUnavailable(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "a", Name: "A", Address: "a.example", Port: 443}},
		{Profile: subscriptionProfile{ID: "b", Name: "B", Address: "b.example", Port: 443}},
	}
	tcp := func(_ context.Context, _ subscriptionProfile) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Samples: []int{100, 105, 110}, Median: 105, Jitter: 10}
	}
	app := func(_ context.Context, _ bestServerInternalCandidate) bestServerQualityApplicationResult {
		return bestServerQualityApplicationResult{OK: true, HTTP: bestServerProbeResult{OK: true, Samples: []int{170, 180, 190}, Median: 180, Jitter: 20}}
	}

	result := rankBestServerQualityCandidates(context.Background(), candidates, 2, false, "", "", tcp, app)
	if result.Available || result.Recommendation != nil {
		t.Fatalf("latency-only result must fail closed without throughput evidence: %#v", result.Recommendation)
	}
}

func TestBestServerQualityPrefersCurrentIdentityOnSharedEndpoint(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "be", Name: "Brussels Belgium Extra", Address: "shared.example", Port: 443}},
		{Profile: subscriptionProfile{ID: "de", Name: "Frankfurt Germany Extra", Address: "shared.example", Port: 443}},
		{Profile: subscriptionProfile{ID: "pl", Name: "Warsaw Poland Extra", Address: "pl.example", Port: 443}},
	}
	tcp := func(_ context.Context, p subscriptionProfile) bestServerProbeResult {
		if p.Address == "shared.example" {
			return bestServerProbeResult{OK: true, Samples: []int{100, 105, 110}, Median: 105, Jitter: 10}
		}
		return bestServerProbeResult{OK: true, Samples: []int{115, 120, 125}, Median: 120, Jitter: 10}
	}
	app := func(_ context.Context, _ bestServerInternalCandidate) bestServerQualityApplicationResult {
		return bestServerQualityApplicationResult{OK: true, HTTP: bestServerProbeResult{OK: true, Samples: []int{200, 205, 210}, Median: 205, Jitter: 10}, DownloadOK: true, DownloadMbps: 80, Media: stableTestMedia(78)}
	}

	result := rankBestServerQualityCandidates(context.Background(), candidates, 3, false, "shared.example:443", "Frankfurt.*Germany", tcp, app)
	var belgium, germany *bestServerQualityCandidate
	for i := range result.Candidates {
		switch result.Candidates[i].ID {
		case "be":
			belgium = &result.Candidates[i]
		case "de":
			germany = &result.Candidates[i]
		}
	}
	if germany == nil || !germany.Current || !germany.Available {
		t.Fatalf("actual current profile must be the shared-endpoint representative: %#v", germany)
	}
	if belgium == nil || belgium.Current || belgium.Available {
		t.Fatalf("non-current label on the same endpoint must not consume a deep quality slot: %#v", belgium)
	}
}

func TestBestServerQualityScoreRewardsMeasuredSpeed(t *testing.T) {
	withSpeed := bestServerQualityScore(250, 120, 20, 10, 100, true)
	withoutSpeed := bestServerQualityScore(250, 120, 20, 10, 0, false)
	if withSpeed <= withoutSpeed {
		t.Fatalf("measured throughput must improve quality score: with=%d without=%d", withSpeed, withoutSpeed)
	}
}
