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

const defaultNetworkHelperPath = "/opt/lib/freenet/apply_network_profile.sh"

type networkPlanResponse struct {
	Success          bool   `json:"success"`
	Supported        bool   `json:"supported"`
	ISP              string `json:"isp"`
	DNSMode          string `json:"dns_mode"`
	EffectiveDNSMode string `json:"effective_dns_mode"`
	Reason           string `json:"reason,omitempty"`
	ProxyDNS         string `json:"proxy_dns,omitempty"`
	Port53Owner      string `json:"port53_owner,omitempty"`
	XrayGID          string `json:"xray_gid,omitempty"`
	DNSOut           bool   `json:"dns_out_present"`
	VLESSProfile     bool   `json:"vless_profile_present"`
	ExpectedDelta    string `json:"expected_delta,omitempty"`
	ExpectedNoDelta  string `json:"expected_no_delta,omitempty"`
	Mutation         string `json:"mutation,omitempty"`
	Error            string `json:"error,omitempty"`
}

type networkApplyRequest struct {
	ISP     string `json:"isp"`
	DNSMode string `json:"dns_mode"`
	Confirm bool   `json:"confirm"`
}

type networkApplyResponse struct {
	Success       bool                `json:"success"`
	Applied       bool                `json:"applied"`
	ISP           string              `json:"isp,omitempty"`
	DNSMode       string              `json:"dns_mode,omitempty"`
	Message       string              `json:"message,omitempty"`
	PrimaryError  string              `json:"primary_error,omitempty"`
	RollbackState string              `json:"rollback_state,omitempty"`
	Error         string              `json:"error,omitempty"`
	Plan          networkPlanResponse `json:"plan"`
}

func networkHelperPath() string {
	if p := strings.TrimSpace(os.Getenv("FREENET_NETWORK_HELPER")); p != "" {
		return p
	}
	return defaultNetworkHelperPath
}

func (a *app) handleNetworkProfilePlan(w http.ResponseWriter, _ *http.Request) {
	plan, err := a.runNetworkPlan()
	if err != nil {
		plan.Success = false
		plan.Error = err.Error()
		writeJSON(w, http.StatusServiceUnavailable, plan)
		return
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
		writeJSON(w, http.StatusServiceUnavailable, networkApplyResponse{Success: false, ISP: savedISP, DNSMode: savedDNS, Plan: plan, Error: err.Error()})
		return
	}
	if !plan.Supported {
		writeJSON(w, http.StatusConflict, networkApplyResponse{Success: false, ISP: savedISP, DNSMode: savedDNS, Plan: plan, Error: plan.Reason})
		return
	}
	if plan.Mutation != "NONE" {
		writeJSON(w, http.StatusConflict, networkApplyResponse{Success: false, ISP: savedISP, DNSMode: savedDNS, Plan: plan, Error: "network plan is not read-only; refusing apply"})
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
		primary, rollback := classifyNetworkApplyFailure(safeOutput)
		if primary == "" {
			primary = cmdErr.Error()
		}
		writeJSON(w, http.StatusBadGateway, networkApplyResponse{
			Success:       false,
			Applied:       false,
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
		ISP:           savedISP,
		DNSMode:       savedDNS,
		Message:       "Сетевой профиль применён и проверен.",
		RollbackState: "NOT_NEEDED",
		Plan:          post,
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

func classifyNetworkApplyFailure(output string) (string, string) {
	primary := ""
	rollback := "UNKNOWN"
	for _, part := range strings.Split(output, " | ") {
		line := strings.TrimSpace(part)
		if i := strings.Index(line, "PRIMARY ERROR:"); i >= 0 {
			primary = strings.TrimSpace(line[i+len("PRIMARY ERROR:"):])
		}
		if strings.Contains(line, "ROLLBACK ERROR/STATE: FAILED/UNKNOWN") {
			rollback = "FAILED/UNKNOWN"
		}
		if strings.Contains(line, "rollback success/no live apply") {
			rollback = "SUCCESS_OR_NOT_APPLIED"
		}
	}
	return primary, rollback
}
