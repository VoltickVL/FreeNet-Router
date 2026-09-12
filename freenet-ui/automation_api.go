package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAutomationStatePath   = "/opt/var/run/freenet-automation.state"
	defaultAutomationHistoryPath = "/opt/var/log/freenet-automation.history"
	defaultAutomationHelperPath  = "/opt/lib/freenet/auto_vpn.sh"
)

// The helper is shipped inside the UI binary so Web Self-Update can deploy the
// whole AUTO VPN v1 runtime atomically without introducing a second asset path.
//go:embed web/accepted-ux-core.js web/automation.js auto_vpn.sh
var automationWebFS embed.FS

type automationSettings struct {
	Enabled                bool   `json:"enabled"`
	Interval               string `json:"interval"`
	CurrentProfileOnly     bool   `json:"current_profile_only"`
	AutoEndpointUpdate     bool   `json:"auto_endpoint_update"`
	AmbiguousNeedsApproval bool   `json:"ambiguous_needs_approval"`
}

type automationEvent struct {
	At      string `json:"at"`
	Kind    string `json:"kind"`
	Result  string `json:"result"`
	Message string `json:"message"`
}

type automationResponse struct {
	Success          bool               `json:"success"`
	Settings         automationSettings `json:"settings"`
	CurrentProfile   string             `json:"current_profile"`
	CurrentEndpoint  string             `json:"current_endpoint"`
	CountryCode      string             `json:"country_code,omitempty"`
	LastRun          string             `json:"last_run,omitempty"`
	NextRun          string             `json:"next_run,omitempty"`
	LastResult       string             `json:"last_result,omitempty"`
	LastReason       string             `json:"last_reason,omitempty"`
	RollbackReady    bool               `json:"rollback_ready"`
	SubscriptionAuto bool               `json:"subscription_auto"`
	GeoDataAuto      bool               `json:"geodata_auto"`
	GeoDataSchedule  string             `json:"geodata_schedule,omitempty"`
	FreeNetAuto      bool               `json:"freenet_auto"`
	LegacyEndpoint   bool               `json:"legacy_endpoint_scheduler"`
	Events           []automationEvent  `json:"events"`
	Error            string             `json:"error,omitempty"`
}

type automationUpdateRequest struct {
	Action   string `json:"action"`
	Enabled  *bool  `json:"enabled,omitempty"`
	Interval string `json:"interval,omitempty"`
}

func registerAutomationAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/automation", a.requireAuth(a.handleAutomationGet))
	mux.HandleFunc("POST /api/automation", a.requireAuth(a.handleAutomationPost))
	mux.HandleFunc("GET /api/automation/assets/accepted-ux-core.js", serveAutomationAsset("web/accepted-ux-core.js"))
	mux.HandleFunc("GET /api/automation/assets/automation.js", serveAutomationAsset("web/automation.js"))
}

func serveAutomationAsset(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := automationWebFS.ReadFile(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	}
}

func automationHelperPath() string {
	if p := strings.TrimSpace(os.Getenv("FREENET_AUTO_VPN_HELPER")); p != "" {
		return p
	}
	return defaultAutomationHelperPath
}

func ensureAutomationHelper() (string, error) {
	path := automationHelperPath()
	if strings.TrimSpace(os.Getenv("FREENET_AUTO_VPN_HELPER")) != "" {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			return "", errors.New("configured AUTO VPN helper is unavailable")
		}
		return path, nil
	}
	data, err := automationWebFS.ReadFile("auto_vpn.sh")
	if err != nil {
		return "", errors.New("embedded AUTO VPN helper is unavailable")
	}
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		_ = os.Chmod(path, 0755)
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", errors.New("cannot prepare AUTO VPN helper directory")
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0755); err != nil {
		return "", errors.New("cannot stage AUTO VPN helper")
	}
	if err := os.Chmod(tmp, 0755); err != nil {
		_ = os.Remove(tmp)
		return "", errors.New("cannot mark AUTO VPN helper executable")
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", errors.New("cannot install AUTO VPN helper")
	}
	return path, nil
}

func automationStatePath() string {
	if p := strings.TrimSpace(os.Getenv("FREENET_AUTOMATION_STATE")); p != "" {
		return p
	}
	return defaultAutomationStatePath
}

func automationHistoryPath() string {
	if p := strings.TrimSpace(os.Getenv("FREENET_AUTOMATION_HISTORY")); p != "" {
		return p
	}
	return defaultAutomationHistoryPath
}

func automationConfigValue(path, key, fallback string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	value := fallback
	prefix := key + "="
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, prefix) {
			value = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, prefix)), "'\"")
		}
	}
	return value
}

func automationIntervalFromCron(cron string) string {
	switch strings.TrimSpace(cron) {
	case "*/30 * * * *":
		return "30m"
	case "0 * * * *":
		return "1h"
	case "0 */3 * * *":
		return "3h"
	case "0 */6 * * *":
		return "6h"
	default:
		return "manual"
	}
}

func automationCron(interval string) (string, bool) {
	switch strings.TrimSpace(interval) {
	case "30m":
		return "*/30 * * * *", true
	case "1h":
		return "0 * * * *", true
	case "3h":
		return "0 */3 * * *", true
	case "6h":
		return "0 */6 * * *", true
	case "manual":
		return "", true
	default:
		return "", false
	}
}

func parseAutomationState(path string) map[string]string {
	values := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return values
	}
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if !ok {
			continue
		}
		switch key {
		case "LAST_RUN", "LAST_RESULT", "LAST_REASON", "ROLLBACK_READY":
			values[key] = strings.TrimSpace(value)
		}
	}
	return values
}

func readAutomationEvents(path string, limit int) []automationEvent {
	if limit <= 0 {
		limit = 8
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return []automationEvent{}
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(data), "\r", "")), "\n")
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	events := make([]automationEvent, 0, len(lines))
	for i := len(lines) - 1; i >= 0; i-- {
		parts := strings.SplitN(lines[i], "\t", 4)
		if len(parts) != 4 {
			continue
		}
		events = append(events, automationEvent{At: parts[0], Kind: parts[1], Result: parts[2], Message: parts[3]})
	}
	return events
}

func automationNextRun(lastRun, interval string) string {
	if lastRun == "" || interval == "manual" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, lastRun)
	if err != nil {
		return ""
	}
	var d time.Duration
	switch interval {
	case "30m":
		d = 30 * time.Minute
	case "1h":
		d = time.Hour
	case "3h":
		d = 3 * time.Hour
	case "6h":
		d = 6 * time.Hour
	}
	if d == 0 {
		return ""
	}
	return t.Add(d).Format(time.RFC3339)
}

func (a *app) automationSnapshot() automationResponse {
	status := a.status()
	enabledRaw := automationConfigValue(a.cfg.ConfigPath, "AUTO_VPN_V1", "")
	enabled := enabledRaw == "yes"
	interval := automationConfigValue(a.cfg.ConfigPath, "AUTO_VPN_V1_INTERVAL", "manual")
	if _, ok := automationCron(interval); !ok {
		interval = "manual"
	}
	legacyEnabled := enabledRaw == "" && automationConfigValue(a.cfg.ConfigPath, "AUTO_ENDPOINT_UPDATE", "no") == "yes"
	state := parseAutomationState(automationStatePath())
	geodata := automationConfigValue(a.cfg.ConfigPath, "AUTO_XKEEN_GEODATA", "yes") == "yes"
	return automationResponse{
		Success: true,
		Settings: automationSettings{
			Enabled: enabled, Interval: interval, CurrentProfileOnly: true,
			AutoEndpointUpdate: true, AmbiguousNeedsApproval: true,
		},
		CurrentProfile: status.ProfileLabel, CurrentEndpoint: status.Endpoint, CountryCode: status.CountryCode,
		LastRun: state["LAST_RUN"], NextRun: automationNextRun(state["LAST_RUN"], interval),
		LastResult: state["LAST_RESULT"], LastReason: state["LAST_REASON"], RollbackReady: state["ROLLBACK_READY"] == "yes",
		SubscriptionAuto: false,
		GeoDataAuto: geodata, GeoDataSchedule: automationConfigValue(a.cfg.ConfigPath, "AUTO_XKEEN_GEODATA_CRON", "30 6 * * *"),
		FreeNetAuto: false,
		LegacyEndpoint: legacyEnabled,
		Events: readAutomationEvents(automationHistoryPath(), 8),
	}
}

func (a *app) handleAutomationGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.automationSnapshot())
}

func (a *app) handleAutomationPost(w http.ResponseWriter, r *http.Request) {
	var req automationUpdateRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, automationResponse{Success: false, Error: "invalid automation request"})
		return
	}

	helper, err := ensureAutomationHelper()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, automationResponse{Success: false, Error: err.Error()})
		return
	}

	switch strings.TrimSpace(req.Action) {
	case "save":
		if req.Enabled == nil {
			writeJSON(w, http.StatusBadRequest, automationResponse{Success: false, Error: "enabled is required"})
			return
		}
		cron, ok := automationCron(req.Interval)
		if !ok {
			writeJSON(w, http.StatusBadRequest, automationResponse{Success: false, Error: "unsupported automation interval"})
			return
		}
		args := []string{"configure", "--enabled", strconv.FormatBool(*req.Enabled), "--interval", req.Interval}
		if cron != "" {
			args = append(args, "--cron", cron)
		}
		out, err := exec.Command(helper, args...).CombinedOutput()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, automationResponse{Success: false, Error: safeAutomationHelperError(out)})
			return
		}
		writeJSON(w, http.StatusOK, a.automationSnapshot())
	case "check":
		select {
		case a.sem <- struct{}{}:
			defer func() { <-a.sem }()
		default:
			writeJSON(w, http.StatusConflict, automationResponse{Success: false, Error: "another FreeNet operation is already running"})
			return
		}
		out, err := exec.Command(helper, "run").CombinedOutput()
		if err != nil {
			snapshot := a.automationSnapshot()
			snapshot.Success = false
			snapshot.Error = safeAutomationHelperError(out)
			writeJSON(w, http.StatusBadGateway, snapshot)
			return
		}
		writeJSON(w, http.StatusOK, a.automationSnapshot())
	default:
		writeJSON(w, http.StatusBadRequest, automationResponse{Success: false, Error: "unsupported automation action"})
	}
}

func safeAutomationHelperError(out []byte) string {
	text := sanitizeOutput(string(out))
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "ERROR=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "ERROR="))
		}
	}
	if strings.TrimSpace(text) != "" {
		return "automation operation failed"
	}
	return "automation helper failed"
}
