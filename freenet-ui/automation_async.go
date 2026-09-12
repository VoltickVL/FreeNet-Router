package main

import (
	"context"
	"net/http"
	"time"
)

// Manual AUTO VPN checks can legitimately take longer than the UI server's
// normal WriteTimeout. Keep the decision detached from the HTTP request and
// expose only a short start/status protocol to the browser.
var manualAutomationChecks operationCoordinator

type automationCheckStatusResponse struct {
	Success    bool                 `json:"success"`
	Active     bool                 `json:"active"`
	Operation  *operationState      `json:"operation,omitempty"`
	Automation *automationResponse  `json:"automation,omitempty"`
	Error      string               `json:"error,omitempty"`
}

func (a *app) handleAutomationCheckStart(w http.ResponseWriter, _ *http.Request) {
	op, leader, conflict := manualAutomationChecks.begin("auto-vpn-check", "health")
	if conflict != nil {
		writeJSON(w, http.StatusConflict, automationCheckStatusResponse{
			Success: false,
			Active: true,
			Operation: conflict,
			Error: "Другая AUTO VPN операция уже выполняется.",
		})
		return
	}
	if !leader {
		state, active, _ := manualAutomationChecks.snapshot()
		writeJSON(w, http.StatusAccepted, automationCheckStatusResponse{Success: true, Active: active, Operation: &state})
		return
	}

	go a.runManualAutomationCheck(op)
	state, active, _ := manualAutomationChecks.snapshot()
	writeJSON(w, http.StatusAccepted, automationCheckStatusResponse{Success: true, Active: active, Operation: &state})
}

func (a *app) runManualAutomationCheck(op *coordinatedOperation) {
	status := http.StatusOK
	success := true
	message := "Проверка текущего VPN завершена."
	errText := ""

	if len(a.sem) > 0 {
		status = http.StatusConflict
		success = false
		errText = "FreeNet выполняет другую подтверждённую операцию."
	} else {
		settings := readAutomationSettings(a.cfg.ConfigPath)
		if settings.Enabled {
			ctx, cancel := context.WithTimeout(context.Background(), automationBestTimeout+4*time.Minute)
			result, err := a.runAutomationHealthWatch(ctx)
			cancel()
			if err != nil {
				status = http.StatusBadGateway
				success = false
				errText = result.Reason
				if errText == "" {
					errText = "AUTO VPN не смог завершить безопасную проверку."
				}
			} else {
				message = result.Reason
			}
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			probe := a.probeAutomationCurrentVPN(ctx)
			cancel()
			result := automationHealthResult{State: probe.State, Reason: probe.Reason}
			recordSettingsV3Health(result)
			message = probe.Reason
			if probe.State == automationHealthFailed {
				status = http.StatusBadGateway
				success = false
				errText = probe.Reason
			}
		}
	}

	if !success {
		message = "Проверка текущего VPN завершилась ошибкой."
	}
	manualAutomationChecks.finish(op, status, nil, success, message, errText)
}

func (a *app) handleAutomationCheckStatus(w http.ResponseWriter, _ *http.Request) {
	state, active, ok := manualAutomationChecks.snapshot()
	if !ok {
		snapshot := a.automationSnapshot()
		writeJSON(w, http.StatusOK, automationCheckStatusResponse{Success: true, Active: false, Automation: &snapshot})
		return
	}
	response := automationCheckStatusResponse{Success: state.State != "failed", Active: active, Operation: &state}
	if !active {
		snapshot := a.automationSnapshot()
		response.Automation = &snapshot
		if state.State == "failed" {
			response.Error = state.Error
		}
	}
	writeJSON(w, http.StatusOK, response)
}
