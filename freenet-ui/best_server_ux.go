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
	bestServerCurrentScanTimeout       = 75 * time.Second
	bestServerMeasuredBatchSize        = 1
	bestServerVisibleAlternatives      = 3
	bestServerMinimumDeepAttemptBudget = 8 * time.Second
)

// bestServerAttemptContext keeps parent cancellation but hides the absolute
// deadline from the generic quality ranker. The outer foreign-scan loop owns
// the remaining-budget decision, so a real third candidate may still be
// attempted with the bounded time left instead of being rejected merely
// because a full 55 s candidate window no longer fits.
type bestServerAttemptContext struct{ context.Context }

func (bestServerAttemptContext) Deadline() (time.Time, bool) { return time.Time{}, false }

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

// Keep every candidate that completed a real deep probe so the UI can explain
// why a measured near-miss was rejected. Eligibility still controls
// recommendation and apply; retaining diagnostics here never makes a failed
// candidate switchable.
func filterMeasuredBestServerResults(candidates []bestServerQualityCandidate) []bestServerQualityCandidate {
	filtered := make([]bestServerQualityCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Current || (candidate.Tested && (candidate.Available || candidate.Reachable)) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

// The browser presents up to three real measured comparison cards. A card does
// not need to be eligible to occupy a comparison slot: a measured rejected
// result is still useful diagnostic evidence and remains non-switchable.
// Eligibility controls recommendation/apply only. Public endpoint is used here
// solely for presentation diversity; it is not logical profile identity.
func measuredBestServerAlternativeCount(candidates []bestServerQualityCandidate, currentEndpoint string) int {
	seenEndpoints := map[string]struct{}{}
	if endpoint := strings.TrimSpace(currentEndpoint); endpoint != "" {
		seenEndpoints[endpoint] = struct{}{}
	}
	count := 0
	for _, candidate := range candidates {
		if candidate.Current || !candidate.Tested || (!candidate.Available && !candidate.Reachable) {
			continue
		}
		endpoint := strings.TrimSpace(candidate.Endpoint)
		if endpoint == "" {
			continue
		}
		if _, exists := seenEndpoints[endpoint]; exists {
			continue
		}
		seenEndpoints[endpoint] = struct{}{}
		count++
	}
	return count
}

// Partial is a user-facing completion signal, not a statement that the scan
// was intentionally non-exhaustive. Once the browser-visible Top-3 target is
// complete, a short remaining budget must not turn a successful result into a
// false timeout warning. Conversely, natural candidate exhaustion below three
// is complete (not partial) when budget did not stop the scan.
func bestServerCompletionPartial(budgetLimited bool, candidates []bestServerQualityCandidate, currentEndpoint string) bool {
	return budgetLimited && measuredBestServerAlternativeCount(candidates, currentEndpoint) < bestServerVisibleAlternatives
}

func sortMeasuredBestServerResults(candidates []bestServerQualityCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.Current != b.Current {
			return !a.Current
		}
		if a.Eligible != b.Eligible {
			return a.Eligible
		}
		if a.Available != b.Available {
			return a.Available
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.ApplicationMS != b.ApplicationMS {
			if a.ApplicationMS == 0 {
				return false
			}
			if b.ApplicationMS == 0 {
				return true
			}
			return a.ApplicationMS < b.ApplicationMS
		}
		if a.DownloadMbps != b.DownloadMbps {
			return a.DownloadMbps > b.DownloadMbps
		}
		return a.ID < b.ID
	})
}

func (a *app) rankMeasuredBestServerBatches(
	ctx context.Context,
	candidates []bestServerInternalCandidate,
	profilesScanned int,
	truncated bool,
	currentEndpoint string,
	currentFilter string,
) bestServerQualityResponse {
	aggregate := bestServerQualityResponse{
		Candidates: []bestServerQualityCandidate{}, ProfilesScanned: profilesScanned, ProfilesTotal: profilesScanned,
		ProfilesTruncated: truncated, Mutation: "NONE",
	}
	budgetLimited := false
	for start := 0; start < len(candidates); start += bestServerMeasuredBatchSize {
		if measuredBestServerAlternativeCount(aggregate.Candidates, currentEndpoint) >= bestServerVisibleAlternatives {
			break
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < bestServerMinimumDeepAttemptBudget {
			budgetLimited = true
			break
		}
		end := start + bestServerMeasuredBatchSize
		if end > len(candidates) {
			end = len(candidates)
		}
		batch := rankBestServerQualityCandidates(
			bestServerAttemptContext{Context: ctx}, candidates[start:end], profilesScanned, truncated, currentEndpoint, currentFilter,
			defaultBestServerQualityTCPProbe, a.probeBestServerQualityApplication,
		)
		// If the parent job deadline fired during this deep attempt, its final
		// classification is not trustworthy enough for a comparison card. Drop
		// that batch fail-closed instead of presenting a deadline interruption as
		// an ordinary fully-tested hard failure.
		if ctx.Err() != nil {
			budgetLimited = true
			break
		}
		if batch.Partial {
			budgetLimited = true
		}
		aggregate.Candidates = append(aggregate.Candidates, filterMeasuredBestServerResults(batch.Candidates)...)
	}
	aggregate.Partial = bestServerCompletionPartial(budgetLimited, aggregate.Candidates, currentEndpoint)
	sortMeasuredBestServerResults(aggregate.Candidates)
	for _, candidate := range aggregate.Candidates {
		if candidate.Eligible && !candidate.Current {
			best := candidate
			aggregate.Recommendation = &best
			aggregate.Available = true
			break
		}
	}
	if aggregate.Recommendation == nil {
		for _, candidate := range aggregate.Candidates {
			if candidate.Eligible {
				best := candidate
				aggregate.Recommendation = &best
				aggregate.Available = true
				break
			}
		}
	}
	return aggregate
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
	profilesScanned := len(candidates)

	// "Проверить текущий VPN" is a separate explicit operation. Best Server may
	// reuse a fresh complete current measurement, but it must not spend the
	// alternatives job budget on an implicit heavy current Speedtest. This keeps
	// the bounded 180 s browser contract focused on producing the Top-3 cards.
	currentBaseline, currentBaselineOK := loadBestServerCurrentQuality(currentEndpoint, currentFilter)
	if currentIndex := bestServerCurrentCandidateIndex(candidates, currentEndpoint, currentFilter); currentIndex >= 0 {
		candidates = withoutBestServerCandidate(candidates, currentIndex)
	}
	if len(candidates) == 0 {
		currentCandidates := []bestServerQualityCandidate{}
		if currentBaselineOK {
			currentCandidates = append(currentCandidates, currentBaseline)
		}
		return bestServerQualityResponse{
			Success: true, Available: currentBaselineOK && currentBaseline.Eligible, Candidates: currentCandidates, ProfilesScanned: profilesScanned, ProfilesTotal: profilesScanned,
			ProfilesTruncated: truncated, Mutation: "NONE", ScannedAt: time.Now().UTC().Format(time.RFC3339), CurrentEndpoint: currentEndpoint,
			Message: "Подходящих зарубежных Extra-профилей нет; текущий VPN не изменён.",
		}, nil
	}

	// First compare the real application path through each candidate VPN. Keep a
	// reserve shortlist, then deep-test one logical profile per batch. Stop after
	// three browser-visible measured alternatives (eligible first, diagnostics
	// allowed), or when the bounded job budget can no longer start a real probe.
	candidates = a.applicationAwareBestServerShortlist(ctx, candidates, currentEndpoint, currentFilter)
	response := a.rankMeasuredBestServerBatches(ctx, candidates, profilesScanned, truncated, currentEndpoint, currentFilter)
	if ctx.Err() != nil && len(response.Candidates) == 0 && !currentBaselineOK {
		return bestServerQualityResponse{}, ctx.Err()
	}
	if currentBaselineOK {
		response.Candidates = append(response.Candidates, currentBaseline)
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
			response.Message = "FreeNet нашёл лучший зарубежный VPN-профиль среди измеренных вариантов."
		}
	} else {
		response.Message = "Достоверная рекомендация среди зарубежных профилей сейчас недоступна; текущий VPN не изменён."
	}
	if currentBaselineOK {
		response.Message += " Свежий подтверждённый замер текущего VPN переиспользован без повторной тяжёлой Speedtest-проверки."
	} else {
		response.Message += " Текущий VPN не перепроверялся автоматически: для него есть отдельная кнопка «Проверить текущий VPN»."
	}
	if after := readBestServerCurrentEndpoint(a.cfg.OutPath); after != currentEndpoint {
		return bestServerQualityResponse{}, errors.New("VPN endpoint changed during Best Server scan")
	}
	if afterFilter := readBestServerCurrentFilter(a.cfg.FilterPath); afterFilter != currentFilter {
		return bestServerQualityResponse{}, errors.New("VPN profile identity changed during Best Server scan")
	}
	return response, nil
}
