package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultAutomationStatePath   = "/opt/var/run/freenet-automation.state"
	defaultAutomationHistoryPath = "/opt/var/log/freenet-automation.history"
	defaultAutomationHelperPath  = "/opt/lib/freenet/auto_vpn.sh"
)

// The helper is shipped inside the UI binary so Web Self-Update can deploy the
// AUTO VPN runtime atomically. Settings v3 keeps legacy assets embedded for
// migration compatibility while presenting one human-facing automatic policy.
//go:embed web/automation.js web/automation-async.js web/runtime-acceptance.js web/settings-v3.js auto_vpn.sh
var automationWebFS embed.FS

type automationSettings struct {
	Enabled                bool     `json:"enabled"`
	Interval               string   `json:"interval"`
	CurrentProfileOnly     bool     `json:"current_profile_only"`
	AutoEndpointUpdate     bool     `json:"auto_endpoint_update"`
	AmbiguousNeedsApproval bool     `json:"ambiguous_needs_approval"`
	Mode                   string   `json:"mode"`
	Policy                 string   `json:"policy"`
	CountryScope           string   `json:"country_scope"`
	Countries              []string `json:"countries"`
	AutoApply              bool     `json:"auto_apply"`
}

type automationEvent struct {
	At      string `json:"at"`
	Kind    string `json:"kind"`
	Result  string `json:"result"`
	Message string `json:"message"`
}

type automationResponse struct {
	Success                 bool               `json:"success"`
	Settings                automationSettings `json:"settings"`
	CurrentProfile          string             `json:"current_profile"`
	CurrentEndpoint         string             `json:"current_endpoint"`
	CountryCode             string             `json:"country_code,omitempty"`
	LastRun                 string             `json:"last_run,omitempty"`
	NextRun                 string             `json:"next_run,omitempty"`
	LastSwitch              string             `json:"last_switch,omitempty"`
	LastResult              string             `json:"last_result,omitempty"`
	LastReason              string             `json:"last_reason,omitempty"`
	RollbackReady           bool               `json:"rollback_ready"`
	CurrentQualityKnown     bool               `json:"current_quality_known"`
	CurrentQualityFresh     bool               `json:"current_quality_fresh"`
	CurrentQualityCheckedAt string             `json:"current_quality_checked_at,omitempty"`
	CurrentEligible         bool               `json:"current_eligible"`
	CurrentLatencyMS        int                `json:"current_latency_ms,omitempty"`
	CurrentJitterMS         int                `json:"current_jitter_ms,omitempty"`
	CurrentDownloadMbps     float64            `json:"current_download_mbps,omitempty"`
	SubscriptionAuto        bool               `json:"subscription_auto"`
	GeoDataAuto             bool               `json:"geodata_auto"`
	GeoDataSchedule         string             `json:"geodata_schedule,omitempty"`
	FreeNetAuto             bool               `json:"freenet_auto"`
	LegacyEndpoint          bool               `json:"legacy_endpoint_scheduler"`
	Events                  []automationEvent  `json:"events"`
	Error                   string             `json:"error,omitempty"`
}

type automationUpdateRequest struct {
	Action         string   `json:"action"`
	Enabled        *bool    `json:"enabled,omitempty"`
	Interval       string   `json:"interval,omitempty"`
	Mode           string   `json:"mode,omitempty"`
	Policy         string   `json:"policy,omitempty"`
	CountryScope   string   `json:"country_scope,omitempty"`
	Countries      []string `json:"countries,omitempty"`
	AutoApply      *bool    `json:"auto_apply,omitempty"`
	GeoDataEnabled *bool    `json:"geodata_enabled,omitempty"`
}

func registerAutomationAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/automation", a.requireAuth(a.handleAutomationGet))
	mux.HandleFunc("POST /api/automation", a.requireAuth(a.handleAutomationPost))
	mux.HandleFunc("GET /api/automation/check", a.requireAuth(a.handleAutomationCheckStatus))
	mux.HandleFunc("GET /api/automation/assets/automation.js", serveAutomationAsset("web/automation.js"))
	mux.HandleFunc("GET /api/automation/assets/automation-async.js", serveAutomationAsset("web/automation-async.js"))
	mux.HandleFunc("GET /api/automation/assets/runtime-acceptance.js", serveAutomationAsset("web/runtime-acceptance.js"))
	mux.HandleFunc("GET /api/automation/assets/settings-v3.js", serveAutomationAsset("web/settings-v3.js"))
	registerSettingsV3API(mux, a)
	registerSettingsCountryCatalogAPI(mux, a)
	mux.HandleFunc("GET /accepted-ux.js", serveAcceptedUXWithAutomation)
}

func serveAcceptedUXWithAutomation(w http.ResponseWriter, r *http.Request) {
	data, err := webFS.ReadFile("web/accepted-ux.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
	_, _ = w.Write([]byte(`
;(() => {
  const loadSettingsV3 = () => {
    const v3 = document.createElement('script');
    v3.src = '/api/automation/assets/settings-v3.js';
    v3.async = false;
    document.head.appendChild(v3);
  };
  const loadAsyncAutomation = () => {
    const asyncScript = document.createElement('script');
    asyncScript.src = '/api/automation/assets/automation-async.js';
    asyncScript.async = false;
    asyncScript.onload = loadSettingsV3;
    asyncScript.onerror = loadSettingsV3;
    document.head.appendChild(asyncScript);
  };
  const loadAutomation = () => {
    const script = document.createElement('script');
    script.src = '/api/automation/assets/automation.js';
    script.async = false;
    script.onload = loadAsyncAutomation;
    script.onerror = loadAsyncAutomation;
    document.head.appendChild(script);
  };
  const patch = document.createElement('script');
  patch.src = '/api/automation/assets/runtime-acceptance.js';
  patch.async = false;
  patch.onload = loadAutomation;
  patch.onerror = loadAutomation;
  document.head.appendChild(patch);
})();
`))
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

func automationConfigValue(path, key, fallback string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == key {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return fallback
}

func automationBoolValue(path, key string, fallback bool) bool {
	defaultValue := "no"
	if fallback {
		defaultValue = "yes"
	}
	return automationConfigValue(path, key, defaultValue) == "yes"
}

func automationStatePath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_AUTOMATION_STATE")); value != "" {
		return value
	}
	return defaultAutomationStatePath
}

func automationHistoryPath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_AUTOMATION_HISTORY")); value != "" {
		return value
	}
	return defaultAutomationHistoryPath
}

func automationHelperPath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_AUTOMATION_HELPER")); value != "" {
		return value
	}
	return defaultAutomationHelperPath
}

func parseAutomationState(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if ok && key != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	return out
}

func readAutomationEvents(path string, limit int) []automationEvent {
	data, err := os.ReadFile(path)
	if err != nil {
		return []automationEvent{}
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n")
	events := make([]automationEvent, 0, limit)
	for i := len(lines) - 1; i >= 0 && len(events) < limit; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		events = append(events, automationEvent{At: parts[0], Kind: parts[1], Result: parts[2], Message: parts[3]})
	}
	return events
}

func writeAutomationJSON(w http.ResponseWriter, status int, payload any) {
	writeJSON(w, status, payload)
}

func automationSnapshotFromFiles(configPath, statePath, historyPath string) automationResponse {
	currentProfile := ""
	currentEndpoint := ""
	countryCode := ""
	currentQualityKnown := false
	currentQualityFresh := false
	currentQualityCheckedAt := ""
	currentEligible := false
	currentLatencyMS := 0
	currentJitterMS := 0
	currentDownloadMbps := 0.0
	rollbackReady := false
	state := parseAutomationState(statePath)
	currentProfile = state["CURRENT_PROFILE"]
	currentEndpoint = state["CURRENT_ENDPOINT"]
	countryCode = strings.ToLower(strings.TrimSpace(state["CURRENT_COUNTRY"])); if countryCode == "" { countryCode = profileCountryCode(currentProfile) }
	currentQualityKnown = state["CURRENT_QUALITY_KNOWN"] == "yes"
	currentQualityFresh = state["CURRENT_QUALITY_FRESH"] == "yes"
	currentQualityCheckedAt = state["CURRENT_QUALITY_CHECKED_AT"]
	currentEligible = state["CURRENT_ELIGIBLE"] == "yes"
	currentLatencyMS, _ = strconv.Atoi(state["CURRENT_LATENCY_MS"])
	currentJitterMS, _ = strconv.Atoi(state["CURRENT_JITTER_MS"])
	currentDownloadMbps, _ = strconv.ParseFloat(state["CURRENT_DOWNLOAD_MBPS"], 64)
	rollbackReady = state["ROLLBACK_READY"] == "yes"

	settings := automationSettings{
		Enabled: automationBoolValue(configPath, "AUTO_VPN_V1", false),
		Interval: automationConfigValue(configPath, "AUTO_VPN_V1_INTERVAL", "manual"),
		CurrentProfileOnly: automationBoolValue(configPath, "AUTO_VPN_CURRENT_PROFILE_ONLY", false),
		AutoEndpointUpdate: automationBoolValue(configPath, "AUTO_ENDPOINT_UPDATE", false),
		AmbiguousNeedsApproval: automationBoolValue(configPath, "AUTO_VPN_AMBIGUOUS_APPROVAL", true),
		Mode: normalizeAutomationMode(automationConfigValue(configPath, "AUTO_VPN_MODE", automationModeEndpoint)),
		Policy: normalizeAutomationPolicy(automationConfigValue(configPath, "AUTO_VPN_POLICY", automationPolicyDegraded)),
		CountryScope: normalizeAutomationCountryScope(automationConfigValue(configPath, "AUTO_VPN_COUNTRY_SCOPE", automationCountryRegion)),
		Countries: normalizeAutomationCountries(strings.Split(automationConfigValue(configPath, "AUTO_VPN_COUNTRIES", ""), ",")),
		AutoApply: automationBoolValue(configPath, "AUTO_VPN_AUTO_APPLY", true),
	}

	return automationResponse{
		Success: true,
		Settings: settings,
		CurrentProfile: currentProfile,
		CurrentEndpoint: currentEndpoint,
		CountryCode: countryCode,
		LastRun: state["LAST_RUN"], NextRun: state["NEXT_RUN"], LastSwitch: state["LAST_SWITCH"],
		LastResult: state["LAST_RESULT"], LastReason: state["LAST_REASON"], RollbackReady: rollbackReady,
		CurrentQualityKnown: currentQualityKnown, CurrentQualityFresh: currentQualityFresh,
		CurrentQualityCheckedAt: currentQualityCheckedAt, CurrentEligible: currentEligible,
		CurrentLatencyMS: currentLatencyMS, CurrentJitterMS: currentJitterMS, CurrentDownloadMbps: currentDownloadMbps,
		SubscriptionAuto: automationBoolValue(configPath, "AUTO_SUBSCRIPTION_REFRESH_ENABLED", false),
		GeoDataAuto: automationBoolValue(configPath, "AUTO_XKEEN_GEODATA", true),
		GeoDataSchedule: automationConfigValue(configPath, "AUTO_XKEEN_GEODATA_CRON", "0 */6 * * *"),
		FreeNetAuto: automationBoolValue(configPath, "AUTO_FREENET_CHECK_ENABLED", false),
		LegacyEndpoint: automationBoolValue(configPath, "AUTO_ENDPOINT_UPDATE", false),
		Events: readAutomationEvents(historyPath, 8),
	}
}

func (a *app) automationSnapshot() automationResponse {
	return automationSnapshotFromFiles(a.cfg.ConfigPath, automationStatePath(), automationHistoryPath())
}

func (a *app) handleAutomationGet(w http.ResponseWriter, _ *http.Request) {
	writeAutomationJSON(w, http.StatusOK, a.automationSnapshot())
}

func (a *app) handleAutomationPost(w http.ResponseWriter, r *http.Request) {
	if a.mutationBlockedBySelfUpdate(w) {
		return
	}
	if !sameOrigin(r) {
		writeAutomationJSON(w, http.StatusForbidden, automationResponse{Success: false, Error: "cross-origin request rejected"})
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeAutomationJSON(w, http.StatusUnsupportedMediaType, automationResponse{Success: false, Error: "application/json required"})
		return
	}
	body := http.MaxBytesReader(w, r.Body, 8192)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req automationUpdateRequest
	if err := dec.Decode(&req); err != nil {
		writeAutomationJSON(w, http.StatusBadRequest, automationResponse{Success: false, Error: "invalid request"})
		return
	}
	if req.Action == "check" {
		a.handleAutomationCheckStart(w, r)
		return
	}
	if req.Action != "save" {
		writeAutomationJSON(w, http.StatusBadRequest, automationResponse{Success: false, Error: "unsupported action"})
		return
	}
	if err := a.saveAutomationSettings(req); err != nil {
		writeAutomationJSON(w, http.StatusInternalServerError, automationResponse{Success: false, Error: "cannot save automation settings"})
		return
	}
	writeAutomationJSON(w, http.StatusOK, a.automationSnapshot())
}
