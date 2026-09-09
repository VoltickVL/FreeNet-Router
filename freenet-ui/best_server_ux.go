package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	bestServerCurrentScanTimeout = 45 * time.Second
	bestServerMeasuredTarget     = 3
	bestServerQualityBatchSize   = 6
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

func countMeasuredBestServerAlternatives(candidates []bestServerQualityCandidate) int {
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

func hasMeasuredBestServerCurrent(candidates []bestServerQualityCandidate) bool {
	for _, candidate := range candidates {
		if candidate.Current && completeBestServerCurrentBaseline(candidate) {
			return true
		}
	}
	return false
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
	enrichCurrentBestServerTCP(ctx, &response, currentEndpoint)
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
	// cheap bounded probe. Keep a reserve beyond the first deep-test batch so a
	// failed Speedtest does not reduce the final comparison to only one or two
	// rows. Expensive probing stops as soon as three measured alternatives exist
	// and a trustworthy current baseline is also available.
	candidates = a.applicationAwareBestServerShortlist(ctx, candidates, currentEndpoint, currentFilter)
	response := bestServerQualityResponse{Candidates: []bestServerQualityCandidate{}, ProfilesTruncated: truncated, Mutation: "NONE"}
	for start := 0; start < len(candidates); start += bestServerQualityBatchSize {
		if ctx.Err() != nil {
			return bestServerQualityResponse{}, ctx.Err()
		}
		end := start + bestServerQualityBatchSize
		if end > len(candidates) {
			end = len(candidates)
		}
		batch := rankBestServerQualityCandidates(
			ctx, candidates[start:end], profilesScanned, truncated, currentEndpoint, currentFilter,
			defaultBestServerQualityTCPProbe, a.probeBestServerQualityApplication,
		)
		if ctx.Err() != nil {
			return bestServerQualityResponse{}, ctx.Err()
		}
		response.Partial = response.Partial || batch.Partial
		response.Available = response.Available || batch.Available
		if response.Recommendation == nil && batch.Recommendation != nil {
			copyValue := *batch.Recommendation
			response.Recommendation = &copyValue
		}
		response.Candidates = append(response.Candidates, filterMeasuredBestServerResults(batch.Candidates)...)
		if countMeasuredBestServerAlternatives(response.Candidates) >= bestServerMeasuredTarget && (cachedOK || hasMeasuredBestServerCurrent(response.Candidates)) {
			break
		}
	}
	if cachedOK {
		response.Candidates = append(response.Candidates, cachedCurrent)
	} else if candidate, ok := currentBestServerQualityCandidate(response); ok {
		storeBestServerCurrentQuality(currentEndpoint, currentFilter, candidate)
	}
	response = applyBestServerRecommendationDeadband(response)
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
