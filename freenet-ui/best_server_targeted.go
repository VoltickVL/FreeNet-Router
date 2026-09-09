package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	bestServerTargetedScanTimeout = 40 * time.Second
	bestServerRefreshPlanTimeout  = 80 * time.Second
)

type bestServerTargetedResponse struct {
	Success   bool                       `json:"success"`
	Candidate *bestServerQualityCandidate `json:"candidate,omitempty"`
	Mutation  string                     `json:"mutation"`
	Message   string                     `json:"message,omitempty"`
	Error     string                     `json:"error,omitempty"`
}

type bestServerRefreshPlanResponse struct {
	Success         bool                        `json:"success"`
	Decision        string                      `json:"decision"`
	Reason          string                      `json:"reason,omitempty"`
	Mutation        string                      `json:"mutation"`
	CurrentEndpoint string                      `json:"current_endpoint,omitempty"`
	ProfileID       string                      `json:"profile_id,omitempty"`
	Current         *bestServerQualityCandidate `json:"current,omitempty"`
	Candidate       *bestServerQualityCandidate `json:"candidate,omitempty"`
	Message         string                      `json:"message,omitempty"`
	Error           string                      `json:"error,omitempty"`
}

func registerBestServerTargetedAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/vpn/candidate-quality", a.requireAuth(a.handleBestServerCandidateQuality))
	mux.HandleFunc("GET /api/vpn/current-refresh-plan", a.requireAuth(a.handleBestServerCurrentRefreshPlan))
}

func (a *app) handleBestServerCandidateQuality(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if !validProfileID(id) {
		writeJSON(w, http.StatusBadRequest, bestServerTargetedResponse{Success: false, Mutation: "NONE", Error: "invalid candidate id"})
		return
	}
	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	default:
		writeJSON(w, http.StatusConflict, bestServerTargetedResponse{Success: false, Mutation: "NONE", Error: "Another operation is active; candidate retry was not started"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), bestServerTargetedScanTimeout)
	defer cancel()
	candidate, err := a.scanBestServerCandidateQuality(ctx, id)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = http.StatusGatewayTimeout
		}
		writeJSON(w, status, bestServerTargetedResponse{Success: false, Mutation: "NONE", Error: safeBestServerError(err)})
		return
	}
	writeJSON(w, http.StatusOK, bestServerTargetedResponse{
		Success: true, Candidate: &candidate, Mutation: "NONE",
		Message: "Повторно проверен только выбранный VPN-сервер; текущий VPN не изменён.",
	})
}

func (a *app) scanBestServerCandidateQuality(ctx context.Context, id string) (bestServerQualityCandidate, error) {
	all, _, _, err := a.discoverBestServerCandidates(ctx)
	if err != nil {
		return bestServerQualityCandidate{}, err
	}
	var selected *bestServerInternalCandidate
	for i := range all {
		if all[i].Profile.ID == id {
			copyValue := all[i]
			selected = &copyValue
			break
		}
	}
	if selected == nil {
		return bestServerQualityCandidate{}, errors.New("candidate is no longer present in the current subscription")
	}
	response := rankBestServerQualityCandidates(
		ctx, []bestServerInternalCandidate{*selected}, 1, false, "", "",
		defaultBestServerQualityTCPProbe, a.probeBestServerQualityApplication,
	)
	if ctx.Err() != nil {
		return bestServerQualityCandidate{}, ctx.Err()
	}
	if len(response.Candidates) != 1 {
		return bestServerQualityCandidate{}, errors.New("candidate quality result is unavailable")
	}
	candidate := response.Candidates[0]
	candidate.Current = false
	return candidate, nil
}

func currentRefreshProfileLabel(candidates []bestServerInternalCandidate, currentEndpoint, exactLabel string) string {
	if label := strings.TrimSpace(sanitizeProfileName(exactLabel)); label != "" {
		return label
	}
	for _, candidate := range candidates {
		if endpointsEqual(profileEndpoint(candidate.Profile), currentEndpoint) {
			return strings.TrimSpace(sanitizeProfileName(candidate.Profile.Name))
		}
	}
	return ""
}

func freshCurrentProfileCandidate(candidates []bestServerInternalCandidate, currentEndpoint, label string) (bestServerInternalCandidate, bool) {
	label = strings.TrimSpace(sanitizeProfileName(label))
	if label == "" {
		return bestServerInternalCandidate{}, false
	}
	for _, candidate := range candidates {
		if !strings.EqualFold(strings.TrimSpace(sanitizeProfileName(candidate.Profile.Name)), label) {
			continue
		}
		if endpointsEqual(profileEndpoint(candidate.Profile), currentEndpoint) {
			continue
		}
		return candidate, true
	}
	return bestServerInternalCandidate{}, false
}

func (a *app) currentQualityForRefresh(ctx context.Context, endpoint, filter string) (bestServerQualityCandidate, bool) {
	if cached, ok := loadBestServerCurrentQuality(endpoint, filter); ok {
		return cached, true
	}
	response := a.scanActiveCurrentVPNQuality(ctx, endpoint, filter)
	candidate, ok := currentBestServerQualityCandidate(response)
	if ok {
		storeBestServerCurrentQuality(endpoint, filter, candidate)
	}
	return candidate, ok
}

func (a *app) handleBestServerCurrentRefreshPlan(w http.ResponseWriter, r *http.Request) {
	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	default:
		writeJSON(w, http.StatusConflict, bestServerRefreshPlanResponse{Success: false, Decision: "KEEP", Mutation: "NONE", Error: "Another operation is active; refresh check was not started"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), bestServerRefreshPlanTimeout)
	defer cancel()
	response, err := a.scanBestServerCurrentRefreshPlan(ctx)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = http.StatusGatewayTimeout
		}
		writeJSON(w, status, bestServerRefreshPlanResponse{Success: false, Decision: "KEEP", Mutation: "NONE", Error: safeBestServerError(err)})
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *app) scanBestServerCurrentRefreshPlan(ctx context.Context) (bestServerRefreshPlanResponse, error) {
	currentEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	currentFilter := readBestServerCurrentFilter(a.cfg.FilterPath)
	base := bestServerRefreshPlanResponse{Success: true, Decision: "KEEP", Mutation: "NONE", CurrentEndpoint: currentEndpoint}
	if strings.TrimSpace(currentEndpoint) == "" {
		base.Reason = "current_unknown"
		base.Message = "Текущий VPN endpoint не определён; обновление не выполнялось."
		return base, nil
	}

	all, _, _, err := a.discoverBestServerCandidates(ctx)
	if err != nil {
		return bestServerRefreshPlanResponse{}, err
	}
	label := currentRefreshProfileLabel(all, currentEndpoint, currentExactProfileLabel(a.cfg.FilterPath))
	if label == "" {
		base.Reason = "identity_unknown"
		base.Message = "Точную локацию текущего VPN не удалось определить; текущий endpoint сохранён."
		return base, nil
	}
	fresh, ok := freshCurrentProfileCandidate(all, currentEndpoint, label)
	if !ok {
		base.Reason = "no_new_endpoint"
		base.Message = "Свежего endpoint для текущей локации в подписке нет; текущий VPN оставлен без изменений."
		return base, nil
	}

	current, currentOK := a.currentQualityForRefresh(ctx, currentEndpoint, currentFilter)
	if !currentOK {
		base.Reason = "current_quality_unknown"
		base.Message = "Не удалось получить безопасный baseline текущего VPN; свежий endpoint не применяется."
		return base, nil
	}
	base.Current = &current

	measured := rankBestServerQualityCandidates(
		ctx, []bestServerInternalCandidate{fresh}, 1, false, "", "",
		defaultBestServerQualityTCPProbe, a.probeBestServerQualityApplication,
	)
	if ctx.Err() != nil {
		return bestServerRefreshPlanResponse{}, ctx.Err()
	}
	if len(measured.Candidates) != 1 {
		base.Reason = "candidate_quality_unknown"
		base.Message = "Свежий endpoint найден, но его качество не удалось подтвердить; текущий VPN сохранён."
		return base, nil
	}
	candidate := measured.Candidates[0]
	candidate.Current = false
	base.Candidate = &candidate
	base.ProfileID = candidate.ID
	if !candidate.Eligible {
		base.Reason = "candidate_failed"
		base.Message = "Свежий endpoint найден, но не прошёл проверку качества; текущий VPN сохранён."
	} else if !bestServerMeaningfullyBetter(current, candidate) {
		base.Reason = "current_preferred"
		base.Message = "Свежий endpoint проверен, но не лучше текущего VPN; переключение не требуется."
	} else {
		base.Decision = "APPLY"
		base.Reason = "candidate_better"
		base.Message = "Свежий endpoint этой же локации проверен и значимо лучше текущего; можно безопасно применить его."
	}

	if after := readBestServerCurrentEndpoint(a.cfg.OutPath); !endpointsEqual(after, currentEndpoint) {
		return bestServerRefreshPlanResponse{}, errors.New("VPN endpoint changed during current refresh check")
	}
	if afterFilter := readBestServerCurrentFilter(a.cfg.FilterPath); afterFilter != currentFilter {
		return bestServerRefreshPlanResponse{}, errors.New("VPN profile identity changed during current refresh check")
	}
	return base, nil
}
