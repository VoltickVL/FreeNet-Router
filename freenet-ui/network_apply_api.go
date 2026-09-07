package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	defaultNetworkHelperPath  = "/opt/lib/freenet/apply_network_profile.sh"
	defaultProviderHelperPath = "/opt/lib/freenet/apply_provider_profile.sh"
)

type providerPlanResponse struct {
	Success         bool   `json:"success"`
	ProfileID       string `json:"profile_id,omitempty"`
	ProfileName     string `json:"profile_name,omitempty"`
	Endpoint        string `json:"endpoint,omitempty"`
	CurrentOutbound string `json:"current_outbound,omitempty"`
	XrayRunning     bool   `json:"xray_running"`
	CandidateValid  bool   `json:"candidate_xray_valid"`
	ExpectedDelta   string `json:"expected_delta,omitempty"`
	ExpectedNoDelta string `json:"expected_no_delta,omitempty"`
	Mutation        string `json:"mutation,omitempty"`
	Error           string `json:"error,omitempty"`
}

type networkPlanResponse struct {
	Success                           bool                       `json:"success"`
	Supported                         bool                       `json:"supported"`
	Active                            bool                       `json:"active"`
	ISP                               string                     `json:"isp"`
	DNSMode                           string                     `json:"dns_mode"`
	ActiveISP                         string                     `json:"active_isp,omitempty"`
	ActiveDNSMode                     string                     `json:"active_dns_mode,omitempty"`
	NativeDNSProvider                 string                     `json:"native_dns_provider,omitempty"`
	ActiveNativeDNSProvider           string                     `json:"active_native_dns_provider,omitempty"`
	NativeDNSProviderOptions          []nativeDNSProviderOption  `json:"native_dns_provider_options,omitempty"`
	EffectiveDNSMode                  string                     `json:"effective_dns_mode"`
	Reason                            string                     `json:"reason,omitempty"`
	ProxyDNS                          string                     `json:"proxy_dns,omitempty"`
	NDMDNSOverride                    string                     `json:"ndm_dns_override,omitempty"`
	NDMFilterEngine                   string                     `json:"ndm_filter_engine,omitempty"`
	NDMDNSIntercept                   string                     `json:"ndm_dns_intercept,omitempty"`
	NDMDNSAssignments                 string                     `json:"ndm_dns_assignments,omitempty"`
	Port53Owner                       string                     `json:"port53_owner,omitempty"`
	XrayDNSInboundCount               string                     `json:"xray_dns_inbound_count,omitempty"`
	XrayRunning                       bool                       `json:"xray_running"`
	XrayGID                           string                     `json:"xray_gid,omitempty"`
	DNSRoutingMode                    string                     `json:"dns_routing_mode,omitempty"`
	DNSOut                            bool                       `json:"dns_out_present"`
	VLESSProfile                      bool                       `json:"vless_profile_present"`
	ExpectedDelta                     string                     `json:"expected_delta,omitempty"`
	ExpectedNoDelta                   string                     `json:"expected_no_delta,omitempty"`
	Mutation                          string                     `json:"mutation,omitempty"`
	NativeFilterEngineConfirmRequired bool                       `json:"native_filter_engine_confirm_required"`
	NativeFilterEngineChoices         []string                   `json:"native_filter_engine_choices,omitempty"`
	ExtraProfiles                     []subscriptionProfile      `json:"extra_profiles,omitempty"`
	ProfilesError                     string                     `json:"profiles_error,omitempty"`
	ProviderPlan                      *providerPlanResponse       `json:"provider_plan,omitempty"`
	SetupFinalizePlan                 *setupFinalizePlanResponse `json:"setup_finalize_plan,omitempty"`
	Error                             string                     `json:"error,omitempty"`
}

type networkApplyRequest struct {
	Operation          string `json:"operation,omitempty"`
	ISP                string `json:"isp,omitempty"`
	DNSMode            string `json:"dns_mode,omitempty"`
	NativeDNSProvider  string `json:"native_dns_provider,omitempty"`
	ProfileID          string `json:"profile_id,omitempty"`
	NativeFilterEngine string `json:"native_filter_engine,omitempty"`
	Confirm            bool   `json:"confirm"`
}

type networkApplyResponse struct {
	Success           bool                       `json:"success"`
	Applied           bool                       `json:"applied"`
	Operation         string                     `json:"operation,omitempty"`
	OperationID       string                     `json:"operation_id,omitempty"`
	ISP               string                     `json:"isp,omitempty"`
	DNSMode           string                     `json:"dns_mode,omitempty"`
	NativeDNSProvider string                     `json:"native_dns_provider,omitempty"`
	ProfileID         string                     `json:"profile_id,omitempty"`
	Message           string                     `json:"message,omitempty"`
	PrimaryError      string                     `json:"primary_error,omitempty"`
	RollbackState     string                     `json:"rollback_state,omitempty"`
	Error             string                     `json:"error,omitempty"`
	Plan              networkPlanResponse        `json:"plan"`
	ProviderPlan      *providerPlanResponse       `json:"provider_plan,omitempty"`
	SetupFinalizePlan *setupFinalizePlanResponse `json:"setup_finalize_plan,omitempty"`
}

func networkHelperPath() string {
	if p := strings.TrimSpace(os.Getenv("FREENET_NETWORK_HELPER")); p != "" {
		return p
	}
	return defaultNetworkHelperPath
}

func providerHelperPath() string {
	if p := strings.TrimSpace(os.Getenv("FREENET_PROVIDER_HELPER")); p != "" {
		return p
	}
	return defaultProviderHelperPath
}

func validProfileID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, r := range id {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func validNetworkSelection(isp, dnsMode string) bool {
	if _, ok := ispProfiles[isp]; !ok {
		return false
	}
	_, ok := dnsModes[dnsMode]
	return ok
}

func (a *app) requestedNetworkSelection(r *http.Request) (string, string, string, string, error) {
	activeISP, activeDNS := readNetworkProfileConfig(a.cfg.ConfigPath)
	isp := strings.TrimSpace(r.URL.Query().Get("isp"))
	dnsMode := strings.TrimSpace(r.URL.Query().Get("dns_mode"))
	if isp == "" {
		isp = activeISP
	}
	if dnsMode == "" {
		dnsMode = activeDNS
	}
	if !validNetworkSelection(isp, dnsMode) {
		return "", "", activeISP, activeDNS, errors.New("unsupported ISP or DNS mode")
	}
	return isp, dnsMode, activeISP, activeDNS, nil
}

func (a *app) requestedNativeDNSProvider(r *http.Request) (string, string, error) {
	active := readNativeDNSProvider(a.cfg.ConfigPath)
	provider := strings.TrimSpace(r.URL.Query().Get("native_dns_provider"))
	if provider == "" {
		provider = active
	}
	if !validNativeDNSProvider(provider) {
		return "", active, errors.New("unsupported Native DNS provider")
	}
	return provider, active, nil
}

func decorateNativeDNSProviderPlan(plan *networkPlanResponse, provider, activeProvider string) {
	if plan == nil {
		return
	}
	plan.NativeDNSProvider = provider
	plan.ActiveNativeDNSProvider = activeProvider
	plan.NativeDNSProviderOptions = nativeDNSProviderOptions()
}

func networkTargetProductStateMatches(isp, dnsMode, provider, activeISP, activeDNS, activeProvider string) bool {
	if isp != activeISP || dnsMode != activeDNS {
		return false
	}
	if dnsMode == "firmware" {
		return provider == activeProvider
	}
	return true
}

func (a *app) handleNetworkProfilePlan(w http.ResponseWriter, r *http.Request) {
	isp, dnsMode, activeISP, activeDNS, selectionErr := a.requestedNetworkSelection(r)
	if selectionErr != nil {
		writeJSON(w, http.StatusBadRequest, networkPlanResponse{Success: false, Error: selectionErr.Error()})
		return
	}
	provider, activeProvider, providerErr := a.requestedNativeDNSProvider(r)
	if providerErr != nil {
		writeJSON(w, http.StatusBadRequest, networkPlanResponse{Success: false, Error: providerErr.Error()})
		return
	}
	if capabilityErr := splitDNSSelectionError(dnsMode); capabilityErr != nil {
		plan := networkPlanResponse{
			Success: false, Supported: false, ISP: isp, DNSMode: dnsMode,
			ActiveISP: activeISP, ActiveDNSMode: activeDNS,
			Reason: capabilityErr.Error(), Mutation: "NONE", Error: capabilityErr.Error(),
		}
		decorateNativeDNSProviderPlan(&plan, provider, activeProvider)
		writeJSON(w, http.StatusConflict, plan)
		return
	}

	plan, err := a.runNetworkPlanFor(isp, dnsMode, provider)
	plan.ActiveISP = activeISP
	plan.ActiveDNSMode = activeDNS
	decorateNativeDNSProviderPlan(&plan, provider, activeProvider)
	plan.Active = plan.Active && networkTargetProductStateMatches(isp, dnsMode, provider, activeISP, activeDNS, activeProvider)
	if err != nil {
		plan.Success = false
		plan.Error = err.Error()
		writeJSON(w, http.StatusServiceUnavailable, plan)
		return
	}

	if subscriptionConfigured(a.cfg.SubPath) {
		ctx, cancel := context.WithTimeout(context.Background(), 32*time.Second)
		profiles, profileErr := a.discoverSubscriptionProfiles(ctx)
		cancel()
		if profileErr != nil {
			plan.ProfilesError = profileErr.Error()
		} else {
			plan.ExtraProfiles = profiles
		}
	}

	if profileID := strings.TrimSpace(r.URL.Query().Get("provider_profile_id")); profileID != "" {
		providerPlan := providerPlanResponse{ProfileID: profileID}
		if !validProfileID(profileID) {
			providerPlan.Error = "invalid provider profile id"
		} else if pp, providerErr := a.runProviderPlan(profileID); providerErr != nil {
			providerPlan = pp
			providerPlan.Error = providerErr.Error()
		} else {
			providerPlan = pp
		}
		plan.ProviderPlan = &providerPlan
	}

	if r.URL.Query().Get("setup_finalize") == "1" {
		finalizePlan, finalizeErr := a.runSetupFinalizePlan()
		if finalizeErr != nil {
			finalizePlan.Error = finalizeErr.Error()
		}
		plan.SetupFinalizePlan = &finalizePlan
	}
	writeJSON(w, http.StatusOK, plan)
}

func (a *app) handleNetworkProfileApply(w http.ResponseWriter, r *http.Request) {
	if a.mutationBlockedBySelfUpdate(w) {
		return
	}
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, networkApplyResponse{Success: false, Error: "cross-origin request rejected"})
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, networkApplyResponse{Success: false, Error: "application/json required"})
		return
	}

	body := http.MaxBytesReader(w, r.Body, 1536)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req networkApplyRequest
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, networkApplyResponse{Success: false, Error: "invalid request"})
		return
	}
	if !req.Confirm {
		writeJSON(w, http.StatusBadRequest, networkApplyResponse{Success: false, Error: "explicit confirmation required"})
		return
	}

	operation := strings.TrimSpace(req.Operation)
	if operation == "" {
		operation = "network"
	}
	if operation == "provider" {
		a.handleProviderProfileApply(w, r, req)
		return
	}
	if operation == "finalize" {
		a.handleSetupFinalizeApply(w, req)
		return
	}
	if operation != "network" {
		writeJSON(w, http.StatusBadRequest, networkApplyResponse{Success: false, Error: "unsupported apply operation"})
		return
	}
	if !validNetworkSelection(req.ISP, req.DNSMode) {
		writeJSON(w, http.StatusBadRequest, networkApplyResponse{Success: false, Error: "unsupported ISP or DNS mode"})
		return
	}
	if strings.TrimSpace(req.NativeDNSProvider) == "" {
		req.NativeDNSProvider = readNativeDNSProvider(a.cfg.ConfigPath)
	}
	if !validNativeDNSProvider(req.NativeDNSProvider) {
		writeJSON(w, http.StatusBadRequest, networkApplyResponse{Success: false, Error: "unsupported Native DNS provider"})
		return
	}
	if capabilityErr := splitDNSSelectionError(req.DNSMode); capabilityErr != nil {
		writeJSON(w, http.StatusConflict, networkApplyResponse{
			Success: false, Applied: false, Operation: "network", ISP: req.ISP, DNSMode: req.DNSMode,
			NativeDNSProvider: req.NativeDNSProvider,
			PrimaryError: capabilityErr.Error(), RollbackState: "NOT_APPLIED",
			Error: "XKeen/Xray DNS недоступен на этом устройстве",
		})
		return
	}

	target := req.ISP + "\x00" + req.DNSMode + "\x00" + req.NativeDNSProvider
	flight, leader := beginNetworkApplyFlight(target)
	if !leader {
		if status, result, ok := waitNetworkApplyFlight(r, flight); ok {
			writeJSON(w, status, result)
		}
		return
	}

	status, result := a.executeNetworkApply(r.Context(), req)
	finishNetworkApplyFlight(flight, status, result)
	writeJSON(w, status, result)
}

func (a *app) executeNetworkApply(requestCtx context.Context, req networkApplyRequest) (int, networkApplyResponse) {
	acquireTimer := time.NewTimer(12 * time.Second)
	defer acquireTimer.Stop()
	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	case <-requestCtx.Done():
		return http.StatusRequestTimeout, networkApplyResponse{
			Success: false, Applied: false, Operation: "network", ISP: req.ISP, DNSMode: req.DNSMode,
			NativeDNSProvider: req.NativeDNSProvider,
			RollbackState: "NOT_APPLIED", Error: "запрос отменён до начала сетевой операции",
		}
	case <-acquireTimer.C:
		if post, err := a.runNetworkPlanFor(req.ISP, req.DNSMode, req.NativeDNSProvider); err == nil && post.Active {
			return http.StatusOK, networkApplyResponse{
				Success: true, Applied: false, Operation: "network", ISP: req.ISP, DNSMode: req.DNSMode,
				NativeDNSProvider: req.NativeDNSProvider,
				Message: "Целевое сетевое состояние уже активно.", RollbackState: "NOT_NEEDED", Plan: post,
			}
		}
		return http.StatusLocked, networkApplyResponse{
			Success: false, Applied: false, Operation: "network", ISP: req.ISP, DNSMode: req.DNSMode,
			NativeDNSProvider: req.NativeDNSProvider,
			RollbackState: "NOT_APPLIED",
			Error: "FreeNet выполняет другую подтверждённую операцию; параллельная mutation заблокирована",
		}
	}

	activeISP, activeDNS := readNetworkProfileConfig(a.cfg.ConfigPath)
	activeProvider := readNativeDNSProvider(a.cfg.ConfigPath)
	plan, err := a.runNetworkPlanFor(req.ISP, req.DNSMode, req.NativeDNSProvider)
	plan.ActiveISP = activeISP
	plan.ActiveDNSMode = activeDNS
	decorateNativeDNSProviderPlan(&plan, req.NativeDNSProvider, activeProvider)
	plan.Active = plan.Active && networkTargetProductStateMatches(req.ISP, req.DNSMode, req.NativeDNSProvider, activeISP, activeDNS, activeProvider)
	if err != nil {
		return http.StatusServiceUnavailable, networkApplyResponse{
			Success: false, Applied: false, Operation: "network", ISP: req.ISP, DNSMode: req.DNSMode,
			NativeDNSProvider: req.NativeDNSProvider,
			Plan: plan, RollbackState: "NOT_APPLIED", Error: err.Error(),
		}
	}
	if !plan.Supported {
		return http.StatusConflict, networkApplyResponse{
			Success: false, Applied: false, Operation: "network", ISP: req.ISP, DNSMode: req.DNSMode,
			NativeDNSProvider: req.NativeDNSProvider,
			Plan: plan, RollbackState: "NOT_APPLIED", Error: plan.Reason,
		}
	}
	if plan.Mutation != "NONE" {
		return http.StatusConflict, networkApplyResponse{
			Success: false, Applied: false, Operation: "network", ISP: req.ISP, DNSMode: req.DNSMode,
			NativeDNSProvider: req.NativeDNSProvider,
			Plan: plan, RollbackState: "NOT_APPLIED", Error: "network plan is not read-only; refusing apply",
		}
	}
	if plan.Active {
		if !networkTargetProductStateMatches(req.ISP, req.DNSMode, req.NativeDNSProvider, activeISP, activeDNS, activeProvider) {
			if err := writeNetworkProfileConfigWithNativeProvider(a.cfg.ConfigPath, req.ISP, req.DNSMode, req.NativeDNSProvider); err != nil {
				return http.StatusInternalServerError, networkApplyResponse{
					Success: false, Applied: false, Operation: "network", ISP: activeISP, DNSMode: activeDNS,
					NativeDNSProvider: activeProvider,
					Plan: plan, PrimaryError: "cannot persist already-active network target", RollbackState: "NOT_APPLIED",
					Error: "runtime target active but product state commit failed",
				}
			}
		}
		return http.StatusOK, networkApplyResponse{
			Success: true, Applied: false, Operation: "network", ISP: req.ISP, DNSMode: req.DNSMode,
			NativeDNSProvider: req.NativeDNSProvider,
			Plan: plan, Message: "Выбранный сетевой профиль уже активен.", RollbackState: "NOT_NEEDED",
		}
	}

	if req.DNSMode == "firmware" {
		if err := prepareCanonicalNativeApplyState(); err != nil {
			return http.StatusServiceUnavailable, networkApplyResponse{
				Success: false, Applied: false, Operation: "network", ISP: req.ISP, DNSMode: req.DNSMode,
				NativeDNSProvider: req.NativeDNSProvider,
				Plan: plan, PrimaryError: err.Error(), RollbackState: "NOT_APPLIED",
				Error: "не удалось подготовить Native DNS state",
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Timeout)
	output, cmdErr := a.runNetworkApplyFor(ctx, req.ISP, req.DNSMode, req.NativeDNSProvider)
	timedOut := ctx.Err() == context.DeadlineExceeded
	cancel()
	safeOutput := sanitizeOutput(string(output))
	if timedOut {
		cmdErr = errors.New("network apply timed out")
	}
	if cmdErr != nil {
		primary, rollback := classifyApplyFailure(safeOutput)
		if primary == "" {
			primary = cmdErr.Error()
		}
		return http.StatusBadGateway, networkApplyResponse{
			Success: false, Applied: false, Operation: "network", ISP: req.ISP, DNSMode: req.DNSMode,
			NativeDNSProvider: req.NativeDNSProvider,
			Plan: plan, PrimaryError: primary, RollbackState: rollback, Error: "network profile apply failed",
		}
	}

	if err := writeNetworkProfileConfigWithNativeProvider(a.cfg.ConfigPath, req.ISP, req.DNSMode, req.NativeDNSProvider); err != nil {
		rollback := a.rollbackNetworkSelection(activeISP, activeDNS, activeProvider)
		return http.StatusBadGateway, networkApplyResponse{
			Success: false, Applied: false, Operation: "network", ISP: activeISP, DNSMode: activeDNS,
			NativeDNSProvider: activeProvider,
			Plan: plan, PrimaryError: "cannot commit accepted network profile", RollbackState: rollback,
			Error: "runtime changed but active profile commit failed",
		}
	}

	post, postErr := a.runNetworkPlan()
	if postErr != nil || !post.Active {
		primary := "post-apply active profile acceptance failed"
		if postErr != nil {
			primary = "post-apply plan unavailable: " + postErr.Error()
		}
		rollback := a.rollbackNetworkSelection(activeISP, activeDNS, activeProvider)
		return http.StatusBadGateway, networkApplyResponse{
			Success: false, Applied: false, Operation: "network", ISP: activeISP, DNSMode: activeDNS,
			NativeDNSProvider: activeProvider,
			Plan: plan, PrimaryError: primary, RollbackState: rollback,
			Error: "network profile acceptance failed after commit",
		}
	}

	return http.StatusOK, networkApplyResponse{
		Success: true, Applied: true, Operation: "network", ISP: req.ISP, DNSMode: req.DNSMode,
		NativeDNSProvider: req.NativeDNSProvider,
		Message: "Сетевой профиль приведён к целевому состоянию, проверен и сохранён.",
		RollbackState: "NOT_NEEDED", Plan: post,
	}
}

func (a *app) handleProviderProfileApply(w http.ResponseWriter, r *http.Request, req networkApplyRequest) {
	profileID := strings.TrimSpace(req.ProfileID)
	if !validProfileID(profileID) {
		writeJSON(w, http.StatusBadRequest, networkApplyResponse{Success: false, Operation: "provider", Error: "invalid provider profile id"})
		return
	}

	op, leader, conflict := vpnOperations.begin("provider", profileID)
	if !leader {
		if conflict != nil {
			writeJSON(w, http.StatusConflict, operationConflictPayload("другая VPN-операция уже выполняется", *conflict))
			return
		}
		status, payload, ok := vpnOperations.wait(r.Context(), op)
		if !ok {
			return
		}
		result, payloadOK := payload.(networkApplyResponse)
		if !payloadOK {
			writeJSON(w, http.StatusInternalServerError, networkApplyResponse{Success: false, Operation: "provider", Error: "operation result unavailable"})
			return
		}
		writeJSON(w, status, result)
		return
	}

	status, result := a.executeProviderProfileApply(req)
	result.OperationID = op.state.ID
	vpnOperations.finish(op, status, result, result.Success, result.Message, result.Error)
	writeJSON(w, status, result)
}

func (a *app) executeProviderProfileApply(req networkApplyRequest) (int, networkApplyResponse) {
	profileID := strings.TrimSpace(req.ProfileID)
	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	default:
		return http.StatusConflict, networkApplyResponse{Success: false, Operation: "provider", ProfileID: profileID, Error: "another FreeNet operation is already running"}
	}

	providerPlan, err := a.runProviderPlan(profileID)
	if err != nil {
		return http.StatusConflict, networkApplyResponse{Success: false, Operation: "provider", ProfileID: profileID, ProviderPlan: &providerPlan, Error: err.Error()}
	}
	if !providerPlan.CandidateValid || providerPlan.Mutation != "NONE" {
		return http.StatusConflict, networkApplyResponse{Success: false, Operation: "provider", ProfileID: profileID, ProviderPlan: &providerPlan, Error: "provider plan is not a validated read-only candidate"}
	}

	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Timeout)
	defer cancel()
	output, cmdErr := runCommand(ctx, providerHelperPath(), "apply", profileID)
	safeOutput := sanitizeOutput(string(output))
	if ctx.Err() == context.DeadlineExceeded {
		cmdErr = errors.New("provider profile apply timed out")
	}
	if cmdErr != nil {
		primary, rollback := classifyApplyFailure(safeOutput)
		if primary == "" {
			primary = cmdErr.Error()
		}
		return http.StatusBadGateway, networkApplyResponse{
			Success: false, Applied: false, Operation: "provider", ProfileID: profileID,
			ProviderPlan: &providerPlan, PrimaryError: primary, RollbackState: rollback,
			Error: "provider profile apply failed",
		}
	}

	postProvider, postErr := a.runProviderPlan(profileID)
	if postErr != nil {
		return http.StatusBadGateway, networkApplyResponse{
			Success: false, Applied: true, Operation: "provider", ProfileID: profileID,
			ProviderPlan: &providerPlan, PrimaryError: "post-apply provider plan unavailable: " + postErr.Error(),
			RollbackState: "NOT_REQUESTED_HELPER_REPORTED_SUCCESS",
			Error: "provider apply completed but UI acceptance could not be read",
		}
	}
	postNetwork, _ := a.runNetworkPlan()
	return http.StatusOK, networkApplyResponse{
		Success: true, Applied: true, Operation: "provider", ProfileID: profileID,
		Message: "VPN-профиль применён и Xray-конфигурация проверена.", RollbackState: "NOT_NEEDED",
		Plan: postNetwork, ProviderPlan: &postProvider,
	}
}

func (a *app) createNetworkDraftConfig(isp, dnsMode, nativeProvider string) (string, error) {
	if !validNetworkSelection(isp, dnsMode) {
		return "", errors.New("unsupported ISP or DNS mode")
	}
	if !validNativeDNSProvider(nativeProvider) {
		return "", errors.New("unsupported Native DNS provider")
	}
	current, err := os.ReadFile(a.cfg.ConfigPath)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "freenet-network-draft-*.conf")
	if err != nil {
		return "", err
	}
	name := f.Name()
	remove := true
	defer func() {
		_ = f.Close()
		if remove {
			_ = os.Remove(name)
		}
	}()
	if err := f.Chmod(0600); err != nil {
		return "", err
	}
	if _, err := f.Write(current); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := writeNetworkProfileConfigWithNativeProvider(name, isp, dnsMode, nativeProvider); err != nil {
		return "", err
	}
	remove = false
	return name, nil
}

func runNetworkHelperWithConfig(ctx context.Context, configPath string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, networkHelperPath(), args...)
	cmd.Env = append(os.Environ(),
		"PATH=/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin",
		"FREENET_CONFIG_FILE="+configPath,
	)
	cmd.WaitDelay = 2 * time.Second
	output, err := cmd.CombinedOutput()
	if errors.Is(err, exec.ErrWaitDelay) && ctx.Err() == nil {
		err = nil
	}
	return output, err
}

func (a *app) runNetworkPlanFor(isp, dnsMode, nativeProvider string) (networkPlanResponse, error) {
	draft, err := a.createNetworkDraftConfig(isp, dnsMode, nativeProvider)
	if err != nil {
		return networkPlanResponse{}, err
	}
	defer os.Remove(draft)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output, cmdErr := runNetworkHelperWithConfig(ctx, draft, "plan")
	if ctx.Err() == context.DeadlineExceeded {
		return networkPlanResponse{}, errors.New("network plan timed out")
	}
	plan, parseErr := parseNetworkPlan(string(output))
	if parseErr != nil {
		return plan, parseErr
	}
	if plan.ISP != isp || plan.DNSMode != dnsMode {
		return plan, errors.New("network helper did not plan the exact requested draft")
	}
	if cmdErr != nil {
		return plan, errors.New("network plan helper failed")
	}
	plan.NativeDNSProvider = nativeProvider
	plan.NativeDNSProviderOptions = nativeDNSProviderOptions()
	return plan, nil
}

func (a *app) runNetworkApplyFor(ctx context.Context, isp, dnsMode, nativeProvider string) ([]byte, error) {
	draft, err := a.createNetworkDraftConfig(isp, dnsMode, nativeProvider)
	if err != nil {
		return nil, err
	}
	defer os.Remove(draft)
	return runNetworkHelperWithConfig(ctx, draft, "apply")
}

func (a *app) rollbackNetworkSelection(isp, dnsMode, nativeProvider string) string {
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Timeout)
	output, err := a.runNetworkApplyFor(ctx, isp, dnsMode, nativeProvider)
	cancel()
	if err != nil {
		_ = output
		return "FAILED/UNKNOWN"
	}
	if err := writeNetworkProfileConfigWithNativeProvider(a.cfg.ConfigPath, isp, dnsMode, nativeProvider); err != nil {
		return "FAILED/UNKNOWN"
	}
	post, err := a.runNetworkPlan()
	if err != nil || !post.Active {
		return "FAILED/UNKNOWN"
	}
	return "SUCCESS"
}

func (a *app) runNetworkPlan() (networkPlanResponse, error) {
	isp, dnsMode := readNetworkProfileConfig(a.cfg.ConfigPath)
	nativeProvider := readNativeDNSProvider(a.cfg.ConfigPath)
	plan, err := a.runNetworkPlanFor(isp, dnsMode, nativeProvider)
	plan.ActiveISP = isp
	plan.ActiveDNSMode = dnsMode
	decorateNativeDNSProviderPlan(&plan, nativeProvider, nativeProvider)
	return plan, err
}

func (a *app) runProviderPlan(profileID string) (providerPlanResponse, error) {
	if !validProfileID(profileID) {
		return providerPlanResponse{ProfileID: profileID}, errors.New("invalid provider profile id")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	output, err := runCommand(ctx, providerHelperPath(), "plan", profileID)
	if ctx.Err() == context.DeadlineExceeded {
		return providerPlanResponse{ProfileID: profileID}, errors.New("provider plan timed out")
	}
	plan, parseErr := parseProviderPlan(string(output))
	if parseErr != nil {
		return plan, parseErr
	}
	if err != nil {
		return plan, errors.New("provider plan helper failed")
	}
	return plan, nil
}

func networkPlanActiveMismatch(p networkPlanResponse) string {
	if !p.Supported {
		return "unsupported"
	}
	var mismatches []string
	need := func(ok bool, detail string) {
		if !ok {
			mismatches = append(mismatches, detail)
		}
	}
	need(p.ProxyDNS == "off", "proxy_dns="+p.ProxyDNS+" (ожидается off)")

	switch p.EffectiveDNSMode {
	case "firmware":
		need(p.NDMDNSOverride == "off", "dns-override="+p.NDMDNSOverride+" (ожидается off)")
		need(p.NDMFilterEngine != "" && p.NDMFilterEngine != "unknown" && p.NDMFilterEngine != "opkg", "filter-engine="+p.NDMFilterEngine+" (ожидается native engine)")
		need(p.NDMDNSIntercept == "on" || p.NDMDNSIntercept == "off", "native-intercept="+p.NDMDNSIntercept+" (ожидается известное native state)")
		need(p.Port53Owner == "ndnproxy", "owner:53="+p.Port53Owner+" (ожидается ndnproxy)")
		need(p.XrayDNSInboundCount == "0", "xray-dns-inbound="+p.XrayDNSInboundCount+" (ожидается 0)")
		need(p.DNSRoutingMode == "native", "dns-routing="+p.DNSRoutingMode+" (ожидается native)")
		need(!p.DNSOut, "dns-out присутствует (ожидается отсутствует)")
		if p.XrayRunning {
			need(p.XrayGID == "11111", "xray-gid="+p.XrayGID+" (ожидается 11111)")
		}
	case "xkeen":
		need(p.NDMDNSOverride == "on", "dns-override="+p.NDMDNSOverride+" (ожидается on)")
		need(p.NDMFilterEngine == "opkg", "filter-engine="+p.NDMFilterEngine+" (ожидается opkg)")
		need(p.NDMDNSIntercept == "off", "native-intercept="+p.NDMDNSIntercept+" (ожидается off)")
		need(p.NDMDNSAssignments == "none", "native-assignments="+p.NDMDNSAssignments+" (ожидается none)")
		need(p.Port53Owner == "xray", "owner:53="+p.Port53Owner+" (ожидается xray)")
		need(p.XrayDNSInboundCount == "1", "xray-dns-inbound="+p.XrayDNSInboundCount+" (ожидается 1)")
		need(p.XrayRunning, "xray-running=no (ожидается yes)")
		need(p.XrayGID == "11111", "xray-gid="+p.XrayGID+" (ожидается 11111)")
		need(p.DNSRoutingMode == "split", "dns-routing="+p.DNSRoutingMode+" (ожидается split)")
		need(p.DNSOut, "dns-out отсутствует")
		need(p.VLESSProfile, "vless-reality отсутствует")
	default:
		mismatches = append(mismatches, "неподдерживаемый effective DNS mode")
	}
	return strings.Join(mismatches, "; ")
}

func networkPlanIsActive(p networkPlanResponse) bool {
	return networkPlanActiveMismatch(p) == ""
}

func parseNetworkPlan(output string) (networkPlanResponse, error) {
	values := map[string]string{}
	for _, raw := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "ISP_ID", "DNS_MODE", "EFFECTIVE_DNS_MODE", "SUPPORTED", "REASON", "PROXY_DNS", "NDM_DNS_OVERRIDE", "NDM_FILTER_ENGINE", "NDM_DNS_INTERCEPT", "NDM_DNS_ASSIGNMENTS", "PORT53_OWNER", "XRAY_DNS_INBOUND_COUNT", "XRAY_RUNNING", "XRAY_GID", "DNS_ROUTING_MODE", "DNS_OUT", "VLESS_PROFILE", "EXPECTED_DELTA", "EXPECTED_NO_DELTA", "MUTATION":
			values[key] = strings.TrimSpace(value)
		}
	}
	if values["ISP_ID"] == "" || values["DNS_MODE"] == "" || values["SUPPORTED"] == "" || values["MUTATION"] == "" {
		return networkPlanResponse{}, errors.New("incomplete network plan")
	}
	if values["MUTATION"] != "NONE" {
		return networkPlanResponse{}, errors.New("network plan unexpectedly reports mutation")
	}
	plan := networkPlanResponse{
		Success: true, Supported: values["SUPPORTED"] == "yes", ISP: values["ISP_ID"], DNSMode: values["DNS_MODE"],
		EffectiveDNSMode: values["EFFECTIVE_DNS_MODE"], Reason: values["REASON"], ProxyDNS: values["PROXY_DNS"],
		NDMDNSOverride: values["NDM_DNS_OVERRIDE"], NDMFilterEngine: values["NDM_FILTER_ENGINE"],
		NDMDNSIntercept: values["NDM_DNS_INTERCEPT"], NDMDNSAssignments: values["NDM_DNS_ASSIGNMENTS"],
		Port53Owner: values["PORT53_OWNER"], XrayDNSInboundCount: values["XRAY_DNS_INBOUND_COUNT"],
		XrayRunning: values["XRAY_RUNNING"] == "yes", XrayGID: values["XRAY_GID"],
		DNSRoutingMode: values["DNS_ROUTING_MODE"], DNSOut: values["DNS_OUT"] == "yes",
		VLESSProfile: values["VLESS_PROFILE"] == "yes", ExpectedDelta: values["EXPECTED_DELTA"],
		ExpectedNoDelta: values["EXPECTED_NO_DELTA"], Mutation: values["MUTATION"],
	}
	mismatch := networkPlanActiveMismatch(plan)
	plan.Active = mismatch == ""
	if plan.Supported && mismatch != "" {
		if plan.Reason != "" {
			plan.Reason += "; "
		}
		plan.Reason += "runtime не подтверждён: " + mismatch
	}
	return plan, nil
}

func parseProviderPlan(output string) (providerPlanResponse, error) {
	values := map[string]string{}
	for _, raw := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "PROFILE_ID", "PROFILE_NAME", "ENDPOINT", "CURRENT_OUTBOUND", "XRAY_RUNNING", "CANDIDATE_XRAY_VALID", "EXPECTED_DELTA", "EXPECTED_NO_DELTA", "MUTATION":
			values[key] = strings.TrimSpace(value)
		}
	}
	if !validProfileID(values["PROFILE_ID"]) || values["PROFILE_NAME"] == "" || values["ENDPOINT"] == "" || values["MUTATION"] == "" {
		return providerPlanResponse{}, errors.New("incomplete provider plan")
	}
	if values["MUTATION"] != "NONE" {
		return providerPlanResponse{}, errors.New("provider plan unexpectedly reports mutation")
	}
	return providerPlanResponse{
		Success: true, ProfileID: values["PROFILE_ID"], ProfileName: values["PROFILE_NAME"], Endpoint: values["ENDPOINT"],
		CurrentOutbound: values["CURRENT_OUTBOUND"], XrayRunning: values["XRAY_RUNNING"] == "yes",
		CandidateValid: values["CANDIDATE_XRAY_VALID"] == "yes", ExpectedDelta: values["EXPECTED_DELTA"],
		ExpectedNoDelta: values["EXPECTED_NO_DELTA"], Mutation: values["MUTATION"],
	}, nil
}
