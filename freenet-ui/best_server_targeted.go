package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	bestServerTargetedRetryTimeout = 42 * time.Second
	bestServerRefreshTimeout       = 210 * time.Second
)

type bestServerRefreshRequest struct {
	Confirm bool `json:"confirm"`
}

type bestServerRefreshResponse struct {
	Success       bool                        `json:"success"`
	Outcome       string                      `json:"outcome"`
	Applied       bool                        `json:"applied"`
	Mutation      string                      `json:"mutation"`
	Current       *bestServerQualityCandidate `json:"current,omitempty"`
	Candidate     *bestServerQualityCandidate `json:"candidate,omitempty"`
	Message       string                      `json:"message,omitempty"`
	PrimaryError  string                      `json:"primary_error,omitempty"`
	RollbackState string                      `json:"rollback_state,omitempty"`
	Error         string                      `json:"error,omitempty"`
}

func registerBestServerTargetedAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/vpn/best-candidate", a.requireAuth(a.handleBestServerCandidateRetry))
	mux.HandleFunc("POST /api/vpn/current-refresh", a.requireAuth(a.handleBestServerCurrentRefresh))
}

func (a *app) handleBestServerCandidateRetry(w http.ResponseWriter, r *http.Request) {
	if len(a.sem) > 0 {
		writeJSON(w, http.StatusConflict, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Error: "VPN operation is active; targeted retry was not started",
		})
		return
	}
	profileID := strings.TrimSpace(r.URL.Query().Get("id"))
	if !validProfileID(profileID) {
		writeJSON(w, http.StatusBadRequest, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Error: "invalid candidate id",
		})
		return
	}

	currentEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	currentFilter := readBestServerCurrentFilter(a.cfg.FilterPath)
	ctx, cancel := context.WithTimeout(r.Context(), bestServerTargetedRetryTimeout)
	defer cancel()

	all, _, _, err := a.discoverBestServerCandidates(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Error: safeBestServerError(err),
		})
		return
	}
	target, ok := bestServerCandidateByID(all, profileID)
	if !ok {
		writeJSON(w, http.StatusNotFound, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Message: "Этот endpoint больше не найден в свежей подписке. Текущий VPN не изменён.",
			Error: "candidate is no longer present in the subscription",
		})
		return
	}
	if endpointsEqual(profileEndpoint(target.Profile), currentEndpoint) {
		writeJSON(w, http.StatusConflict, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Error: "targeted retry candidate is already the active endpoint",
		})
		return
	}

	response := rankBestServerQualityCandidates(
		ctx,
		[]bestServerInternalCandidate{target},
		1,
		false,
		currentEndpoint,
		currentFilter,
		defaultBestServerQualityTCPProbe,
		a.probeBestServerQualityApplication,
	)
	if ctx.Err() != nil {
		status := http.StatusGatewayTimeout
		if errors.Is(ctx.Err(), context.Canceled) {
			status = http.StatusRequestTimeout
		}
		writeJSON(w, status, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Error: safeBestServerError(ctx.Err()),
		})
		return
	}
	if current, cached := loadBestServerCurrentQuality(currentEndpoint, currentFilter); cached {
		response.Candidates = append([]bestServerQualityCandidate{current}, response.Candidates...)
	}
	response.Success = true
	response.Mutation = "NONE"
	response.ScannedAt = time.Now().UTC().Format(time.RFC3339)
	response.CurrentEndpoint = currentEndpoint
	response.ProfilesScanned = 1
	response.ProfilesTotal = 1
	response = applyBestServerRecommendationDeadband(response)
	if candidate := qualityCandidateByID(response.Candidates, profileID); candidate != nil {
		if candidate.Eligible {
			response.Message = "Сервер проверен повторно и прошёл все проверки. Текущий VPN не изменён."
		} else {
			response.Message = "Сервер проверен повторно, но пока не подходит для переключения. Текущий VPN не изменён."
		}
	}

	if after := readBestServerCurrentEndpoint(a.cfg.OutPath); after != currentEndpoint {
		writeJSON(w, http.StatusConflict, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Error: "VPN endpoint changed during targeted retry; result discarded",
		})
		return
	}
	if afterFilter := readBestServerCurrentFilter(a.cfg.FilterPath); afterFilter != currentFilter {
		writeJSON(w, http.StatusConflict, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Error: "VPN profile identity changed during targeted retry; result discarded",
		})
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *app) handleBestServerCurrentRefresh(w http.ResponseWriter, r *http.Request) {
	if a.mutationBlockedBySelfUpdate(w) {
		return
	}
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, bestServerRefreshResponse{Success: false, Outcome: "check_failed", Mutation: "NONE", Error: "cross-origin request rejected"})
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, bestServerRefreshResponse{Success: false, Outcome: "check_failed", Mutation: "NONE", Error: "application/json required"})
		return
	}
	body := http.MaxBytesReader(w, r.Body, 512)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req bestServerRefreshRequest
	if err := dec.Decode(&req); err != nil || !req.Confirm {
		writeJSON(w, http.StatusBadRequest, bestServerRefreshResponse{Success: false, Outcome: "check_failed", Mutation: "NONE", Error: "explicit confirmation required"})
		return
	}

	op, leader, conflict := vpnOperations.begin("refresh", "current")
	if !leader {
		if conflict != nil {
			writeJSON(w, http.StatusConflict, operationConflictPayload("другая VPN-операция уже выполняется", *conflict))
			return
		}
		status, payload, ok := vpnOperations.wait(r.Context(), op)
		if !ok {
			return
		}
		result, payloadOK := payload.(bestServerRefreshResponse)
		if !payloadOK {
			writeJSON(w, http.StatusInternalServerError, bestServerRefreshResponse{Success: false, Outcome: "check_failed", Mutation: "NONE", Error: "operation result unavailable"})
			return
		}
		writeJSON(w, status, result)
		return
	}

	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(bestServerRefreshTimeout + 15*time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		result := bestServerRefreshResponse{Success: false, Outcome: "check_failed", Mutation: "NONE", Error: "unable to prepare bounded refresh response deadline"}
		vpnOperations.finish(op, http.StatusServiceUnavailable, result, false, "", result.Error)
		writeJSON(w, http.StatusServiceUnavailable, result)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), bestServerRefreshTimeout)
	defer cancel()
	status, result := a.executeBestServerCurrentRefresh(ctx)
	vpnOperations.finish(op, status, result, result.Success, result.Message, result.Error)
	writeJSON(w, status, result)
}

func (a *app) executeBestServerCurrentRefresh(ctx context.Context) (int, bestServerRefreshResponse) {
	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	default:
		return http.StatusConflict, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Mutation: "NONE", RollbackState: "NOT_APPLIED",
			Error: "another FreeNet operation is already running",
		}
	}

	currentEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	currentFilter := readBestServerCurrentFilter(a.cfg.FilterPath)
	if currentEndpoint == "" || currentFilter == "" {
		return http.StatusConflict, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Mutation: "NONE", RollbackState: "NOT_APPLIED",
			Error: "current VPN identity is incomplete; refresh stopped without mutation",
		}
	}
	matcher, err := regexp.Compile(currentFilter)
	if err != nil {
		return http.StatusConflict, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Mutation: "NONE", RollbackState: "NOT_APPLIED",
			Error: "current VPN profile filter is invalid; refresh stopped without mutation",
		}
	}

	all, _, _, err := a.discoverBestServerCandidates(ctx)
	if err != nil {
		return http.StatusServiceUnavailable, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Mutation: "NONE", RollbackState: "NOT_APPLIED",
			Error: "fresh subscription could not be read; current VPN was preserved",
		}
	}
	fresh, ok := bestServerFreshCandidateForCurrent(all, matcher, currentExactProfileLabel(a.cfg.FilterPath), currentEndpoint)
	if !ok {
		return http.StatusOK, bestServerRefreshResponse{
			Success: true, Outcome: "no_new", Applied: false, Mutation: "NONE", RollbackState: "NOT_NEEDED",
			Message: "Нового endpoint для текущей локации нет. Текущий VPN сохранён.",
		}
	}

	currentResponse := a.scanActiveCurrentVPNQuality(ctx, currentEndpoint, currentFilter)
	current := currentCandidateFromQuality(currentResponse)
	if current == nil || !completeBestServerCurrentBaseline(*current) {
		return http.StatusOK, bestServerRefreshResponse{
			Success: true, Outcome: "check_failed", Applied: false, Mutation: "NONE", Current: current, RollbackState: "NOT_NEEDED",
			Message: "Не удалось получить сопоставимый baseline текущего VPN. Переключение не выполнялось.",
		}
	}

	candidateResponse := rankBestServerQualityCandidates(
		ctx,
		[]bestServerInternalCandidate{fresh},
		1,
		false,
		currentEndpoint,
		currentFilter,
		defaultBestServerQualityTCPProbe,
		a.probeBestServerQualityApplication,
	)
	candidate := qualityCandidateByID(candidateResponse.Candidates, fresh.Profile.ID)
	if ctx.Err() != nil {
		return http.StatusGatewayTimeout, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Applied: false, Mutation: "NONE", Current: current, Candidate: candidate,
			RollbackState: "NOT_APPLIED", Error: "quality-gated refresh timed out before mutation; current VPN was preserved",
		}
	}
	if candidate == nil || !candidate.Tested || !candidate.Eligible {
		return http.StatusOK, bestServerRefreshResponse{
			Success: true, Outcome: "check_failed", Applied: false, Mutation: "NONE", Current: current, Candidate: candidate, RollbackState: "NOT_NEEDED",
			Message: "Свежий endpoint не прошёл проверку качества. Текущий VPN сохранён.",
		}
	}
	if !bestServerMeaningfullyBetter(*current, *candidate) {
		return http.StatusOK, bestServerRefreshResponse{
			Success: true, Outcome: "current_better", Applied: false, Mutation: "NONE", Current: current, Candidate: candidate, RollbackState: "NOT_NEEDED",
			Message: "Свежий endpoint проверен, но текущий VPN лучше или разница несущественна. Переключение не выполнялось.",
		}
	}

	if readBestServerCurrentEndpoint(a.cfg.OutPath) != currentEndpoint || readBestServerCurrentFilter(a.cfg.FilterPath) != currentFilter {
		return http.StatusConflict, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Applied: false, Mutation: "NONE", Current: current, Candidate: candidate, RollbackState: "NOT_APPLIED",
			Error: "current VPN changed during quality decision; refresh stopped without mutation",
		}
	}

	applyStatus, applied := a.applyBestServerRefreshCandidate(ctx, fresh)
	applied.Current = current
	applied.Candidate = candidate
	return applyStatus, applied
}

func (a *app) applyBestServerRefreshCandidate(ctx context.Context, target bestServerInternalCandidate) (int, bestServerRefreshResponse) {
	plan, err := a.runProviderPlan(target.Profile.ID)
	if err != nil || !plan.CandidateValid || plan.Mutation != "NONE" || !endpointsEqual(plan.Endpoint, profileEndpoint(target.Profile)) {
		return http.StatusConflict, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Applied: false, Mutation: "NONE", RollbackState: "NOT_APPLIED",
			PrimaryError: "fresh endpoint did not pass exact provider plan validation",
			Error: "fresh endpoint validation failed; current VPN was preserved",
		}
	}

	snap, err := a.takeSnapshot()
	if err != nil {
		return http.StatusInternalServerError, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Applied: false, Mutation: "NONE", RollbackState: "NOT_APPLIED",
			PrimaryError: "cannot create pre-apply VPN snapshot", Error: "refresh stopped before mutation",
		}
	}

	applyCtx, cancel := context.WithTimeout(ctx, a.cfg.Timeout)
	output, cmdErr := runCommand(applyCtx, providerHelperPath(), "apply", target.Profile.ID)
	cancel()
	safeOutput := sanitizeOutput(string(output))
	if cmdErr != nil {
		primary, rollback := classifyApplyFailure(safeOutput)
		if primary == "" {
			primary = cmdErr.Error()
		}
		if rollback == "UNKNOWN" || rollback == "FAILED/UNKNOWN" {
			if rbErr := a.restoreSnapshot(snap); rbErr == nil {
				rollback = "SUCCESS"
			} else {
				rollback = "FAILED/UNKNOWN"
			}
		}
		return http.StatusBadGateway, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Applied: false, Mutation: "ROLLED_BACK",
			PrimaryError: primary, RollbackState: rollback,
			Error: "свежий endpoint не был принят; текущий VPN восстановлен либо операция остановлена для проверки состояния",
		}
	}

	expectedEndpoint := profileEndpoint(target.Profile)
	activeEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	activeLabel := currentExactProfileLabel(a.cfg.FilterPath)
	postOK := endpointsEqual(activeEndpoint, expectedEndpoint) && processRunning("xray")
	if plan.ProfileName != "" {
		postOK = postOK && activeLabel == sanitizeProfileName(plan.ProfileName)
	}
	if !postOK {
		rollback := "SUCCESS"
		if rbErr := a.restoreSnapshot(snap); rbErr != nil {
			rollback = "FAILED/UNKNOWN"
		}
		return http.StatusBadGateway, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Applied: false, Mutation: "ROLLED_BACK",
			PrimaryError: "post-apply exact endpoint acceptance failed", RollbackState: rollback,
			Error: "post-apply проверка не подтвердила свежий endpoint; выполнен откат к предыдущему VPN",
		}
	}

	return http.StatusOK, bestServerRefreshResponse{
		Success: true, Outcome: "applied", Applied: true, Mutation: "APPLIED", RollbackState: "NOT_NEEDED",
		Message: "Свежий endpoint проверен, оказался заметно лучше и применён. Соединение подтверждено.",
	}
}

func bestServerCandidateByID(candidates []bestServerInternalCandidate, id string) (bestServerInternalCandidate, bool) {
	for _, candidate := range candidates {
		if candidate.Profile.ID == id {
			return candidate, true
		}
	}
	return bestServerInternalCandidate{}, false
}

func bestServerFreshCandidateForCurrent(candidates []bestServerInternalCandidate, matcher *regexp.Regexp, exactLabel, currentEndpoint string) (bestServerInternalCandidate, bool) {
	if matcher == nil {
		return bestServerInternalCandidate{}, false
	}
	exactLabel = sanitizeProfileName(exactLabel)
	var fallback *bestServerInternalCandidate
	for i := range candidates {
		candidate := candidates[i]
		if endpointsEqual(profileEndpoint(candidate.Profile), currentEndpoint) || !matcher.MatchString(candidate.Profile.Name) {
			continue
		}
		if exactLabel != "" && sanitizeProfileName(candidate.Profile.Name) == exactLabel {
			return candidate, true
		}
		if fallback == nil {
			copyValue := candidate
			fallback = &copyValue
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return bestServerInternalCandidate{}, false
}

func qualityCandidateByID(candidates []bestServerQualityCandidate, id string) *bestServerQualityCandidate {
	for i := range candidates {
		if candidates[i].ID == id {
			copyValue := candidates[i]
			return &copyValue
		}
	}
	return nil
}

func currentCandidateFromQuality(response bestServerQualityResponse) *bestServerQualityCandidate {
	for i := range response.Candidates {
		if response.Candidates[i].Current {
			copyValue := response.Candidates[i]
			return &copyValue
		}
	}
	return nil
}
