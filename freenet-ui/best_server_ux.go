package main

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	bestServerCurrentScanTimeout   = 45 * time.Second
	bestServerMeasuredDisplayTarget = 3
)

func registerBestServerUXAPI(mux *http.ServeMux, a *app) {
	jobs := &bestServerJobs{}
	mux.HandleFunc("GET /api/vpn/current-quality", a.requireAuth(jobs.wrap(a, "current", a.handleCurrentVPNQuality, a.scanCurrentVPNQuality)))
	mux.HandleFunc("GET /api/vpn/best-foreign", a.requireAuth(jobs.wrap(a, "best", a.handleBestServerForeign, a.scanBestServerForeign)))
}

func isRussianBestServerCandidate(candidate bestServerInternalCandidate) bool {
	if strings.EqualFold(strings.TrimSpace(candidate.Profile.CountryCode), "ru") {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(candidate.Profile.Name))
	return strings.Contains(name, "russia") || strings.Contains(name, "росси")
}

func isSpecializedBestServerCandidate(candidate bestServerInternalCandidate) bool {
	name := strings.ToLower(strings.TrimSpace(candidate.Profile.Name))
	return strings.Contains(name, "whitelist")
}

func filterForeignBestServerCandidates(candidates []bestServerInternalCandidate) []bestServerInternalCandidate {
	filtered := make([]bestServerInternalCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if isRussianBestServerCandidate(candidate) || isSpecializedBestServerCandidate(candidate) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func filterMeasuredBestServerResults(candidates []bestServerQualityCandidate) []bestServerQualityCandidate {
	filtered := make([]bestServerQualityCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Current || (candidate.Tested && candidate.DownloadMbps > 0 && candidate.MediaSamples >= bestServerMediaRequiredRuns) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func countMeasuredForeignBestServerResults(candidates []bestServerQualityCandidate) int {
	count := 0
	for _, candidate := range candidates {
		if !candidate.Current && candidate.Tested && candidate.DownloadMbps > 0 && candidate.MediaSamples >= bestServerMediaRequiredRuns {
			count++
		}
	}
	return count
}

func remainingUntestedBestServerCandidates(internal []bestServerInternalCandidate, results []bestServerQualityCandidate) []bestServerInternalCandidate {
	tested := make(map[string]bool, len(results))
	for _, candidate := range results {
		if candidate.Tested && candidate.ID != "" {
			tested[candidate.ID] = true
		}
	}
	remaining := make([]bestServerInternalCandidate, 0, len(internal))
	for _, candidate := range internal {
		if !tested[candidate.Profile.ID] {
			remaining = append(remaining, candidate)
		}
	}
	return remaining
}

func mergeBestServerQualityResponse(primary, extra bestServerQualityResponse) bestServerQualityResponse {
	seen := make(map[string]bool, len(primary.Candidates)+len(extra.Candidates))
	merged := make([]bestServerQualityCandidate, 0, len(primary.Candidates)+len(extra.Candidates))
	for _, group := range [][]bestServerQualityCandidate{primary.Candidates, extra.Candidates} {
		for _, candidate := range group {
			key := candidate.ID + "|" + candidate.Endpoint
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, candidate)
		}
	}
	primary.Candidates = merged
	primary.Partial = primary.Partial || extra.Partial
	if extra.Available && extra.Recommendation != nil && (!primary.Available || primary.Recommendation == nil || extra.Recommendation.Score > primary.Recommendation.Score) {
		copyRecommendation := *extra.Recommendation
		primary.Recommendation = &copyRecommendation
		primary.Available = true
	}
	return primary
}

func finalizeMeasuredBestServerResponse(response bestServerQualityResponse) bestServerQualityResponse {
	response.Candidates = filterMeasuredBestServerResults(response.Candidates)
	sort.SliceStable(response.Candidates, func(i, j int) bool {
		a, b := response.Candidates[i], response.Candidates[j]
		if a.Current != b.Current {
			return !a.Current
		}
		if a.Eligible != b.Eligible {
			return a.Eligible
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
	response.Available = false
	response.Recommendation = nil
	for _, candidate := range response.Candidates {
		if candidate.Eligible && !candidate.Current {
			best := candidate
			response.Recommendation = &best
			response.Available = true
			break
		}
	}
	return response
}

func currentBestServerCandidate(candidates []bestServerInternalCandidate, currentEndpoint, currentFilter string) ([]bestServerInternalCandidate, bool) {
	index := bestServerCurrentCandidateIndex(candidates, currentEndpoint, currentFilter)
	if index < 0 {
		return nil, false
	}
	return []bestServerInternalCandidate{candidates[index]}, true
}

func withoutBestServerCandidate(candidates []bestServerInternalCandidate, index int) []bestServerInternalCandidate {
	if index < 0 || index >= len(candidates) {
		return candidates
	}
	out := make([]bestServerInternalCandidate, 0, len(candidates)-1)
	out = append(out, candidates[:index]...)
	out = append(out, candidates[index+1:]...)
	return out
}

func (a *app) handleCurrentVPNQuality(w http.ResponseWriter, r *http.Request) {
	if len(a.sem) > 0 {
		writeJSON(w, http.StatusConflict, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Error: "VPN operation is active; current VPN check was not started",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), bestServerCurrentScanTimeout)
	defer cancel()
	response, err := a.scanCurrentVPNQuality(ctx)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = http.StatusGatewayTimeout
		}
		writeJSON(w, status, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Error: safeBestServerError(err),
		})
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *app) scanCurrentVPNQuality(ctx context.Context) (bestServerQualityResponse, error) {
	currentEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	currentFilter := readBestServerCurrentFilter(a.cfg.FilterPath)
	response := a.scanActiveCurrentVPNQuality(ctx, currentEndpoint, currentFilter)
	if ctx.Err() != nil {
		return bestServerQualityResponse{}, ctx.Err()
	}
	if after := readBestServerCurrentEndpoint(a.cfg.OutPath); after != currentEndpoint {
		return bestServerQualityResponse{}, errors.New("VPN endpoint changed during current VPN check")
	}
	if afterFilter := readBestServerCurrentFilter(a.cfg.FilterPath); afterFilter != currentFilter {
		return bestServerQualityResponse{}, errors.New("VPN profile identity changed during current VPN check")
	}
	return response, nil
}

func (a *app) handleBestServerForeign(w http.ResponseWriter, r *http.Request) {
	if len(a.sem) > 0 {
		writeJSON(w, http.StatusConflict, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Error: "VPN operation is active; Best Server scan was not started",
		})
		return
	}

	if !prepareBestServerResponse(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), bestServerQualityScanTimeout)
	defer cancel()
	response, err := a.scanBestServerForeign(ctx)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = http.StatusGatewayTimeout
		}
		writeJSON(w, status, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Error: safeBestServerError(err),
		})
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *app) scanBestServerForeign(ctx context.Context) (bestServerQualityResponse, error) {
	currentEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	currentFilter := readBestServerCurrentFilter(a.cfg.FilterPath)
	all, _, truncated, err := a.discoverBestServerCandidates(ctx)
	if err != nil {
		return bestServerQualityResponse{}, err
	}
	candidates := filterForeignBestServerCandidates(all)
	if len(candidates) == 0 {
		return bestServerQualityResponse{
			Success: true, Available: false, Candidates: []bestServerQualityCandidate{}, ProfilesScanned: 0, ProfilesTotal: 0,
			ProfilesTruncated: truncated, Mutation: "NONE", ScannedAt: time.Now().UTC().Format(time.RFC3339), CurrentEndpoint: currentEndpoint,
			Message: "Подходящих зарубежных Extra-профилей нет; российские и специализированные Whitelist-профили исключены из автоматического подбора.",
		}, nil
	}

	profilesScanned := len(candidates)
	cachedCurrent, cachedOK := loadBestServerCurrentQuality(currentEndpoint, currentFilter)
	if cachedOK {
		if currentIndex := bestServerCurrentCandidateIndex(candidates, currentEndpoint, currentFilter); currentIndex >= 0 {
			candidates = withoutBestServerCandidate(candidates, currentIndex)
		}
	}

	// First compare the real application path through each candidate VPN with a
	// cheap bounded probe. Keep a small reserve beyond the first deep-test batch;
	// if one of the first candidates cannot produce throughput evidence, use the
	// remaining budget to fill the user-visible top three with measured results.
	candidates = a.applicationAwareBestServerShortlist(ctx, candidates, currentEndpoint, currentFilter)
	response := rankBestServerQualityCandidates(
		ctx, candidates, profilesScanned, truncated, currentEndpoint, currentFilter,
		defaultBestServerQualityTCPProbe, a.probeBestServerQualityApplication,
	)
	if ctx.Err() != nil {
		return bestServerQualityResponse{}, ctx.Err()
	}
	if countMeasuredForeignBestServerResults(response.Candidates) < bestServerMeasuredDisplayTarget {
		remaining := remainingUntestedBestServerCandidates(candidates, response.Candidates)
		if len(remaining) > 0 {
			hasBudget := true
			if deadline, ok := ctx.Deadline(); ok {
				hasBudget = time.Until(deadline) >= bestServerQualityCandidateTimeout+time.Second
			}
			if hasBudget {
				extra := rankBestServerQualityCandidates(
					ctx, remaining, profilesScanned, truncated, currentEndpoint, currentFilter,
					defaultBestServerQualityTCPProbe, a.probeBestServerQualityApplication,
				)
				if ctx.Err() == nil {
					response = mergeBestServerQualityResponse(response, extra)
				}
			}
		}
	}
	response = finalizeMeasuredBestServerResponse(response)
	if cachedOK {
		response.Candidates = append(response.Candidates, cachedCurrent)
	} else if candidate, ok := currentBestServerQualityCandidate(response); ok {
		storeBestServerCurrentQuality(currentEndpoint, currentFilter, candidate)
	}
	response.ProfilesScanned = profilesScanned
	response.Success = true
	response.Mutation = "NONE"
	response.ScannedAt = time.Now().UTC().Format(time.RFC3339)
	response.CurrentEndpoint = currentEndpoint
	if response.Available && response.Recommendation != nil {
		if response.Recommendation.Current {
			response.Message = "Текущий VPN уже лучший среди проверенных зарубежных профилей."
		} else {
			response.Message = "FreeNet нашёл лучший зарубежный VPN-профиль."
		}
	} else {
		response.Message = "Достоверная рекомендация среди зарубежных профилей сейчас недоступна; текущий VPN не изменён."
	}
	if cachedOK {
		response.Message += " Свежий подтверждённый замер текущего VPN переиспользован без повторной тяжёлой Speedtest-проверки."
	}
	if after := readBestServerCurrentEndpoint(a.cfg.OutPath); after != currentEndpoint {
		return bestServerQualityResponse{}, errors.New("VPN endpoint changed during Best Server scan")
	}
	if afterFilter := readBestServerCurrentFilter(a.cfg.FilterPath); afterFilter != currentFilter {
		return bestServerQualityResponse{}, errors.New("VPN profile identity changed during Best Server scan")
	}
	return response, nil
}
