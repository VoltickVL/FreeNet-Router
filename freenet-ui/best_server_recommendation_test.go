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

// Different endpoints of the same logical profile are refreshed through the
// explicit current-VPN endpoint action, not offered as switchable alternatives.
func TestBestServerRecommendationRemovesSameLogicalCurrentAlternatives(t *testing.T) {
	current := healthyRecommendationCandidate("current-frankfurt", true, 99.2, 162, 10000)
	current.Name = "DE Франкфурт-на-Майне, Германия, Extra"
	current.Endpoint = "87.85.241.87:443"
	sameLogical := healthyRecommendationCandidate("same-frankfurt-new-endpoint", false, 122.0, 167, 12000)
	sameLogical.Name = "DE Франкфурт-на-Майне, Германия, Extra"
	sameLogical.Endpoint = "203.0.113.10:443"
	other := healthyRecommendationCandidate("bratislava", false, 94.2, 180, 9000)
	other.Name = "SK Братислава, Словакия, Extra"
	other.Endpoint = "203.0.113.20:443"

	response := bestServerQualityResponse{
		Available:      true,
		Candidates:     []bestServerQualityCandidate{sameLogical, other, current},
		Recommendation: &sameLogical,
	}
	final := applyBestServerRecommendationDeadband(response)
	for _, candidate := range final.Candidates {
		if candidate.ID == sameLogical.ID {
			t.Fatalf("same logical current profile must not be exposed as a switchable alternative: %#v", candidate)
		}
	}
	if final.Recommendation != nil && final.Recommendation.ID == sameLogical.ID {
		t.Fatalf("same logical current profile must not remain the recommendation: %#v", final.Recommendation)
	}
	foundOther := false
	for _, candidate := range final.Candidates {
		if candidate.ID == other.ID {
			foundOther = true
		}
	}
	if !foundOther {
		t.Fatalf("different logical profile must remain available for comparison: %#v", final.Candidates)
	}
}
