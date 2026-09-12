package main

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Manual AUTO VPN checks can legitimately take longer than the UI server's
// normal WriteTimeout. Keep the long-running decision detached from the HTTP
// request and expose only a short start/status protocol to the browser.
var manualAutomationChecks operationCoordinator

type automationCheckStatusResponse struct {
	Success    bool                `json:"success"`
	Active     bool                `json:"active"`
	Operation *operationState     `json:"operation,omitempty"`
	Automation *automationResponse `json:"automation,omitempty"`
	Error      string              `json:"error,omitempty"`
}

func (a *app) handleAutomationCheckStart(w http.ResponseWriter, _ *http.Request) {
	settings := readAutomationSettings(a.cfg.ConfigPath)
	target := normalizeAutomationMode(settings.Mode)
	op, leader, conflict := manualAutomationChecks.begin("auto-vpn-check", target)
	if conflict != nil {
		writeJSON(w, http.StatusConflict, automationCheckStatusResponse{
			Success: false,
			Active:  true,
			Operation: conflict,
			Error:   "Другая AUTO VPN операция уже выполняется.",
		})
		return
	}
	if !leader {
		state, active, _ := manualAutomationChecks.snapshot()
		writeJSON(w, http.StatusAccepted, automationCheckStatusResponse{Success: true, Active: active, Operation: &state})
		return
	}

	go a.runManualAutomationCheck(op, target)
	state, active, _ := manualAutomationChecks.snapshot()
	writeJSON(w, http.StatusAccepted, automationCheckStatusResponse{Success: true, Active: active, Operation: &state})
}

func (a *app) runManualAutomationCheck(op *coordinatedOperation, mode string) {
	status := http.StatusOK
	success := true
	message := "AUTO VPN проверка завершена."
	errText := ""

	if mode == automationModeBest {
		if len(a.sem) > 0 {
			status = http.StatusConflict
			success = false
			errText = "FreeNet выполняет другую подтверждённую операцию."
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), automationBestTimeout+15*time.Second)
			_, err := a.runAutomationBestCycle(ctx, true)
			cancel()
			if err != nil {
				status = http.StatusBadGateway
				success = false
				snapshot := a.automationSnapshot()
				if strings.TrimSpace(snapshot.LastReason) != "" {
					errText = snapshot.LastReason
				} else {
					errText = "AUTO VPN проверка не завершена."
				}
			}
		}
	} else {
		helper, err := ensureAutomationHelper()
		if err != nil {
			status = http.StatusServiceUnavailable
			success = false
			errText = err.Error()
		} else {
			select {
			case a.sem <- struct{}{}:
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				out, runErr := exec.CommandContext(ctx, helper, "run").CombinedOutput()
				cancel()
				<-a.sem
				if runErr != nil {
					status = http.StatusBadGateway
					success = false
					errText = safeAutomationHelperError(out)
				}
			default:
				status = http.StatusConflict
				success = false
				errText = "FreeNet выполняет другую подтверждённую операцию."
			}
		}
	}

	if !success {
		message = "AUTO VPN проверка завершилась ошибкой."
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
