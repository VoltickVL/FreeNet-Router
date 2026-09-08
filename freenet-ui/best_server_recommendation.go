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

func applyBestServerRecommendationDeadband(response bestServerQualityResponse) bestServerQualityResponse {
	out := cloneBestServerQualityResponse(response)
	for i := range out.Candidates {
		out.Candidates[i].Reason = strings.ReplaceAll(out.Candidates[i].Reason, "Speedtest single-stream median", "Speedtest aggregate capacity")
	}
	currentIndex := -1
	for i := range out.Candidates {
		if out.Candidates[i].Current && out.Candidates[i].Eligible {
			currentIndex = i
			break
		}
	}
	if currentIndex < 0 {
		return out
	}
	current := out.Candidates[currentIndex]

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
