package main

import (
	"context"
	"sort"
)

const bestServerMeasuredComparisonTarget = 3

func measuredBestServerForeignCount(candidates []bestServerQualityCandidate) int {
	count := 0
	for _, candidate := range candidates {
		if candidate.Current {
			continue
		}
		if candidate.Tested && candidate.DownloadMbps > 0 && candidate.MediaSamples >= bestServerMediaRequiredRuns {
			count++
		}
	}
	return count
}

func remainingBestServerCandidates(all, selected []bestServerInternalCandidate) []bestServerInternalCandidate {
	seen := make(map[string]bool, len(selected))
	for _, candidate := range selected {
		seen[candidate.Profile.ID] = true
	}
	remaining := make([]bestServerInternalCandidate, 0, len(all))
	for _, candidate := range all {
		if !seen[candidate.Profile.ID] {
			remaining = append(remaining, candidate)
		}
	}
	return remaining
}

func mergeBestServerQualityResponses(first, second bestServerQualityResponse) bestServerQualityResponse {
	out := first
	out.Candidates = append(append([]bestServerQualityCandidate(nil), first.Candidates...), second.Candidates...)
	out.Partial = first.Partial || second.Partial
	out.Available = first.Available || second.Available
	if second.Recommendation != nil && (out.Recommendation == nil || second.Recommendation.Score > out.Recommendation.Score) {
		copyValue := *second.Recommendation
		out.Recommendation = &copyValue
	}
	sort.SliceStable(out.Candidates, func(i, j int) bool {
		a, b := out.Candidates[i], out.Candidates[j]
		if a.Available != b.Available {
			return a.Available
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.ApplicationMS != b.ApplicationMS {
			return a.ApplicationMS < b.ApplicationMS
		}
		if a.DownloadMbps != b.DownloadMbps {
			return a.DownloadMbps > b.DownloadMbps
		}
		return a.ID < b.ID
	})
	return out
}

// rankBestServerForeignWithFill keeps the normal bounded first pass, but if
// fewer than three alternatives produced real throughput evidence it spends
// the remaining global scan budget on one more application-aware batch. This
// avoids presenting only one or two rows merely because an early Speedtest
// candidate failed while other measured Extra profiles remain available.
func (a *app) rankBestServerForeignWithFill(
	ctx context.Context,
	candidates []bestServerInternalCandidate,
	profilesScanned int,
	truncated bool,
	currentEndpoint string,
	currentFilter string,
) bestServerQualityResponse {
	firstBatch := a.applicationAwareBestServerShortlist(ctx, candidates, currentEndpoint, currentFilter)
	first := rankBestServerQualityCandidates(
		ctx, firstBatch, profilesScanned, truncated, currentEndpoint, currentFilter,
		defaultBestServerQualityTCPProbe, a.probeBestServerQualityApplication,
	)
	if ctx.Err() != nil || measuredBestServerForeignCount(first.Candidates) >= bestServerMeasuredComparisonTarget {
		return first
	}

	remaining := remainingBestServerCandidates(candidates, firstBatch)
	if len(remaining) == 0 {
		return first
	}
	secondBatch := a.applicationAwareBestServerShortlist(ctx, remaining, currentEndpoint, currentFilter)
	if len(secondBatch) == 0 || ctx.Err() != nil {
		return first
	}
	second := rankBestServerQualityCandidates(
		ctx, secondBatch, profilesScanned, truncated, currentEndpoint, currentFilter,
		defaultBestServerQualityTCPProbe, a.probeBestServerQualityApplication,
	)
	return mergeBestServerQualityResponses(first, second)
}
