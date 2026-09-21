package main

import (
	"encoding/json"
	"math"
	"strings"
)

const (
	bestServerMeaningfulSpeedGainRatio = 0.15
	bestServerMeaningfulSpeedGainMbps  = 15.0
	bestServerMeaningfulHTTPGainMS     = 20
)

func bestServerMeaningfullyBetter(current, challenger bestServerQualityCandidate) bool {
	if !challenger.Eligible || challenger.Current {
		return false
	}
	if !current.Eligible {
		return true
	}
	if challenger.Score <= current.Score {
		return false
	}

	minimumSpeedGain := math.Max(bestServerMeaningfulSpeedGainMbps, current.DownloadMbps*bestServerMeaningfulSpeedGainRatio)
	speedGain := challenger.DownloadMbps - current.DownloadMbps
	httpGain := current.ApplicationMS - challenger.ApplicationMS
	speedNotMateriallyWorse := current.DownloadMbps <= 0 || challenger.DownloadMbps >= current.DownloadMbps*0.90
	return speedGain >= minimumSpeedGain || (httpGain >= bestServerMeaningfulHTTPGainMS && speedNotMateriallyWorse)
}

func completeBestServerCurrentBaseline(candidate bestServerQualityCandidate) bool {
	return candidate.ApplicationMS > 0 && candidate.MediaSamples >= bestServerMediaRequiredRuns && candidate.ServiceTotal >= 3
}

func bestServerCurrentCandidateIndexes(candidates []bestServerQualityCandidate) (int, int) {
	currentAnyIndex := -1
	currentEligibleIndex := -1
	for i := range candidates {
		if !candidates[i].Current {
			continue
		}
		if currentAnyIndex < 0 {
			currentAnyIndex = i
		}
		if candidates[i].Eligible {
			currentEligibleIndex = i
			break
		}
	}
	return currentAnyIndex, currentEligibleIndex
}

func bestServerLogicalProfileKey(candidate bestServerQualityCandidate) string {
	key := strings.ToLower(strings.Join(strings.Fields(profileDisplayName(candidate.Name)), " "))
	if key == "" || key == "extra profile" {
		return ""
	}
	return key
}

func bestServerSameLogicalProfile(a, b bestServerQualityCandidate) bool {
	left := bestServerLogicalProfileKey(a)
	right := bestServerLogicalProfileKey(b)
	return left != "" && left == right
}

func removeBestServerCurrentLogicalAlternatives(candidates []bestServerQualityCandidate, currentIndex int) []bestServerQualityCandidate {
	if currentIndex < 0 || currentIndex >= len(candidates) {
		return candidates
	}
	current := candidates[currentIndex]
	out := make([]bestServerQualityCandidate, 0, len(candidates))
	for i, candidate := range candidates {
		if i != currentIndex && !candidate.Current && bestServerSameLogicalProfile(current, candidate) {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

func bestServerRecommendationExists(candidates []bestServerQualityCandidate, recommendation *bestServerQualityCandidate) bool {
	if recommendation == nil {
		return false
	}
	for i := range candidates {
		candidate := candidates[i]
		if candidate.ID != "" && recommendation.ID != "" && candidate.ID == recommendation.ID {
			return true
		}
		if candidate.Endpoint != "" && recommendation.Endpoint != "" && endpointsEqual(candidate.Endpoint, recommendation.Endpoint) {
			return true
		}
	}
	return false
}

func resetBestServerRecommendationToFirstEligible(response *bestServerQualityResponse) {
	response.Recommendation = nil
	response.Available = false
	for i := range response.Candidates {
		if response.Candidates[i].Eligible {
			candidate := response.Candidates[i]
			response.Recommendation = &candidate
			response.Available = true
			return
		}
	}
}

func applyBestServerRecommendationDeadband(response bestServerQualityResponse) bestServerQualityResponse {
	out := cloneBestServerQualityResponse(response)
	for i := range out.Candidates {
		out.Candidates[i].Reason = strings.ReplaceAll(out.Candidates[i].Reason, "Speedtest single-stream median", "Speedtest aggregate capacity")
	}
	currentAnyIndex, currentEligibleIndex := bestServerCurrentCandidateIndexes(out.Candidates)
	if currentAnyIndex >= 0 {
		out.Candidates = removeBestServerCurrentLogicalAlternatives(out.Candidates, currentAnyIndex)
		if !bestServerRecommendationExists(out.Candidates, out.Recommendation) {
			resetBestServerRecommendationToFirstEligible(&out)
		}
		currentAnyIndex, currentEligibleIndex = bestServerCurrentCandidateIndexes(out.Candidates)
	}

	// Missing evidence is different from a measured unhealthy current VPN. If
	// the current path was only partially measured, fail closed. If the current
	// path was fully measured and proved unhealthy, a healthy replacement may be
	// recommended.
	if currentAnyIndex >= 0 && currentEligibleIndex < 0 && !completeBestServerCurrentBaseline(out.Candidates[currentAnyIndex]) {
		out.Recommendation = nil
		out.Available = false
		out.Message = "Текущий VPN не удалось полностью измерить. Переключение не предлагается, пока нет подтверждённого сравнения."
		return out
	}
	if currentEligibleIndex < 0 {
		return out
	}
	current := out.Candidates[currentEligibleIndex]

	// Quality eligibility and recommendation significance are deliberately
	// separate. A healthy alternative that is only marginally different from
	// current remains a valid comparison/manual choice; it is simply not a
	// reason for FreeNet to recommend switching a working VPN.
	var winner *bestServerQualityCandidate
	for i := range out.Candidates {
		candidate := out.Candidates[i]
		if candidate.Eligible && !candidate.Current && bestServerMeaningfullyBetter(current, candidate) {
			if winner == nil || candidate.Score > winner.Score {
				copyValue := candidate
				winner = &copyValue
			}
		}
	}
	if winner != nil {
		out.Recommendation = winner
		out.Available = true
		return out
	}

	copyCurrent := current
	out.Recommendation = &copyCurrent
	out.Available = true
	out.Message = "Текущий VPN остаётся предпочтительным: проверенные альтернативы не дают значимого улучшения."
	return out
}

func (response bestServerQualityResponse) MarshalJSON() ([]byte, error) {
	type responseAlias bestServerQualityResponse
	final := applyBestServerRecommendationDeadband(response)
	return json.Marshal(responseAlias(final))
}
