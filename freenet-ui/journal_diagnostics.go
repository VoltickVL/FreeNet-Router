package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Diagnostics is deliberately pull-only. It must never run network probes,
// update state, call AUTO VPN, or write additional journal entries.
const journalDiagnosticEventsPerSource = 750

type journalDiagnosticAuto struct {
	Enabled             bool   `json:"enabled"`
	Mode                string `json:"mode"`
	Policy              string `json:"policy"`
	HealthInterval      string `json:"health_interval"`
	EndpointInterval    string `json:"endpoint_interval"`
	LastRun             string `json:"last_run,omitempty"`
	LastResult          string `json:"last_result,omitempty"`
	LastReason          string `json:"last_reason,omitempty"`
	LastSwitch          string `json:"last_switch,omitempty"`
	RollbackReady       string `json:"rollback_ready,omitempty"`
	MutationBlocked     bool   `json:"mutation_blocked"`
	PostUpdateAck       bool   `json:"post_update_ack_recorded"`
	Health              journalHealthStatus `json:"watchdog"`
	HealthScheduledLast string `json:"health_scheduled_last,omitempty"`
	QualityDegradedCount int    `json:"quality_degraded_count,omitempty"`
	QualityDegradedSince string `json:"quality_degraded_since,omitempty"`
	QualityOptimizationLast string `json:"quality_optimization_last,omitempty"`
}

type journalDiagnosticUpdate struct {
	State         string `json:"state,omitempty"`
	FromVersion   string `json:"from_version,omitempty"`
	TargetVersion string `json:"target_version,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	RollbackState string `json:"rollback_state,omitempty"`
}

type journalDiagnosticReport struct {
	Schema        int                      `json:"schema"`
	GeneratedAt   string                   `json:"generated_at"`
	Version       string                   `json:"version"`
	Mode          string                   `json:"mode"`
	Window        string                   `json:"window"`
	AutoVPN       journalDiagnosticAuto    `json:"auto_vpn"`
	Update        journalDiagnosticUpdate  `json:"update"`
	Environment journalDiagnosticEnvironment `json:"environment"`
	SourceCount   map[string]int           `json:"source_event_count"`
	Events        []automationEvent        `json:"events"`
	TechnicalEvents []technicalJournalRecord `json:"technical_events"`
	TechnicalDropped uint64 `json:"technical_dropped"`
	TechnicalWriteErrors uint64 `json:"technical_write_errors"`
	Notes         []string                 `json:"notes"`
}

var (
	journalDiagnosticURI = regexp.MustCompile(`(?i)(?:vless|vmess|trojan|ss|socks5?|https?|wss?)://[^\s"'<>]+`)
	journalDiagnosticHost = regexp.MustCompile(`(?i)\b(?:[a-z0-9-]+\.)+[a-z]{2,}\b`)
	journalDiagnosticIP = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}(?::[0-9]{1,5})?\b`)
	journalDiagnosticUUID = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	journalDiagnosticSecret = regexp.MustCompile(`(?i)\b(token|secret|password|passwd|private[_-]?key|authorization|api[_-]?key|access[_-]?key|uuid)\s*[:=]\s*[^\s;,}]+\b`)
	journalDiagnosticBearer = regexp.MustCompile(`(?i)\bbearer\s+[^\s;,}]+`)
)

// Redact endpoint identity and credentials before creating a shareable report.
// This export does not contain raw config, Xray outbounds, URLs, or DNS secrets.
func journalDiagnosticRedact(input string) string {
	s := strings.ReplaceAll(strings.ReplaceAll(input, "\r", " "), "\n", " ")
	s = journalDiagnosticURI.ReplaceAllString(s, "[url-redacted]")
	s = journalDiagnosticSecret.ReplaceAllString(s, "$1=[redacted]")
	s = journalDiagnosticBearer.ReplaceAllString(s, "Bearer [redacted]")
	s = journalDiagnosticUUID.ReplaceAllString(s, "[id-redacted]")
	s = journalDiagnosticIP.ReplaceAllString(s, "[ip-redacted]")
	s = journalDiagnosticHost.ReplaceAllString(s, "[host-redacted]")
	r := []rune(s)
	if len(r) > 900 {
		s = string(r[:900]) + "...[truncated]"
	}
	return strings.TrimSpace(s)
}

func journalDiagnosticSafeEvent(e automationEvent) automationEvent {
	return automationEvent{
		At: e.At, Kind: journalDiagnosticRedact(e.Kind),
		Result: journalDiagnosticRedact(e.Result),
		Message: journalDiagnosticRedact(e.Message),
	}
}

// A fixed, bounded, non-destructive snapshot of pre-existing events. Unlike
// canonicalJournalEvents, this intentionally does not deduplicate or suppress
// AUTO VPN incident stages. No changes to the writer or AUTO state machine.
func (a *app) journalDiagnostics(now time.Time) journalDiagnosticReport {
	autoHistory := readAutomationEvents(automationHistoryPath(), journalDiagnosticEventsPerSource)
	systemHistory := readAutomationEvents(settingsV3HistoryPath(), journalDiagnosticEventsPerSource)
	merged := v3MergeEvents(0, autoHistory, systemHistory)
	events := make([]automationEvent, 0, len(merged))
	for _, event := range merged {
		events = append(events, journalDiagnosticSafeEvent(event))
	}

	state := parseAutomationState(automationStatePath())
	healthState := v3ParseState(settingsV3StatePath())
	updateState := readStateFile(a.cfg.UpdateState)
	settings := readAutomationSettings(a.cfg.ConfigPath)
	qualityCount, _ := strconv.Atoi(strings.TrimSpace(healthState["QUALITY_DEGRADED_COUNT"]))
	if qualityCount < 0 { qualityCount = 0 }

	healthInterval := configuredAutomationHealthInterval(a.cfg.ConfigPath)
	return journalDiagnosticReport{
		Schema: 1,
		GeneratedAt: now.UTC().Format(time.RFC3339),
		Version: version,
		Mode: "read-only",
		Window: "latest 750 original events per source; no deduplication",
		AutoVPN: journalDiagnosticAuto{
			Enabled: settings.Enabled,
			Mode: settings.Mode,
			Policy: settings.Policy,
			HealthInterval: healthInterval,
			EndpointInterval: settings.Interval,
			LastRun: state["LAST_RUN"],
			LastResult: journalDiagnosticRedact(state["LAST_RESULT"]),
			LastReason: journalDiagnosticRedact(state["LAST_REASON"]),
			LastSwitch: state["LAST_SWITCH"],
			RollbackReady: journalDiagnosticRedact(state["ROLLBACK_READY"]),
			MutationBlocked: strings.EqualFold(state["MUTATION_BLOCKED"], "yes"),
			PostUpdateAck: strings.TrimSpace(state["POST_UPDATE_ACK"]) != "",
			Health: journalHealthFreshness(settings.Enabled, automationHealthIntervalDuration(healthInterval), healthState, now),
			HealthScheduledLast: healthState["HEALTH_SCHEDULE_LAST"],
			QualityDegradedCount: qualityCount,
			QualityDegradedSince: healthState["QUALITY_DEGRADED_SINCE"],
			QualityOptimizationLast: healthState["QUALITY_OPTIMIZATION_LAST"],
		},
		Update: journalDiagnosticUpdate{
			State: journalDiagnosticRedact(updateState["STATE"]),
			FromVersion: journalDiagnosticRedact(updateState["FROM_VERSION"]),
			TargetVersion: journalDiagnosticRedact(updateState["TARGET_VERSION"]),
			UpdatedAt: updateState["UPDATED_AT"],
			RollbackState: journalDiagnosticRedact(updateState["ROLLBACK_STATE"]),
		},
		Environment: a.journalDiagnosticEnvironment(),
		SourceCount: map[string]int{"auto": len(autoHistory), "settings": len(systemHistory)},
		Events: events,
		TechnicalEvents: readTechnicalJournalRecords(technicalJournalPath(), technicalJournalMaxExportLines),
		TechnicalDropped: technicalJournalDropped.Load(),
		TechnicalWriteErrors: technicalJournalWriteErrors.Load(),
		Notes: []string{
			"Historical observations only: no new VPN probe or router connectivity acceptance was performed.",
			"Original on-disk journals are unchanged; these entries bypass display deduplication.",
			"Only an allowlist of state fields is included; credentials and endpoint addresses are redacted.",
			"Source counts indicate selected tail events, not all historical events.",
		},
	}
}

func (a *app) handleJournalDiagnostics(w http.ResponseWriter, _ *http.Request) {
	report := a.journalDiagnostics(time.Now().UTC())
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		http.Error(w, "diagnostics encoding failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="freenet-diagnostics-`+time.Now().UTC().Format("20060102T150405Z")+`.json"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(payload, '\n'))
}

