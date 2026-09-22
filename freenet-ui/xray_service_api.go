package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const xrayRestartTimeout = 45 * time.Second

var xrayServiceProcessRunning = processRunning

type xrayServiceActionRequest struct {
	Action string `json:"action"`
}

type xrayServiceResponse struct {
	Success bool              `json:"success"`
	Online  bool              `json:"online"`
	Version string            `json:"version,omitempty"`
	Message string            `json:"message,omitempty"`
	Events  []automationEvent `json:"events"`
	Error   string            `json:"error,omitempty"`
}

func registerXrayServiceAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/xray/service", a.requireAuth(a.handleXrayServiceGet))
	mux.HandleFunc("POST /api/xray/service", a.requireAuth(a.handleXrayServicePost))
	mux.HandleFunc("GET /api/xray/core/catalog", a.requireAuth(a.handleXrayCoreCatalog))
	mux.HandleFunc("POST /api/xray/core/apply", a.requireAuth(a.handleXrayCoreApply))
}

func xrayServiceEvents(limit int) []automationEvent {
	all := readAutomationEvents(settingsV3HistoryPath(), 64)
	out := make([]automationEvent, 0, limit)
	for _, event := range all {
		kind := strings.ToLower(strings.TrimSpace(event.Kind))
		if kind != "xray" && kind != "config_studio" {
			continue
		}
		out = append(out, event)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func (a *app) xrayServiceSnapshot(ctx context.Context) xrayServiceResponse {
	status := a.configStudioXrayStatus(ctx)
	return xrayServiceResponse{Success: true, Online: status.Online, Version: status.Version, Events: xrayServiceEvents(8)}
}

func (a *app) handleXrayServiceGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.xrayServiceSnapshot(r.Context()))
}

func decodeXrayServiceAction(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, xrayServiceResponse{Success: false, Events: []automationEvent{}, Error: "cross-origin request rejected"})
		return "", false
	}
	if ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))); !strings.HasPrefix(ct, "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, xrayServiceResponse{Success: false, Events: []automationEvent{}, Error: "application/json required"})
		return "", false
	}
	body := http.MaxBytesReader(w, r.Body, 8<<10)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req xrayServiceActionRequest
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, xrayServiceResponse{Success: false, Events: []automationEvent{}, Error: "invalid action request"})
		return "", false
	}
	if strings.TrimSpace(req.Action) != "restart" {
		writeJSON(w, http.StatusBadRequest, xrayServiceResponse{Success: false, Events: []automationEvent{}, Error: "unsupported action"})
		return "", false
	}
	return "restart", true
}

func waitForXrayOnline(ctx context.Context) bool {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if xrayServiceProcessRunning("xray") {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func (a *app) restartXrayControlled(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, xrayRestartTimeout)
	defer cancel()
	if err := a.validateConfigStudioLive(ctx); err != nil {
		return errors.New("текущая конфигурация Xray не прошла проверку; перезапуск отменён")
	}
	if _, err := runCommand(ctx, a.cfg.XKeenPath, "-restart"); err != nil {
		return errors.New("Xray не удалось перезапустить")
	}
	if !waitForXrayOnline(ctx) {
		return errors.New("Xray не вернулся в рабочее состояние после перезапуска")
	}
	if err := a.validateConfigStudioLive(ctx); err != nil {
		return errors.New("Xray запущен, но post-check конфигурации не подтверждён")
	}
	return nil
}

func (a *app) handleXrayServicePost(w http.ResponseWriter, r *http.Request) {
	if a.mutationBlockedBySelfUpdate(w) { return }
	if _, ok := decodeXrayServiceAction(w, r); !ok { return }
	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	default:
		writeJSON(w, http.StatusConflict, xrayServiceResponse{Success: false, Events: xrayServiceEvents(8), Error: "другая операция FreeNet уже выполняется"})
		return
	}
	releaseMutation, lockErr := acquireVPNMutationLock()
	if lockErr != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(lockErr, errVPNMutationBusy) {
			status = http.StatusConflict
		}
		writeJSON(w, status, xrayServiceResponse{Success: false, Events: xrayServiceEvents(8), Error: vpnMutationLockMessage(lockErr)})
		return
	}
	defer releaseMutation()
	if err := a.restartXrayControlled(r.Context()); err != nil {
		message := sanitizeAutomationReason(err.Error())
		v3AppendEvent("xray", "failed", message)
		resp := a.xrayServiceSnapshot(r.Context())
		resp.Success = false
		resp.Error = message
		writeJSON(w, http.StatusBadGateway, resp)
		return
	}
	v3AppendEvent("xray", "success", "Xray перезапущен через FreeNet.")
	resp := a.xrayServiceSnapshot(r.Context())
	resp.Message = "Xray перезапущен и снова работает."
	writeJSON(w, http.StatusOK, resp)
}