package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultNetworkHelperPath  = "/opt/lib/freenet/apply_network_profile.sh"
	defaultProviderHelperPath = "/opt/lib/freenet/apply_provider_profile.sh"
)

type providerPlanResponse struct {
	Success          bool   `json:"success"`
	ProfileID        string `json:"profile_id,omitempty"`
	ProfileName      string `json:"profile_name,omitempty"`
	Endpoint         string `json:"endpoint,omitempty"`
	CurrentOutbound  string `json:"current_outbound,omitempty"`
	XrayRunning      bool   `json:"xray_running"`
	CandidateValid   bool   `json:"candidate_xray_valid"`
	ExpectedDelta    string `json:"expected_delta,omitempty"`
	ExpectedNoDelta  string `json:"expected_no_delta,omitempty"`
	Mutation         string `json:"mutation,omitempty"`
	Error            string `json:"error,omitempty"`
}

type networkPlanResponse struct {
	Success          bool                  `json:"success"`
	Supported        bool                  `json:"supported"`
	ISP              string                `json:"isp"`
	DNSMode          string                `json:"dns_mode"`
	EffectiveDNSMode string                `json:"effective_dns_mode"`
	Reason           string                `json:"reason,omitempty"`
	ProxyDNS         string                `json:"proxy_dns,omitempty"`
	Port53Owner      string                `json:"port53_owner,omitempty"`
	XrayGID          string                `json:"xray_gid,omitempty"`
	DNSOut           bool                  `json:"dns_out_present"`
	VLESSProfile     bool                  `json:"vless_profile_present"`
	ExpectedDelta    string                `json:"expected_delta,omitempty"`
	ExpectedNoDelta  string                `json:"expected_no_delta,omitempty"`
	Mutation         string                `json:"mutation,omitempty"`
	ExtraProfiles    []subscriptionProfile `json:"extra_profiles,omitempty"`
	ProfilesError    string                `json:"profiles_error,omitempty"`
	ProviderPlan     *providerPlanResponse `json:"provider_plan,omitempty"`
	Error            string                `json:"error,omitempty"`
}

type networkApplyRequest struct {
	Operation string `json:"operation,omitempty"`
	ISP       string `json:"isp,omitempty"`
	DNSMode   string `json:"dns_mode,omitempty"`
	ProfileID string `json:"profile_id,omitempty"`
	Confirm   bool   `json:"confirm"`
}

type networkApplyResponse struct {
	Success       bool                  `json:"success"`
	Applied       bool                  `json:"applied"`
	Operation     string                `json:"operation,omitempty"`
	ISP           string                `json:"isp,omitempty"`
	DNSMode       string                `json:"dns_mode,omitempty"`
	ProfileID     string                `json:"profile_id,omitempty"`
	Message       string                `json:"message,omitempty"`
	PrimaryError  string                `json:"primary_error,omitempty"`
	RollbackState string                `json:"rollback_state,omitempty"`
	Error         string                `json:"error,omitempty"`
	Plan          networkPlanResponse   `json:"plan"`
	ProviderPlan  *providerPlanResponse `json:"provider_plan,omitempty"`
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

func (a *app) handleNetworkProfilePlan(w http.ResponseWriter, r *http.Request) {
	plan, err := a.runNetworkPlan()
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
	writeJSON(w, http.StatusOK, plan)
}

func (a *app) handleNetworkProfileApply(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, networkApplyResponse{Success: false, Error: "cross-origin request rejected"})
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, networkApplyResponse{Success: false, Error: "application/json required"})
		return
	}

	body := http.MaxBytesReader(w, r.Body, 1024)
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
		a.handleProviderProfileApply(w, req)
		return
	}
	if operation != "network" {
		writeJSON(w, http.StatusBadRequest, networkApplyResponse{Success: false, Error: "unsupported apply operation"})
		return
	}

	if _, ok := ispProfiles[req.ISP]; !ok {
		writeJSON(w, http.StatusBadRequest, networkApplyResponse{Success: false, Error: "unsupported ISP"})
		return
	}
	if _, ok := dnsModes[req.DNSMode]; !ok {
		writeJSON(w, http.StatusBadRequest, networkApplyResponse{Success: false, Error: "unsupported DNS mode"})
		return
	}

	savedISP, savedDNS := readNetworkProfileConfig(a.cfg.ConfigPath)
	if req.ISP != savedISP || req.DNSMode != savedDNS {
		writeJSON(w, http.StatusConflict, networkApplyResponse{
			Success: false,
			ISP:     savedISP,
			DNSMode: savedDNS,
			Error:   "saved network profile changed; review the plan again",
		})
		return
	}

	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	default:
		writeJSON(w, http.StatusConflict, networkApplyResponse{Success: false, Error: "another FreeNet operation is already running"})
		return
	}

	plan, err := a.runNetworkPlan()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, networkApplyResponse{Success: false, Operation: "network", ISP: savedISP, DNSMode: savedDNS, Plan: plan, Error: err.Error()})
		return
	}
	if !plan.Supported {
		writeJSON(w, http.StatusConflict, networkApplyResponse{Success: false, Operation: "network", ISP: savedISP, DNSMode: savedDNS, Plan: plan, Error: plan.Reason})
		return
	}
	if plan.Mutation != "NONE" {
		writeJSON(w, http.StatusConflict, networkApplyResponse{Success: false, Operation: "network", ISP: savedISP, DNSMode: savedDNS, Plan: plan, Error: "network plan is not read-only; refusing apply"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Timeout)
	defer cancel()
	output, cmdErr := runCommand(ctx, networkHelperPath(), "apply")
	safeOutput := sanitizeOutput(string(output))
	if ctx.Err() == context.DeadlineExceeded {
		cmdErr = errors.New("network apply timed out")
	}
	if cmdErr != nil {
		primary, rollback := classifyApplyFailure(safeOutput)
		if primary == "" {
			primary = cmdErr.Error()
		}
		writeJSON(w, http.StatusBadGateway, networkApplyResponse{
			Success:       false,
			Applied:       false,
			Operation:     "network",
			ISP:           savedISP,
			DNSMode:       savedDNS,
			Plan:          plan,
			PrimaryError:  primary,
			RollbackState: rollback,
			Error:         "network profile apply failed",
		})
		return
	}

	post, postErr := a.runNetworkPlan()
	if postErr != nil {
		writeJSON(w, http.StatusBadGateway, networkApplyResponse{
			Success:       false,
			Applied:       true,
			Operation:     "network",
			ISP:           savedISP,
			DNSMode:       savedDNS,
			Plan:          plan,
			PrimaryError:  "post-apply plan unavailable: " + postErr.Error(),
			RollbackState: "NOT_REQUESTED_HELPER_REPORTED_SUCCESS",
			Error:         "network apply completed but UI acceptance could not be read",
		})
		return
	}

	writeJSON(w, http.StatusOK, networkApplyResponse{
		Success:       true,
		Applied:       true,
		Operation:     "network",
		ISP:           savedISP,
		DNSMode:       savedDNS,
		Message:       "Сетевой профиль применён и проверен.",
		RollbackState: "NOT_NEEDED",
		Plan:          post,
	})
}

func (a *app) handleProviderProfileApply(w http.ResponseWriter, req networkApplyRequest) {
	profileID := strings.TrimSpace(req.ProfileID)
	if !validProfileID(profileID) {
		writeJSON(w, http.StatusBadRequest, networkApplyResponse{Success: false, Operation: "provider", Error: "invalid provider profile id"})
		return
	}

	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	default:
		writeJSON(w, http.StatusConflict, networkApplyResponse{Success: false, Operation: "provider", Error: "another FreeNet operation is already running"})
		return
	}

	providerPlan, err := a.runProviderPlan(profileID)
	if err != nil {
		writeJSON(w, http.StatusConflict, networkApplyResponse{Success: false, Operation: "provider", ProfileID: profileID, ProviderPlan: &providerPlan, Error: err.Error()})
		return
	}
	if !providerPlan.CandidateValid || providerPlan.Mutation != "NONE" {
		writeJSON(w, http.StatusConflict, networkApplyResponse{Success: false, Operation: "provider", ProfileID: profileID, ProviderPlan: &providerPlan, Error: "provider plan is not a validated read-only candidate"})
		return
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
		writeJSON(w, http.StatusBadGateway, networkApplyResponse{
			Success:       false,
			Applied:       false,
			Operation:     "provider",
			ProfileID:     profileID,
			ProviderPlan:  &providerPlan,
			PrimaryError:  primary,
			RollbackState: rollback,
			Error:         "provider profile apply failed",
		})
		return
	}

	postProvider, postErr := a.runProviderPlan(profileID)
	if postErr != nil {
		writeJSON(w, http.StatusBadGateway, networkApplyResponse{
			Success:       false,
			Applied:       true,
			Operation:     "provider",
			ProfileID:     profileID,
			ProviderPlan:  &providerPlan,
			PrimaryError:  "post-apply provider plan unavailable: " + postErr.Error(),
			RollbackState: "NOT_REQUESTED_HELPER_REPORTED_SUCCESS",
			Error:         "provider apply completed but UI acceptance could not be read",
		})
		return
	}

	postNetwork, _ := a.runNetworkPlan()
	writeJSON(w, http.StatusOK, networkApplyResponse{
		Success:       true,
		Applied:       true,
		Operation:     "provider",
		ProfileID:     profileID,
		Message:       "VPN-профиль применён и Xray-конфигурация проверена.",
		RollbackState: "NOT_NEEDED",
		Plan:          postNetwork,
		ProviderPlan:  &postProvider,
	})
}

func (a *app) runNetworkPlan() (networkPlanResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	output, err := runCommand(ctx, networkHelperPath(), "plan")
	if ctx.Err() == context.DeadlineExceeded {
		return networkPlanResponse{}, errors.New("network plan timed out")
	}
	plan, parseErr := parseNetworkPlan(string(output))
	if parseErr != nil {
		return plan, parseErr
	}
	if err != nil {
		return plan, errors.New("network plan helper failed")
	}
	return plan, nil
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

func parseNetworkPlan(output string) (networkPlanResponse, error) {
	values := map[string]string{}
	for _, raw := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "ISP_ID", "DNS_MODE", "EFFECTIVE_DNS_MODE", "SUPPORTED", "REASON", "PROXY_DNS", "PORT53_OWNER", "XRAY_GID", "DNS_OUT", "VLESS_PROFILE", "EXPECTED_DELTA", "EXPECTED_NO_DELTA", "MUTATION":
			values[key] = strings.TrimSpace(value)
		}
	}
	if values["ISP_ID"] == "" || values["DNS_MODE"] == "" || values["SUPPORTED"] == "" || values["MUTATION"] == "" {
		return networkPlanResponse{}, errors.New("incomplete network plan")
	}
	if values["MUTATION"] != "NONE" {
		return networkPlanResponse{}, errors.New("network plan unexpectedly reports mutation")
	}
	return networkPlanResponse{
		Success:          true,
		Supported:        values["SUPPORTED"] == "yes",
		ISP:              values["ISP_ID"],
		DNSMode:          values["DNS_MODE"],
		EffectiveDNSMode: values["EFFECTIVE_DNS_MODE"],
		Reason:           values["REASON"],
		ProxyDNS:         values["PROXY_DNS"],
		Port53Owner:      values["PORT53_OWNER"],
		XrayGID:          values["XRAY_GID"],
		DNSOut:           values["DNS_OUT"] == "yes",
		VLESSProfile:     values["VLESS_PROFILE"] == "yes",
		ExpectedDelta:    values["EXPECTED_DELTA"],
		ExpectedNoDelta:  values["EXPECTED_NO_DELTA"],
		Mutation:         values["MUTATION"],
	}, nil
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
		Success:         true,
		ProfileID:       values["PROFILE_ID"],
		ProfileName:     values["PROFILE_NAME"],
		Endpoint:        values["ENDPOINT"],
		CurrentOutbound: values["CURRENT_OUTBOUND"],
		XrayRunning:     values["XRAY_RUNNING"] == "yes",
		CandidateValid:  values["CANDIDATE_XRAY_VALID"] == "yes",
		ExpectedDelta:   values["EXPECTED_DELTA"],
		ExpectedNoDelta: values["EXPECTED_NO_DELTA"],
		Mutation:        values["MUTATION"],
	}, nil
}

func classifyApplyFailure(output string) (string, string) {
	primary := ""
	rollback := "UNKNOWN"
	for _, part := range strings.Split(output, " | ") {
		line := strings.TrimSpace(part)
		if i := strings.Index(line, "PRIMARY ERROR:"); i >= 0 {
			primary = strings.TrimSpace(line[i+len("PRIMARY ERROR:"):])
		}
		switch {
		case strings.Contains(line, "ROLLBACK ERROR/STATE: FAILED/UNKNOWN"):
			rollback = "FAILED/UNKNOWN"
		case strings.Contains(line, "ROLLBACK ERROR/STATE: rollback success"):
			rollback = "SUCCESS"
		case strings.Contains(line, "ROLLBACK ERROR/STATE: no live apply"):
			rollback = "NOT_APPLIED"
		case strings.Contains(line, "rollback success/no live apply"):
			rollback = "SUCCESS_OR_NOT_APPLIED"
		}
	}
	return primary, rollback
}
