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
	bestServerCurrentScanTimeout     = 45 * time.Second
	bestServerComparisonTarget       = 3
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

func appendUniqueMeasuredBestServerResults(dst []bestServerQualityCandidate, candidates []bestServerQualityCandidate) []bestServerQualityCandidate {
	seen := make(map[string]bool, len(dst)+len(candidates))
	for _, candidate := range dst {
		seen[candidate.ID+"|"+candidate.Endpoint] = true
	}
	for _, candidate := range candidates {
		if candidate.Current || !candidate.Tested || candidate.DownloadMbps <= 0 || candidate.MediaSamples < bestServerMediaRequiredRuns {
			continue
		}
		key := candidate.ID + "|" + candidate.Endpoint
		if seen[key] {
			continue
		}
		seen[key] = true
		dst = append(dst, candidate)
	}
	return dst
}

func sortMeasuredBestServerResults(candidates []bestServerQualityCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
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
	if !cachedOK {
		currentResponse := a.scanActiveCurrentVPNQuality(ctx, currentEndpoint, currentFilter)
		if current, ok := currentBestServerQualityCandidate(currentResponse); ok && completeBestServerCurrentBaseline(current) {
			cachedCurrent = current
			cachedOK = true
		}
	}
	if cachedOK {
		if currentIndex := bestServerCurrentCandidateIndex(candidates, currentEndpoint, currentFilter); currentIndex >= 0 {
			candidates = withoutBestServerCandidate(candidates, currentIndex)
		}
	}

	// First compare the real application path through each candidate VPN with a
	// cheap bounded probe. Keep a reserve beyond the first deep-test batch so a
	// failed Speedtest sample does not leave the UI with only one or two rows.
	shortlisted := a.applicationAwareBestServerShortlist(ctx, candidates, currentEndpoint, currentFilter)
	response := bestServerQualityResponse{
		Partial: false, Success: true, Available: false, Candidates: []bestServerQualityCandidate{},
		ProfilesScanned: profilesScanned, ProfilesTotal: profilesScanned, ProfilesTruncated: truncated,
		Mutation: "NONE", ScannedAt: time.Now().UTC().Format(time.RFC3339), CurrentEndpoint: currentEndpoint,
	}
	measured := make([]bestServerQualityCandidate, 0, bestServerComparisonTarget)
	for offset := 0; offset < len(shortlisted) && len(measured) < bestServerComparisonTarget; offset += bestServerQualityShortlist {
		end := offset + bestServerQualityShortlist
		if end > len(shortlisted) {
			end = len(shortlisted)
		}
		batch := shortlisted[offset:end]
		batchResponse := rankBestServerQualityCandidates(
			ctx, batch, len(batch), false, currentEndpoint, currentFilter,
			defaultBestServerQualityTCPProbe, a.probeBestServerQualityApplication,
		)
		if ctx.Err() != nil {
			return bestServerQualityResponse{}, ctx.Err()
		}
		response.Partial = response.Partial || batchResponse.Partial
		measured = appendUniqueMeasuredBestServerResults(measured, batchResponse.Candidates)
	}
	sortMeasuredBestServerResults(measured)
	if len(measured) > bestServerComparisonTarget {
		measured = measured[:bestServerComparisonTarget]
	}
	response.Candidates = append(response.Candidates, measured...)
	if cachedOK {
		response.Candidates = append(response.Candidates, cachedCurrent)
	}

	// Seed the response with the best measured foreign option; the conservative
	// deadband below will keep current preferred unless the improvement is real.
	for i := range measured {
		if measured[i].Eligible {
			best := measured[i]
			response.Recommendation = &best
			response.Available = true
			break
		}
	}
	if cachedOK {
		response = applyBestServerRecommendationDeadband(response)
	} else {
		response.Available = false
		response.Recommendation = nil
		response.Message = "Текущий VPN не удалось полностью измерить. Варианты показаны только для сравнения; автоматическая рекомендация отключена."
	}

	if response.Message == "" {
		if response.Available && response.Recommendation != nil {
			if response.Recommendation.Current {
				response.Message = "Текущий VPN остаётся предпочтительным среди проверенных зарубежных профилей."
			} else {
				response.Message = "FreeNet нашёл зарубежный VPN с подтверждённым значимым улучшением."
			}
		} else {
			response.Message = "Достоверная рекомендация среди зарубежных профилей сейчас недоступна; текущий VPN не изменён."
		}
	}
	if cachedOK {
		response.Message += " Свежий подтверждённый замер текущего VPN использован как базовая точка сравнения."
	}
	if len(measured) < bestServerComparisonTarget {
		response.Message += " Полностью измеренных альтернатив: " + strconv.Itoa(len(measured)) + "."
	}
	if after := readBestServerCurrentEndpoint(a.cfg.OutPath); after != currentEndpoint {
		return bestServerQualityResponse{}, errors.New("VPN endpoint changed during Best Server scan")
	}
	if afterFilter := readBestServerCurrentFilter(a.cfg.FilterPath); afterFilter != currentFilter {
		return bestServerQualityResponse{}, errors.New("VPN profile identity changed during Best Server scan")
	}
	return response, nil
}
