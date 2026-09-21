package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	bestServerTargetedRetryTimeout     = 75 * time.Second
	bestServerRefreshTimeout           = 150 * time.Second
	bestServerEndpointReadinessTimeout = 20 * time.Second
)

type bestServerRefreshRequest struct {
	Confirm bool `json:"confirm"`
}

type endpointRefreshSnapshot struct {
	base          snapshot
	profilePath   string
	profileExists bool
	profile       []byte
	profileMode   os.FileMode
}

func endpointRefreshProfilePath() string {
	if path := strings.TrimSpace(os.Getenv("FREENET_PROFILE_FILE")); path != "" {
		return path
	}
	return "/opt/etc/freenet/vpn_profile_name"
}

func (a *app) takeEndpointRefreshSnapshot() (endpointRefreshSnapshot, error) {
	base, err := a.takeSnapshot()
	if err != nil {
		return endpointRefreshSnapshot{}, err
	}
	s := endpointRefreshSnapshot{base: base, profilePath: endpointRefreshProfilePath()}
	info, err := os.Stat(s.profilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return endpointRefreshSnapshot{}, err
	}
	s.profileMode = info.Mode().Perm()
	s.profile, err = os.ReadFile(s.profilePath)
	if err != nil {
		return endpointRefreshSnapshot{}, err
	}
	s.profileExists = true
	return s, nil
}

func (a *app) restoreEndpointRefreshFiles(s endpointRefreshSnapshot) error {
	if s.base.filterExists {
		if err := atomicWrite(a.cfg.FilterPath, s.base.filter, 0644); err != nil {
			return err
		}
	} else if err := os.Remove(a.cfg.FilterPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	mode := s.base.outMode
	if mode == 0 {
		mode = 0600
	}
	if err := atomicWrite(a.cfg.OutPath, s.base.out, mode); err != nil {
		return err
	}

	if s.profileExists {
		profileMode := s.profileMode
		if profileMode == 0 {
			profileMode = 0600
		}
		if err := atomicWrite(s.profilePath, s.profile, profileMode); err != nil {
			return err
		}
	} else if err := os.Remove(s.profilePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

var bestServerEndpointTCPProbe = defaultBestServerTCPProbe
var bestServerEndpointApplicationProbe = func(a *app, ctx context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
	return a.probeBestServerApplication(ctx, candidate)
}

func (a *app) probeBestServerFreshEndpointReadiness(parent context.Context, candidate bestServerInternalCandidate) *bestServerQualityCandidate {
	ctx, cancel := context.WithTimeout(parent, bestServerEndpointReadinessTimeout)
	defer cancel()

	tcpCh := make(chan bestServerProbeResult, 1)
	appCh := make(chan bestServerProbeResult, 1)
	go func() { tcpCh <- bestServerEndpointTCPProbe(ctx, candidate.Profile) }()
	go func() { appCh <- bestServerEndpointApplicationProbe(a, ctx, candidate) }()

	tcp := <-tcpCh
	app := <-appCh
	result := &bestServerQualityCandidate{
		ID: candidate.Profile.ID, Name: candidate.Profile.Name, CountryCode: candidate.Profile.CountryCode,
		Endpoint: profileEndpoint(candidate.Profile),
		Tested: ctx.Err() == nil,
		Reachable: tcp.OK,
		TCPRTTMS: tcp.Median, TCPJitterMS: tcp.Jitter,
		ApplicationMS: app.Median, JitterMS: app.Jitter,
		HTTPSamples: len(app.Samples),
	}
	result.Available = result.Tested && tcp.OK && app.OK
	result.Eligible = result.Available
	switch {
	case result.Eligible:
		result.Confidence = "high"
		result.Reason = "fresh endpoint TCP and isolated VPN application probes passed"
	case ctx.Err() != nil:
		result.Reason = "fresh endpoint readiness timed out"
	case !tcp.OK:
		result.Reason = "fresh endpoint TCP probe failed"
	default:
		result.Reason = "fresh endpoint isolated VPN application probe failed"
	}
	return result
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

	var current *bestServerQualityCandidate
	if cached, ok := loadBestServerCurrentQuality(currentEndpoint, currentFilter); ok {
		copyValue := cached
		current = &copyValue
	}

	candidate := a.probeBestServerFreshEndpointReadiness(ctx, fresh)
	if ctx.Err() != nil || candidate == nil || !candidate.Tested {
		return http.StatusGatewayTimeout, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Applied: false, Mutation: "NONE", Current: current, Candidate: candidate,
			RollbackState: "NOT_APPLIED", Error: "fresh endpoint readiness timed out before mutation; current VPN was preserved",
		}
	}
	if !candidate.Available || !candidate.Eligible {
		return http.StatusOK, bestServerRefreshResponse{
			Success: true, Outcome: "check_failed", Applied: false, Mutation: "NONE", Current: current, Candidate: candidate, RollbackState: "NOT_NEEDED",
			Message: "Свежий endpoint текущего профиля не прошёл быструю isolated VPN readiness-проверку. Текущий VPN сохранён.",
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
	snap, err := a.takeEndpointRefreshSnapshot()
	if err != nil {
		return http.StatusInternalServerError, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Applied: false, Mutation: "NONE", RollbackState: "NOT_APPLIED",
			PrimaryError: "cannot create pre-apply VPN snapshot", Error: "refresh stopped before mutation",
		}
	}

	applyCtx, cancel := context.WithTimeout(context.Background(), a.cfg.Timeout)
	output, cmdErr := runCommand(applyCtx, providerHelperPath(), "apply-core", target.Profile.ID)
	cancel()
	safeOutput := sanitizeOutput(string(output))
	if cmdErr != nil {
		primary, rollback := classifyApplyFailure(safeOutput)
		if primary == "" {
			primary = cmdErr.Error()
		}
		if rollback == "UNKNOWN" || rollback == "FAILED/UNKNOWN" {
			if rbErr := a.restoreEndpointRefreshSnapshotCoreOnly(snap); rbErr == nil {
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
	expectedLabel := sanitizeProfileName(target.Profile.Name)
	if expectedLabel != "" {
		postOK = postOK && activeLabel == expectedLabel
	}
	if !postOK {
		rollback := "SUCCESS"
		if rbErr := a.restoreEndpointRefreshSnapshotCoreOnly(snap); rbErr != nil {
			rollback = "FAILED/UNKNOWN"
		}
		return http.StatusBadGateway, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Applied: false, Mutation: "ROLLED_BACK",
			PrimaryError: "post-apply exact endpoint acceptance failed", RollbackState: rollback,
			Error: "post-apply проверка не подтвердила свежий endpoint; выполнен откат к предыдущему VPN",
		}
	}

	probeCtx, cancelProbe := context.WithTimeout(context.Background(), automationHealthProbeTimeout)
	postProbe := a.probeAutomationCurrentVPN(probeCtx)
	cancelProbe()
	if postProbe.State != automationHealthHealthy {
		rollback := "SUCCESS"
		if rbErr := a.restoreEndpointRefreshSnapshotCoreOnly(snap); rbErr != nil {
			rollback = "FAILED/UNKNOWN"
		}
		return http.StatusBadGateway, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Applied: false, Mutation: "ROLLED_BACK",
			PrimaryError: "post-apply VPN Internet probe did not confirm the refreshed endpoint", RollbackState: rollback,
			Error: "свежий endpoint применён, но проверка доступа через VPN не подтвердилась; выполнен откат либо операция остановлена для проверки состояния",
		}
	}

	return http.StatusOK, bestServerRefreshResponse{
		Success: true, Outcome: "applied", Applied: true, Mutation: "APPLIED", RollbackState: "NOT_NEEDED",
		Message: "Свежий endpoint текущего VPN проверен, применён и подтверждён доступом через VPN.",
	}
}

func (a *app) restoreEndpointRefreshSnapshotCoreOnly(s endpointRefreshSnapshot) error {
	if err := a.restoreEndpointRefreshFiles(s); err != nil {
		return err
	}
	restartCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := runCommand(restartCtx, providerHelperPath(), "core-restart")
	if err != nil {
		return errors.New("core-only Xray rollback restart failed: " + sanitizeOutput(string(output)))
	}
	return nil
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
	matches := make([]bestServerInternalCandidate, 0, 2)
	exact := make([]bestServerInternalCandidate, 0, 2)
	for i := range candidates {
		candidate := candidates[i]
		if endpointsEqual(profileEndpoint(candidate.Profile), currentEndpoint) || !matcher.MatchString(candidate.Profile.Name) {
			continue
		}
		matches = append(matches, candidate)
		if exactLabel != "" && sanitizeProfileName(candidate.Profile.Name) == exactLabel {
			exact = append(exact, candidate)
		}
	}
	if exactLabel != "" {
		if len(exact) == 1 {
			return exact[0], true
		}
		if len(exact) > 1 {
			return bestServerInternalCandidate{}, false
		}
	}
	if len(matches) == 1 {
		return matches[0], true
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
