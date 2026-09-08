package main

import "testing"

func healthyRecommendationCandidate(id string, current bool, speed float64, httpMS, score int) bestServerQualityCandidate {
	return bestServerQualityCandidate{
		ID: id, Current: current, Eligible: true, Available: true,
		DownloadMbps: speed, ApplicationMS: httpMS, Score: score,
		MediaSamples: bestServerMediaChunkRuns, MediaGrade: "excellent",
		ServiceOK: 4, ServiceTotal: 4, Confidence: "high",
	}
}

func TestBestServerRecommendationKeepsCurrentForNoiseLevelDifference(t *testing.T) {
	current := healthyRecommendationCandidate("pl", true, 23.7, 189, 10000)
	challenger := healthyRecommendationCandidate("bg", false, 25.7, 188, 10100)
	response := bestServerQualityResponse{Available: true, Candidates: []bestServerQualityCandidate{challenger, current}, Recommendation: &challenger}
	final := applyBestServerRecommendationDeadband(response)
	if final.Recommendation == nil || final.Recommendation.ID != "pl" {
		t.Fatalf("noise-level difference must keep current VPN: %#v", final.Recommendation)
	}
	for _, candidate := range final.Candidates {
		if candidate.ID == "bg" && !candidate.Eligible {
			t.Fatalf("marginal but healthy challenger must remain available for comparison/manual choice: %#v", candidate)
		}
	}
}

func TestBestServerRecommendationAllowsMeaningfulAggregateSpeedGain(t *testing.T) {
	current := healthyRecommendationCandidate("pl", true, 200, 190, 10000)
	challenger := healthyRecommendationCandidate("de", false, 250, 188, 11200)
	response := bestServerQualityResponse{Available: true, Candidates: []bestServerQualityCandidate{challenger, current}, Recommendation: &challenger}
	final := applyBestServerRecommendationDeadband(response)
	if final.Recommendation == nil || final.Recommendation.ID != "de" {
		t.Fatalf("meaningful aggregate speed gain should allow challenger: %#v", final.Recommendation)
	}
}

func TestBestServerRecommendationAllowsMeaningfulHTTPGainWithoutLargeSpeedLoss(t *testing.T) {
	current := healthyRecommendationCandidate("pl", true, 200, 210, 10000)
	challenger := healthyRecommendationCandidate("de", false, 190, 180, 10500)
	response := bestServerQualityResponse{Available: true, Candidates: []bestServerQualityCandidate{challenger, current}, Recommendation: &challenger}
	final := applyBestServerRecommendationDeadband(response)
	if final.Recommendation == nil || final.Recommendation.ID != "de" {
		t.Fatalf("meaningful HTTP gain with comparable speed should allow challenger: %#v", final.Recommendation)
	}
}

func TestBestServerRecommendationDoesNotProtectUnhealthyCurrent(t *testing.T) {
	current := healthyRecommendationCandidate("pl", true, 0, 300, 2000)
	current.Eligible = false
	challenger := healthyRecommendationCandidate("de", false, 80, 180, 9000)
	response := bestServerQualityResponse{Available: true, Candidates: []bestServerQualityCandidate{challenger, current}, Recommendation: &challenger}
	final := applyBestServerRecommendationDeadband(response)
	if final.Recommendation == nil || final.Recommendation.ID != "de" {
		t.Fatalf("unhealthy current must not block a valid replacement: %#v", final.Recommendation)
	}
}
