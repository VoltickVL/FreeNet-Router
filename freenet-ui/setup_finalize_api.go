package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultFinalizeHelperPath = "/opt/lib/freenet/finalize_setup.sh"

type setupFinalizePlanResponse struct {
	Success                bool   `json:"success"`
	Ready                  bool   `json:"ready"`
	Reason                 string `json:"reason,omitempty"`
	InstallScenario        string `json:"install_scenario,omitempty"`
	SetupComplete          bool   `json:"setup_complete"`
	SubscriptionConfigured bool   `json:"subscription_configured"`
	PreferredProfileSet    bool   `json:"preferred_profile_set"`
	NetworkSupported       bool   `json:"network_supported"`
	XrayRunning            bool   `json:"xray_running"`
	XrayValid              bool   `json:"xray_valid"`
	DNSOut                 bool   `json:"dns_out_present"`
	VLESSProfile           bool   `json:"vless_profile_present"`
	XKeenAutostart         string `json:"xkeen_autostart,omitempty"`
	AutoEndpointUpdate     bool   `json:"auto_endpoint_update"`
	AutoEndpointCron       string `json:"auto_endpoint_cron,omitempty"`
	ExpectedDelta          string `json:"expected_delta,omitempty"`
	ExpectedNoDelta        string `json:"expected_no_delta,omitempty"`
	Mutation               string `json:"mutation,omitempty"`
	Error                  string `json:"error,omitempty"`
}

func finalizeHelperPath() string {
	if p := strings.TrimSpace(os.Getenv("FREENET_FINALIZE_HELPER")); p != "" {
		return p
	}
	return defaultFinalizeHelperPath
}

func (a *app) runSetupFinalizePlan() (setupFinalizePlanResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	output, cmdErr := runCommand(ctx, finalizeHelperPath(), "plan")
	if ctx.Err() == context.DeadlineExceeded {
		return setupFinalizePlanResponse{}, errors.New("final setup plan timed out")
	}
	plan, parseErr := parseSetupFinalizePlan(string(output))
	if parseErr != nil {
		if cmdErr != nil {
			return plan, errors.New("final setup plan helper failed")
		}
		return plan, parseErr
	}
	// Для состояния READY=no helper штатно возвращает ненулевой код после печати
	// полного read-only плана. Валидный распарсенный план важнее exit code.
	return plan, nil
}

func parseSetupFinalizePlan(output string) (setupFinalizePlanResponse, error) {
	values := map[string]string{}
	for _, raw := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "READY", "REASON", "INSTALL_SCENARIO", "SETUP_COMPLETE", "SUBSCRIPTION_CONFIGURED", "PREFERRED_PROFILE_SET", "NETWORK_SUPPORTED", "XRAY_RUNNING", "XRAY_VALID", "DNS_OUT", "VLESS_PROFILE", "XKEEN_AUTOSTART", "AUTO_ENDPOINT_UPDATE", "AUTO_ENDPOINT_CRON", "EXPECTED_DELTA", "EXPECTED_NO_DELTA", "MUTATION":
			values[key] = strings.TrimSpace(value)
		}
	}
	if values["READY"] == "" || values["SETUP_COMPLETE"] == "" || values["XKEEN_AUTOSTART"] == "" || values["MUTATION"] == "" {
		return setupFinalizePlanResponse{}, errors.New("incomplete final setup plan")
	}
	if values["MUTATION"] != "NONE" {
		return setupFinalizePlanResponse{}, errors.New("final setup plan unexpectedly reports mutation")
	}
	installScenario := values["INSTALL_SCENARIO"]
	if installScenario != "existing_stack" && installScenario != "fresh_entware" {
		installScenario = "unknown"
	}
	return setupFinalizePlanResponse{
		Success:                true,
		Ready:                  values["READY"] == "yes",
		Reason:                 values["REASON"],
		InstallScenario:        installScenario,
		SetupComplete:          values["SETUP_COMPLETE"] == "yes",
		SubscriptionConfigured: values["SUBSCRIPTION_CONFIGURED"] == "yes",
		PreferredProfileSet:    values["PREFERRED_PROFILE_SET"] == "yes",
		NetworkSupported:       values["NETWORK_SUPPORTED"] == "yes",
		XrayRunning:            values["XRAY_RUNNING"] == "yes",
		XrayValid:              values["XRAY_VALID"] == "yes",
		DNSOut:                 values["DNS_OUT"] == "yes",
		VLESSProfile:           values["VLESS_PROFILE"] == "yes",
		XKeenAutostart:         values["XKEEN_AUTOSTART"],
		AutoEndpointUpdate:     values["AUTO_ENDPOINT_UPDATE"] == "yes",
		AutoEndpointCron:       values["AUTO_ENDPOINT_CRON"],
		ExpectedDelta:          values["EXPECTED_DELTA"],
		ExpectedNoDelta:        values["EXPECTED_NO_DELTA"],
		Mutation:               values["MUTATION"],
	}, nil
}

func (a *app) handleSetupFinalizeApply(w http.ResponseWriter, req networkApplyRequest) {
	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	default:
		writeJSON(w, http.StatusConflict, networkApplyResponse{Success: false, Operation: "finalize", Error: "another FreeNet operation is already running"})
		return
	}

	plan, err := a.runSetupFinalizePlan()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, networkApplyResponse{Success: false, Operation: "finalize", SetupFinalizePlan: &plan, Error: err.Error()})
		return
	}
	if !plan.Ready {
		writeJSON(w, http.StatusConflict, networkApplyResponse{Success: false, Operation: "finalize", SetupFinalizePlan: &plan, Error: plan.Reason})
		return
	}
	if plan.Mutation != "NONE" {
		writeJSON(w, http.StatusConflict, networkApplyResponse{Success: false, Operation: "finalize", SetupFinalizePlan: &plan, Error: "final setup plan is not read-only; refusing apply"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Timeout)
	defer cancel()
	output, cmdErr := runCommand(ctx, finalizeHelperPath(), "apply")
	safeOutput := sanitizeOutput(string(output))
	if ctx.Err() == context.DeadlineExceeded {
		cmdErr = errors.New("final setup apply timed out")
	}
	if cmdErr != nil {
		primary, rollback := classifyApplyFailure(safeOutput)
		if primary == "" {
			primary = cmdErr.Error()
		}
		writeJSON(w, http.StatusBadGateway, networkApplyResponse{
			Success:           false,
			Applied:           false,
			Operation:         "finalize",
			SetupFinalizePlan: &plan,
			PrimaryError:      primary,
			RollbackState:     rollback,
			Error:             "final setup apply failed",
		})
		return
	}

	post, postErr := a.runSetupFinalizePlan()
	if postErr != nil || !post.Ready || !post.SetupComplete || post.XKeenAutostart != "on" {
		primary := "post-finalize acceptance failed"
		if postErr != nil {
			primary += ": " + postErr.Error()
		}
		writeJSON(w, http.StatusBadGateway, networkApplyResponse{
			Success:           false,
			Applied:           true,
			Operation:         "finalize",
			SetupFinalizePlan: &post,
			PrimaryError:      primary,
			RollbackState:     "NOT_REQUESTED_HELPER_REPORTED_SUCCESS",
			Error:             "final setup completed but UI acceptance could not be confirmed",
		})
		return
	}

	writeJSON(w, http.StatusOK, networkApplyResponse{
		Success:           true,
		Applied:           true,
		Operation:         "finalize",
		Message:           "Настройка FreeNet завершена и проверена. Роутер готов к проверке после перезагрузки.",
		RollbackState:     "NOT_NEEDED",
		SetupFinalizePlan: &post,
	})
}
