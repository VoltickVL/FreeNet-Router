package main

import (
	"context"
	"testing"
)

func stableTestMedia(mbps float64) bestServerMediaQualityResult {
	return bestServerMediaQualityResult{
		OK: true, Samples: bestServerMediaChunkRuns, MedianMbps: mbps, MinMbps: mbps * 0.9,
		Stalls: 0, ServiceOK: 3, ServiceTotal: 3, Grade: "excellent", Penalty: 0,
	}
}

func TestBestServerQualityBalancesLatencyAndThroughput(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "de", Name: "Germany Frankfurt Extra", Address: "de.example", Port: 443}},
		{Profile: subscriptionProfile{ID: "pl", Name: "Poland Warsaw Extra", Address: "pl.example", Port: 443}},
		{Profile: subscriptionProfile{ID: "nl", Name: "Netherlands Amsterdam Extra", Address: "nl.example", Port: 443}},
	}
	app := func(_ context.Context, c bestServerInternalCandidate) bestServerQualityApplicationResult {
		switch c.Profile.ID {
		case "de":
			return bestServerQualityApplicationResult{OK: true, HTTP: bestServerProbeResult{OK: true, Samples: []int{150, 160, 170}, Median: 160, Jitter: 20}, DownloadOK: true, DownloadMbps: 120, Media: stableTestMedia(115)}
		case "pl":
			return bestServerQualityApplicationResult{OK: true, HTTP: bestServerProbeResult{OK: true, Samples: []int{174, 180, 188}, Median: 180, Jitter: 14}, DownloadOK: true, DownloadMbps: 25, Media: stableTestMedia(24)}
		default:
			return bestServerQualityApplicationResult{OK: true, HTTP: bestServerProbeResult{OK: true, Samples: []int{250, 260, 270}, Median: 260, Jitter: 20}, DownloadOK: true, DownloadMbps: 80, Media: stableTestMedia(78)}
		}
	}

	result := rankBestServerQualityCandidates(context.Background(), candidates, 3, false, "de.example:443", "", app)
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

func TestBestServerQualityRejectsHighApplicationLatencyDespiteThroughput(t *testing.T) {
	candidate := bestServerQualityCandidate{
		Tested: true, Available: true, ApplicationMS: bestServerQualityMaxApplicationMS + 13,
		DownloadMbps: 114, MediaSamples: bestServerMediaChunkRuns, MediaStalls: 0,
		MediaGrade: "excellent", ServiceOK: 4, ServiceTotal: 4, JitterMS: 13, TCPJitterMS: 10,
	}
	if eligibleBestServerQuality(candidate) {
		t.Fatalf("high-latency candidate must fail eligibility even with strong throughput: %+v", candidate)
	}
	candidate.ApplicationMS = bestServerQualityMaxApplicationMS
	if !eligibleBestServerQuality(candidate) {
		t.Fatalf("candidate at latency ceiling should remain eligible when all other gates pass: %+v", candidate)
	}
}

func TestBestServerQualityPenalizesMissingThroughput(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "fast-latency", Name: "Low latency no speed", Address: "a.example", Port: 443}},
		{Profile: subscriptionProfile{ID: "balanced", Name: "Balanced", Address: "b.example", Port: 443}},
	}
	app := func(_ context.Context, c bestServerInternalCandidate) bestServerQualityApplicationResult {
		if c.Profile.ID == "fast-latency" {
			return bestServerQualityApplicationResult{OK: true, HTTP: bestServerProbeResult{OK: true, Samples: []int{145, 150, 155}, Median: 150, Jitter: 10}}
		}
		return bestServerQualityApplicationResult{OK: true, HTTP: bestServerProbeResult{OK: true, Samples: []int{150, 160, 170}, Median: 160, Jitter: 20}, DownloadOK: true, DownloadMbps: 70, Media: stableTestMedia(68)}
	}

	result := rankBestServerQualityCandidates(context.Background(), candidates, 2, false, "", "", app)
	if result.Recommendation == nil || result.Recommendation.ID != "balanced" {
		t.Fatalf("speed-unverified candidate must not beat a well-balanced measured candidate: %#v", result.Recommendation)
	}
}

func TestBestServerQualityFailsClosedWhenSpeedUnavailable(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "a", Name: "A", Address: "a.example", Port: 443}},
		{Profile: subscriptionProfile{ID: "b", Name: "B", Address: "b.example", Port: 443}},
	}
	app := func(_ context.Context, _ bestServerInternalCandidate) bestServerQualityApplicationResult {
		return bestServerQualityApplicationResult{OK: true, HTTP: bestServerProbeResult{OK: true, Samples: []int{170, 180, 190}, Median: 180, Jitter: 20}}
	}

	result := rankBestServerQualityCandidates(context.Background(), candidates, 2, false, "", "", app)
	if result.Available || result.Recommendation != nil {
		t.Fatalf("latency-only result must fail closed without throughput evidence: %#v", result.Recommendation)
	}
}

func TestBestServerQualityMeasuresSharedEndpointLogicalProfilesIndependently(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "be", Name: "Brussels Belgium Extra", Address: "shared.example", Port: 443}},
		{Profile: subscriptionProfile{ID: "de", Name: "Frankfurt Germany Extra", Address: "shared.example", Port: 443}},
		{Profile: subscriptionProfile{ID: "pl", Name: "Warsaw Poland Extra", Address: "pl.example", Port: 443}},
	}
	app := func(_ context.Context, c bestServerInternalCandidate) bestServerQualityApplicationResult {
		median := 205
		speed := 80.0
		if c.Profile.ID == "be" {
			median = 175
			speed = 95
		}
		return bestServerQualityApplicationResult{
			OK: true,
			HTTP: bestServerProbeResult{OK: true, Samples: []int{median - 5, median, median + 5}, Median: median, Jitter: 10},
			DownloadOK: true, DownloadMbps: speed, Media: stableTestMedia(speed - 2),
		}
	}

	result := rankBestServerQualityCandidates(context.Background(), candidates, 3, false, "shared.example:443", "Frankfurt.*Germany", app)
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
		t.Fatalf("actual current profile must remain identified: %#v", germany)
	}
	if belgium == nil || belgium.Current || !belgium.Available || belgium.ApplicationMS != 175 {
		t.Fatalf("different logical profile on shared ingress must receive its own deep measurement: %#v", belgium)
	}
}

func TestBestServerQualityScorePrefersStableLatencyOverExtraMbps(t *testing.T) {
	stable := bestServerQualityScore(160, 10, 80, true)
	fastButLaggy := bestServerQualityScore(200, 50, 150, true)
	if stable <= fastButLaggy {
		t.Fatalf("stability-first score lost to extra Mbps: stable=%d fast-laggy=%d", stable, fastButLaggy)
	}
}

func TestBestServerQualityScoreRewardsMeasuredSpeed(t *testing.T) {
	withSpeed := bestServerQualityScore(250, 20, 100, true)
	withoutSpeed := bestServerQualityScore(250, 20, 0, false)
	if withSpeed <= withoutSpeed {
		t.Fatalf("measured throughput must improve quality score: with=%d without=%d", withSpeed, withoutSpeed)
	}
}

func TestBestServerQualityPublishesOnlyStrictComparableThroughput(t *testing.T) {
	candidate := bestServerInternalCandidate{
		Profile: subscriptionProfile{ID: "strict", Name: "Strict Extra", Address: "strict.example", Port: 443},
	}
	app := func(_ context.Context, _ bestServerInternalCandidate) bestServerQualityApplicationResult {
		return bestServerQualityApplicationResult{
			OK: true,
			HTTP: bestServerProbeResult{OK: true, Samples: []int{145, 150, 155}, Median: 150, Jitter: 10},
			DownloadOK: true, DownloadMbps: 82,
			FallbackDownloadMbps: 999, ThroughputSource: bestServerThroughputCurrentFallback,
			Media: stableTestMedia(82),
		}
	}
	result := rankBestServerQualityCandidates(context.Background(), []bestServerInternalCandidate{candidate}, 1, false, "", "", app)
	if len(result.Candidates) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	got := result.Candidates[0]
	if got.DownloadMbps != 82 || got.ThroughputSource != bestServerThroughputStrictAggregate {
		t.Fatalf("Best Server candidate must publish strict comparable throughput: %#v", got)
	}
	if got.FallbackDownloadMbps != 0 {
		t.Fatalf("current-only fallback throughput leaked into comparison candidate: %#v", got)
	}
}

func TestBestServerQualityCarriesCanonicalVPNRTTWithoutTCPGate(t *testing.T) {
	candidate := bestServerInternalCandidate{
		Profile: subscriptionProfile{ID: "vpn-rtt", Name: "VPN RTT", Address: "shared.example", Port: 443},
		VPNRTTMS: 163, VPNJitterMS: 7,
	}
	appCalls := 0
	app := func(_ context.Context, _ bestServerInternalCandidate) bestServerQualityApplicationResult {
		appCalls++
		return bestServerQualityApplicationResult{
			OK: true,
			HTTP: bestServerProbeResult{OK: true, Samples: []int{175, 180, 185}, Median: 180, Jitter: 10},
			DownloadOK: true, DownloadMbps: 80, Media: stableTestMedia(78),
		}
	}
	result := rankBestServerQualityCandidates(context.Background(), []bestServerInternalCandidate{candidate}, 1, false, "", "", app)
	if appCalls != 1 {
		t.Fatalf("deep VPN probe calls=%d want=1", appCalls)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].VPNRTTMS != 163 || result.Candidates[0].VPNJitterMS != 7 {
		t.Fatalf("canonical VPN RTT evidence was not carried into result: %#v", result.Candidates)
	}
	if result.Candidates[0].TCPRTTMS != 0 || result.Candidates[0].TCPJitterMS != 0 {
		t.Fatalf("raw endpoint TCP must not be manufactured by canonical deep selection: %#v", result.Candidates[0])
	}
}
