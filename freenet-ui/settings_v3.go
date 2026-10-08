package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	settingsV3StatePathDefault    = "/opt/var/run/freenet-settings-v3.state"
	settingsV3HistoryPathDefault  = "/opt/var/log/freenet-settings-v3.history"
	settingsV3BackupRootDefault   = "/opt/backups/freenet-settings"
	settingsV3UpdaterDefault      = "/opt/bin/blanc_xkeen_update_outbounds.sh"
	journalHistoryFileLimit         = 20000
	journalCanonicalRetentionLimit  = 15000
	journalDefaultPageSize          = 100
	journalMaxPageSize              = 500
	journalSemanticDedupeWindow          = 15 * time.Second
	journalRoutineHeartbeatWindow        = 6 * time.Hour
	journalPendingQualityHeartbeatWindow = 6 * time.Hour
)

var journalHistoryMu sync.Mutex

type settingsV3Schedule struct {
	Enabled  bool   `json:"enabled"`
	Interval string `json:"interval"`
	LastRun  string `json:"last_run,omitempty"`
	NextRun  string `json:"next_run,omitempty"`
	Result   string `json:"result,omitempty"`
	Message  string `json:"message,omitempty"`
}

type settingsV3AutoVPN struct {
	Enabled          bool     `json:"enabled"`
	Mode             string   `json:"mode"`
	HealthInterval   string   `json:"health_interval"`
	EndpointInterval string   `json:"endpoint_interval"`
	CountryScope     string   `json:"country_scope"`
	Countries        []string `json:"countries"`
	LastHealth       string   `json:"last_health,omitempty"`
	NextHealth       string   `json:"next_health,omitempty"`
}

type settingsV3BackupInfo struct {
	Root       string `json:"root"`
	Latest     string `json:"latest,omitempty"`
	LatestPath string `json:"latest_path,omitempty"`
	Tracked    int    `json:"tracked"`
}

type settingsV3Response struct {
	Success      bool                 `json:"success"`
	AutoVPN      settingsV3AutoVPN    `json:"auto_vpn"`
	Automation   automationResponse   `json:"automation"`
	Subscription settingsV3Schedule   `json:"subscription"`
	GeoData      settingsV3Schedule   `json:"geodata"`
	FreeNet      settingsV3Schedule   `json:"freenet"`
	Backup       settingsV3Schedule   `json:"backup"`
	BackupInfo   settingsV3BackupInfo `json:"backup_info"`
	Events       []automationEvent    `json:"events"`
	Error        string               `json:"error,omitempty"`
}

type journalStats struct {
	Total    int            `json:"total"`
	Success  int            `json:"success"`
	Neutral  int            `json:"neutral"`
	Errors   int            `json:"errors"`
	ByKind   map[string]int `json:"by_kind"`
	ByResult map[string]int `json:"by_result"`
}

type journalResponse struct {
	Success       bool              `json:"success"`
	Events        []automationEvent `json:"events"`
	GeneratedAt   string            `json:"generated_at"`
	Total         int               `json:"total"`
	FilteredTotal int               `json:"filtered_total"`
	Page          int               `json:"page"`
	PageSize      int               `json:"page_size"`
	Pages         int               `json:"pages"`
	RetainedFrom  string            `json:"retained_from,omitempty"`
	RetainedTo    string            `json:"retained_to,omitempty"`
	RangeFrom     string            `json:"range_from,omitempty"`
	RangeTo       string            `json:"range_to,omitempty"`
	Stats         journalStats      `json:"stats"`
	Health        journalHealthStatus `json:"health"`
}

// Journal rows are intentionally compacted: keep live health freshness
// separate from the latest significant event, with no credentials/endpoints.
type journalHealthStatus struct {
	Enabled         bool   `json:"enabled"`
	LastCompleted   string `json:"last_completed,omitempty"`
	LastScheduled   string `json:"last_scheduled,omitempty"`
	Result          string `json:"result,omitempty"`
	IntervalSeconds int    `json:"interval_seconds"`
	Freshness       string `json:"freshness"`
}

type journalQuery struct {
	Page       int
	PageSize   int
	Category   string
	Result     string
	Search     string
	From       time.Time
	To         time.Time
	HasFrom    bool
	HasTo      bool
}

type settingsV3SaveRequest struct {
	Action                   string   `json:"action"`
	AutoVPNEnabled           *bool    `json:"auto_vpn_enabled,omitempty"`
	AutoVPNMode              string   `json:"auto_vpn_mode,omitempty"`
	AutoVPNHealthInterval    string   `json:"auto_vpn_health_interval,omitempty"`
	AutoVPNEndpointInterval  string   `json:"auto_vpn_endpoint_interval,omitempty"`
	CountryScope             string   `json:"country_scope,omitempty"`
	Countries            []string `json:"countries,omitempty"`
	SubscriptionEnabled  *bool    `json:"subscription_enabled,omitempty"`
	SubscriptionInterval string   `json:"subscription_interval,omitempty"`
	GeoDataEnabled       *bool    `json:"geodata_enabled,omitempty"`
	GeoDataInterval      string   `json:"geodata_interval,omitempty"`
	FreeNetEnabled       *bool    `json:"freenet_enabled,omitempty"`
	FreeNetInterval      string   `json:"freenet_interval,omitempty"`
	BackupEnabled        *bool    `json:"backup_enabled,omitempty"`
	BackupInterval       string   `json:"backup_interval,omitempty"`
}

type settingsV3ActionRequest struct {
	Action string `json:"action"`
}

type settingsV3ActionResponse struct {
	Success           bool                  `json:"success"`
	Message           string                `json:"message,omitempty"`
	Profiles          []subscriptionProfile `json:"profiles,omitempty"`
	ProfilesAvailable int                   `json:"profiles_available,omitempty"`
	ProfilesStale     bool                  `json:"profiles_stale,omitempty"`
	ProfilesUpdatedAt string                `json:"profiles_updated_at,omitempty"`
	BackupInfo        *settingsV3BackupInfo `json:"backup_info,omitempty"`
	Error             string                `json:"error,omitempty"`
}

func registerSettingsV3API(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/settings-v3", a.requireAuth(a.handleSettingsV3Get))
	mux.HandleFunc("GET /api/journal", a.requireAuth(a.handleJournalGet))
	mux.HandleFunc("GET /api/journal/export", a.requireAuth(a.handleJournalExport))
	mux.HandleFunc("POST /api/settings-v3", a.requireAuth(a.handleSettingsV3Save))
	mux.HandleFunc("POST /api/settings-v3/action", a.requireAuth(a.handleSettingsV3Action))
}

func settingsV3StatePath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_SETTINGS_V3_STATE")); value != "" {
		return value
	}
	return settingsV3StatePathDefault
}

func settingsV3HistoryPath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_SETTINGS_V3_HISTORY")); value != "" {
		return value
	}
	return settingsV3HistoryPathDefault
}

func settingsV3BackupRoot() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_SETTINGS_BACKUP_ROOT")); value != "" {
		return value
	}
	return settingsV3BackupRootDefault
}

func latestV3BackupSnapshot() (string, string, error) {
	root := settingsV3BackupRoot()
	latestRaw, err := os.ReadFile(filepath.Join(root, "latest"))
	if err != nil {
		return "", "", errors.New("backup is unavailable")
	}
	name := strings.TrimSpace(string(latestRaw))
	if name == "" || filepath.Base(name) != name || !strings.HasPrefix(name, "backup-") {
		return "", "", errors.New("backup reference is invalid")
	}
	source := filepath.Join(root, name)
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return "", "", errors.New("backup directory is unavailable")
	}
	return name, source, nil
}

func (a *app) v3BackupInfo() settingsV3BackupInfo {
	info := settingsV3BackupInfo{
		Root:    settingsV3BackupRoot(),
		Tracked: len(a.v3BackupTrackedFiles()),
	}
	name, path, err := latestV3BackupSnapshot()
	if err == nil {
		info.Latest = name
		info.LatestPath = path
	}
	return info
}

func settingsV3UpdaterPath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_SUBSCRIPTION_UPDATER")); value != "" {
		return value
	}
	return settingsV3UpdaterDefault
}

func v3Bool(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

const defaultAutomationHealthInterval = "1m"

func automationHealthIntervalDuration(interval string) time.Duration {
	switch strings.TrimSpace(interval) {
	case "30s":
		return 30 * time.Second
	case "1m":
		return time.Minute
	case "5m":
		return 5 * time.Minute
	default:
		return 0
	}
}

func normalizeAutomationHealthInterval(interval string) string {
	if automationHealthIntervalDuration(interval) > 0 {
		return strings.TrimSpace(interval)
	}
	return defaultAutomationHealthInterval
}

func automationHealthCron(interval string) string {
	switch normalizeAutomationHealthInterval(interval) {
	case "5m":
		return "*/5 * * * *"
	default:
		// Cron is a resilience fallback. The 30-second cadence is owned by the
		// long-running FreeNet service; once-per-minute fallback keeps recovery
		// alive across a brief service restart without using sleep-based cron hacks.
		return "* * * * *"
	}
}

func configuredAutomationHealthInterval(configPath string) string {
	return normalizeAutomationHealthInterval(automationConfigValue(configPath, "AUTO_VPN_HEALTH_INTERVAL", defaultAutomationHealthInterval))
}

func v3IntervalDuration(interval string) time.Duration {
	switch strings.TrimSpace(interval) {
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "3h":
		return 3 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "12h":
		return 12 * time.Hour
	case "24h":
		return 24 * time.Hour
	default:
		return 0
	}
}

func v3IntervalCron(interval string) (string, bool) {
	switch strings.TrimSpace(interval) {
	case "30m":
		return "*/30 * * * *", true
	case "1h":
		return "0 * * * *", true
	case "3h":
		return "0 */3 * * *", true
	case "6h":
		return "0 */6 * * *", true
	case "12h":
		return "0 */12 * * *", true
	case "24h":
		return "17 4 * * *", true
	default:
		return "", false
	}
}

func v3IntervalCronOffset(interval string, minute int) (string, bool) {
	if minute < 0 || minute > 29 {
		return "", false
	}
	switch strings.TrimSpace(interval) {
	case "30m":
		return fmt.Sprintf("%d,%d * * * *", minute, minute+30), true
	case "1h":
		return fmt.Sprintf("%d * * * *", minute), true
	case "3h":
		return fmt.Sprintf("%d */3 * * *", minute), true
	case "6h":
		return fmt.Sprintf("%d */6 * * *", minute), true
	case "12h":
		return fmt.Sprintf("%d */12 * * *", minute), true
	case "24h":
		return fmt.Sprintf("%d 4 * * *", minute), true
	default:
		return "", false
	}
}

func v3DefaultInterval(key string) string {
	switch key {
	case "auto_vpn_endpoint":
		return "1h"
	case "subscription":
		return "6h"
	case "geodata":
		return "3h"
	case "freenet":
		return "12h"
	case "backup":
		return "24h"
	default:
		return "6h"
	}
}

func v3NormalizeInterval(value, key string) string {
	if v3IntervalDuration(value) > 0 {
		return strings.TrimSpace(value)
	}
	return v3DefaultInterval(key)
}

func v3ParseState(path string) map[string]string {
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

func v3WriteState(values map[string]string) error {
	path := settingsV3StatePath()
	current := v3ParseState(path)
	for key, value := range values {
		current[key] = sanitizeAutomationReason(value)
	}
	keys := make([]string, 0, len(current))
	for key := range current {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "%s=%s\n", key, strings.ReplaceAll(current[key], "\n", " "))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(b.String()), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func appendBoundedJournalLine(path, line string) {
	journalHistoryMu.Lock()
	defer journalHistoryMu.Unlock()

	_ = os.MkdirAll(filepath.Dir(path), 0755)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	_, _ = file.WriteString(line)
	_ = file.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(data), "\r", "")), "\n")
	if len(lines) <= journalHistoryFileLimit {
		return
	}
	lines = lines[len(lines)-journalHistoryFileLimit:]
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600)
}

func v3AppendEvent(kind, result, message string) {
	line := time.Now().UTC().Format(time.RFC3339) + "\t" + sanitizeAutomationReason(kind) + "\t" + sanitizeAutomationReason(result) + "\t" + sanitizeAutomationReason(message) + "\n"
	appendBoundedJournalLine(settingsV3HistoryPath(), line)
}

func v3Next(last, interval string) string {
	if last == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, last)
	if err != nil {
		return ""
	}
	d := v3IntervalDuration(interval)
	if d <= 0 {
		return ""
	}
	return t.Add(d).Format(time.RFC3339)
}

func v3ScheduleFromConfig(configPath, prefix, key string, defaultEnabled bool) settingsV3Schedule {
	state := v3ParseState(settingsV3StatePath())
	enabledDefault := "no"
	if defaultEnabled {
		enabledDefault = "yes"
	}
	enabled := automationConfigValue(configPath, prefix+"_ENABLED", enabledDefault) == "yes"
	interval := v3NormalizeInterval(automationConfigValue(configPath, prefix+"_INTERVAL", v3DefaultInterval(key)), key)
	upper := strings.ToUpper(key)
	last := state[upper+"_LAST"]
	return settingsV3Schedule{
		Enabled: enabled, Interval: interval, LastRun: last, NextRun: v3Next(last, interval),
		Result: state[upper+"_RESULT"], Message: state[upper+"_MESSAGE"],
	}
}

func v3HealthTimes(enabled bool, healthInterval string) (string, string) {
	if !enabled {
		return "", ""
	}
	state := v3ParseState(settingsV3StatePath())
	last := state["HEALTH_LAST"]
	base := state["HEALTH_SCHEDULE_LAST"]
	if base == "" {
		base = last
	}
	interval := automationHealthIntervalDuration(normalizeAutomationHealthInterval(healthInterval))
	if base == "" {
		return last, time.Now().UTC().Add(interval).Format(time.RFC3339)
	}
	t, err := time.Parse(time.RFC3339, base)
	if err != nil {
		return last, ""
	}
	return last, t.Add(interval).Format(time.RFC3339)
}

func v3MergeEvents(limit int, groups ...[]automationEvent) []automationEvent {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	events := make([]automationEvent, 0, total)
	for _, group := range groups {
		events = append(events, group...)
	}
	sort.SliceStable(events, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339, events[i].At)
		right, rightErr := time.Parse(time.RFC3339, events[j].At)
		switch {
		case leftErr == nil && rightErr == nil:
			return left.After(right)
		case leftErr == nil:
			return true
		case rightErr == nil:
			return false
		default:
			return false
		}
	})
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	return events
}

func selfUpdateJournalEvents(path string) []automationEvent {
	kv := readStateFile(path)
	state := strings.ToUpper(strings.TrimSpace(kv["STATE"]))
	at := strings.TrimSpace(kv["UPDATED_AT"])
	if _, err := time.Parse(time.RFC3339, at); err != nil {
		return nil
	}
	if state != "SUCCESS" && state != "FAILED" && state != "ROLLBACK_FAILED" {
		return nil
	}

	from := strings.TrimSpace(kv["FROM_VERSION"])
	target := strings.TrimSpace(kv["TARGET_VERSION"])
	transition := strings.Trim(strings.Join([]string{from, target}, " → "), " →")
	message := strings.TrimSpace(kv["MESSAGE"])
	result := "success"
	prefix := "Обновление FreeNet завершено"
	switch state {
	case "FAILED":
		result = "failed"
		prefix = "Обновление FreeNet отменено"
	case "ROLLBACK_FAILED":
		result = "failed"
		prefix = "Обновление FreeNet остановлено: rollback не подтверждён"
	}
	if transition != "" {
		prefix += ": " + transition
	}
	if message != "" {
		prefix += ". " + message
	}
	return []automationEvent{{At: at, Kind: "freenet_update", Result: result, Message: prefix}}
}

func canonicalJournalKindKey(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "auto vpn", "auto_vpn":
		return "auto_vpn"
	case "vpn":
		return "vpn"
	case "subscription":
		return "subscription"
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

func canonicalJournalMessageKey(message string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(message)), " "))
}

func journalEventIsCanceledReleaseCatalogNoise(event automationEvent) bool {
	return strings.EqualFold(strings.TrimSpace(event.Kind), "freenet_release_catalog") &&
		strings.Contains(strings.ToLower(event.Message), "context canceled")
}

func journalResultPriority(result string) int {
	value := strings.ToLower(strings.TrimSpace(result))
	if idx := strings.LastIndex(value, ":"); idx >= 0 {
		value = strings.TrimSpace(value[idx+1:])
	}
	switch value {
	case "failed", "critical", "rollback_failed":
		return 3
	case "success", "healthy", "switched", "updated", "cleared":
		return 2
	default:
		return 1
	}
}

func dedupeCanonicalJournalEvents(events []automationEvent) []automationEvent {
	out := make([]automationEvent, 0, len(events))
	seen := map[string]int{}
	for _, event := range events {
		key := canonicalJournalKindKey(event.Kind) + "\x00" + canonicalJournalMessageKey(event.Message)
		if key == "\x00" {
			out = append(out, event)
			continue
		}
		if index, ok := seen[key]; ok {
			newerAt, newerErr := time.Parse(time.RFC3339, out[index].At)
			eventAt, eventErr := time.Parse(time.RFC3339, event.At)
			if newerErr == nil && eventErr == nil {
				delta := newerAt.Sub(eventAt)
				if delta < 0 {
					delta = -delta
				}
				if delta <= journalSemanticDedupeWindow {
					if journalResultPriority(event.Result) > journalResultPriority(out[index].Result) {
						out[index].Result = event.Result
					}
					continue
				}
			}
		}
		seen[key] = len(out)
		out = append(out, event)
	}
	return out
}

func journalEventIsRoutineAutoNoise(event automationEvent) bool {
	if canonicalJournalKindKey(event.Kind) != "auto_vpn" {
		return false
	}
	result := strings.ToLower(strings.TrimSpace(event.Result))
	// Recovery stages are deliberately verbose and must never be compacted.
	if strings.Contains(result, ":") {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(event.Message))
	switch result {
	case "same":
		return true
	case "cooldown":
		return true
	case "healthy", "success":
		return strings.Contains(message, "работают стабильно") ||
			strings.Contains(message, "подтверждён как рабочий") ||
			strings.Contains(message, "не требует смены") ||
			strings.Contains(message, "не требует поиска замены")
	default:
		return false
	}
}

func journalEventIsPendingQualityObservation(event automationEvent) bool {
	if canonicalJournalKindKey(event.Kind) != "auto_vpn" {
		return false
	}
	if strings.ToLower(strings.TrimSpace(event.Result)) != "uncertain" {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(event.Message))
	return strings.Contains(message, "vpn отвечает, но качество соединения ухудшено:") &&
		strings.Contains(message, "auto vpn накапливает подтверждение деградации")
}

func compactPendingQualityJournalEvents(events []automationEvent) []automationEvent {
	if len(events) < 2 {
		return events
	}
	keep := make([]bool, len(events))
	pendingRun := false
	lastKept := time.Time{}

	// v3MergeEvents returns newest-first. Walk oldest->newest so the first
	// observation of each pending degradation run is always retained.
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if !journalEventIsPendingQualityObservation(event) {
			keep[i] = true
			// Only another AUTO VPN state/stage ends the pending-quality run.
			// Unrelated system/subscription events must not create a fresh row.
			if canonicalJournalKindKey(event.Kind) == "auto_vpn" {
				pendingRun = false
				lastKept = time.Time{}
			}
			continue
		}
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(event.At))
		if err != nil {
			keep[i] = true
			pendingRun = false
			lastKept = time.Time{}
			continue
		}
		if !pendingRun {
			keep[i] = true
			pendingRun = true
			lastKept = at
			continue
		}
		if at.Sub(lastKept) >= journalPendingQualityHeartbeatWindow {
			keep[i] = true
			lastKept = at
		}
	}

	out := make([]automationEvent, 0, len(events))
	for i, event := range events {
		if keep[i] {
			out = append(out, event)
		}
	}
	return out
}

func compactRoutineJournalEvents(events []automationEvent) []automationEvent {
	out := make([]automationEvent, 0, len(events))
	lastByKey := map[string]time.Time{}
	for _, event := range events {
		if !journalEventIsRoutineAutoNoise(event) {
			out = append(out, event)
			continue
		}
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(event.At))
		if err != nil {
			out = append(out, event)
			continue
		}
		key := canonicalJournalKindKey(event.Kind) + "\x00" +
			strings.ToLower(strings.TrimSpace(event.Result)) + "\x00" +
			canonicalJournalMessageKey(event.Message)
		if newer, ok := lastByKey[key]; ok {
			delta := newer.Sub(at)
			if delta >= 0 && delta < journalRoutineHeartbeatWindow {
				continue
			}
		}
		lastByKey[key] = at
		out = append(out, event)
	}
	return out
}

func canonicalJournalEvents(limit int, updateStatePath ...string) []automationEvent {
	if limit <= 0 || limit > journalCanonicalRetentionLimit {
		limit = journalCanonicalRetentionLimit
	}
	automationEvents := readAutomationEvents(automationHistoryPath(), journalHistoryFileLimit)
	filteredAutomation := make([]automationEvent, 0, len(automationEvents))
	for _, event := range automationEvents {
		// Provider helper writes legacy generic switch rows into the automation
		// history. Canonical manual VPN events are recorded explicitly by the
		// HTTP handlers, so keep the legacy row out of the user-facing journal
		// to avoid duplicate/misclassified AUTO VPN entries.
		if strings.EqualFold(strings.TrimSpace(event.Kind), "VPN switch") {
			continue
		}
		filteredAutomation = append(filteredAutomation, event)
	}
	rawSettingsEvents := readAutomationEvents(settingsV3HistoryPath(), journalHistoryFileLimit)
	settingsEvents := make([]automationEvent, 0, len(rawSettingsEvents))
	for _, event := range rawSettingsEvents {
		if journalEventIsCanceledReleaseCatalogNoise(event) {
			continue
		}
		settingsEvents = append(settingsEvents, event)
	}
	groups := [][]automationEvent{filteredAutomation, settingsEvents}
	if len(updateStatePath) > 0 && strings.TrimSpace(updateStatePath[0]) != "" {
		groups = append(groups, selfUpdateJournalEvents(updateStatePath[0]))
	}
	merged := v3MergeEvents(0, groups...)
	merged = dedupeCanonicalJournalEvents(merged)
	merged = compactPendingQualityJournalEvents(merged)
	merged = compactRoutineJournalEvents(merged)
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged
}

func (a *app) settingsV3Snapshot() settingsV3Response {
	auto := a.automationSnapshot()
	scope := normalizeAutomationCountryScope(auto.Settings.CountryScope)
	if scope == "" {
		scope = automationCountryRegion
	}
	healthInterval := configuredAutomationHealthInterval(a.cfg.ConfigPath)
	lastHealth, nextHealth := v3HealthTimes(auto.Settings.Enabled, healthInterval)
	events := canonicalJournalEvents(50, a.cfg.UpdateState)
	subscription := v3ScheduleFromConfig(a.cfg.ConfigPath, "AUTO_SUBSCRIPTION_REFRESH", "subscription", true)
	if subscription.Enabled {
		subscription.NextRun = subscriptionNextCronRun(subscription.Interval, time.Now())
	}
	mode := automationModeBest
	if rawMode := strings.TrimSpace(automationConfigValue(a.cfg.ConfigPath, "AUTO_VPN_MODE", "")); rawMode != "" {
		mode = normalizeAutomationMode(rawMode)
	}
	endpointInterval := auto.Settings.Interval
	if v3IntervalDuration(endpointInterval) <= 0 {
		endpointInterval = v3DefaultInterval("auto_vpn_endpoint")
	}
	return settingsV3Response{
		Success: true,
		AutoVPN: settingsV3AutoVPN{
			Enabled: auto.Settings.Enabled, Mode: mode, HealthInterval: healthInterval, EndpointInterval: endpointInterval,
			CountryScope: scope, Countries: auto.Settings.Countries, LastHealth: lastHealth, NextHealth: nextHealth,
		},
		Automation: auto,
		Subscription: subscription,
		GeoData: v3ScheduleFromConfig(a.cfg.ConfigPath, "AUTO_GEODATA", "geodata", automationConfigValue(a.cfg.ConfigPath, "AUTO_XKEEN_GEODATA", "yes") == "yes"),
		FreeNet:    v3ScheduleFromConfig(a.cfg.ConfigPath, "AUTO_FREENET_CHECK", "freenet", false),
		Backup:     v3ScheduleFromConfig(a.cfg.ConfigPath, "AUTO_BACKUP", "backup", false),
		BackupInfo: a.v3BackupInfo(),
		Events:     events,
	}
}

func (a *app) handleSettingsV3Get(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.settingsV3Snapshot())
}

func journalEventCategory(event automationEvent) string {
	kind := strings.ToLower(strings.TrimSpace(event.Kind))
	switch kind {
	case "vpn":
		return "vpn"
	case "auto vpn", "auto_vpn":
		return "auto"
	case "subscription":
		return "subscription"
	default:
		return "system"
	}
}

func journalEventResultClass(event automationEvent) string {
	full := strings.ToLower(strings.TrimSpace(event.Result))
	if full == "selection" {
		return "neutral"
	}
	raw := full
	if index := strings.LastIndex(raw, ":"); index >= 0 {
		raw = strings.TrimSpace(raw[index+1:])
	}
	switch raw {
	case "success", "healthy", "switched", "updated", "cleared":
		return "ok"
	case "failed", "critical", "rollback_failed":
		return "bad"
	default:
		return "neutral"
	}
}

func parseJournalQuery(r *http.Request) (journalQuery, error) {
	values := r.URL.Query()
	query := journalQuery{Page: 1, PageSize: journalDefaultPageSize, Category: "all", Result: "all"}

	if raw := strings.TrimSpace(values.Get("page")); raw != "" {
		page, err := strconv.Atoi(raw)
		if err != nil || page < 1 {
			return journalQuery{}, errors.New("invalid Journal page")
		}
		query.Page = page
	}
	if raw := strings.TrimSpace(values.Get("page_size")); raw != "" {
		pageSize, err := strconv.Atoi(raw)
		if err != nil {
			return journalQuery{}, errors.New("invalid Journal page_size")
		}
		switch pageSize {
		case 50, 100, 200, journalMaxPageSize:
			query.PageSize = pageSize
		default:
			return journalQuery{}, errors.New("unsupported Journal page_size")
		}
	}
	if raw := strings.ToLower(strings.TrimSpace(values.Get("category"))); raw != "" {
		switch raw {
		case "all", "vpn", "auto", "subscription", "system":
			query.Category = raw
		default:
			return journalQuery{}, errors.New("unsupported Journal category")
		}
	}
	if raw := strings.ToLower(strings.TrimSpace(values.Get("result"))); raw != "" {
		switch raw {
		case "all", "ok", "neutral", "bad":
			query.Result = raw
		default:
			return journalQuery{}, errors.New("unsupported Journal result")
		}
	}
	query.Search = strings.TrimSpace(values.Get("q"))
	if len([]rune(query.Search)) > 200 {
		return journalQuery{}, errors.New("Journal search is too long")
	}
	if raw := strings.TrimSpace(values.Get("from")); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return journalQuery{}, errors.New("invalid Journal from timestamp")
		}
		query.From, query.HasFrom = value, true
	}
	if raw := strings.TrimSpace(values.Get("to")); raw != "" {
		value, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return journalQuery{}, errors.New("invalid Journal to timestamp")
		}
		query.To, query.HasTo = value, true
	}
	if query.HasFrom && query.HasTo && !query.To.After(query.From) {
		return journalQuery{}, errors.New("Journal to timestamp must be after from")
	}
	return query, nil
}

func filterJournalEvents(events []automationEvent, query journalQuery) []automationEvent {
	search := strings.ToLower(strings.TrimSpace(query.Search))
	filtered := make([]automationEvent, 0, len(events))
	for _, event := range events {
		if query.Category != "all" && journalEventCategory(event) != query.Category {
			continue
		}
		if query.Result != "all" && journalEventResultClass(event) != query.Result {
			continue
		}
		if query.HasFrom || query.HasTo {
			at, err := time.Parse(time.RFC3339, strings.TrimSpace(event.At))
			if err != nil {
				continue
			}
			if query.HasFrom && at.Before(query.From) {
				continue
			}
			if query.HasTo && !at.Before(query.To) {
				continue
			}
		}
		if search != "" {
			haystack := strings.ToLower(strings.Join([]string{
				event.At, event.Kind, event.Result, event.Message, journalEventCategory(event),
			}, " "))
			if !strings.Contains(haystack, search) {
				continue
			}
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func journalStatsFor(events []automationEvent) journalStats {
	stats := journalStats{
		Total: len(events),
		ByKind: map[string]int{},
		ByResult: map[string]int{},
	}
	for _, event := range events {
		switch journalEventResultClass(event) {
		case "ok":
			stats.Success++
		case "bad":
			stats.Errors++
		default:
			stats.Neutral++
		}
		kind := canonicalJournalKindKey(event.Kind)
		if kind == "" {
			kind = "system"
		}
		result := strings.ToLower(strings.TrimSpace(event.Result))
		if result == "" {
			result = "unknown"
		}
		stats.ByKind[kind]++
		stats.ByResult[result]++
	}
	return stats
}

func paginateJournalEvents(events []automationEvent, page, pageSize int) ([]automationEvent, int, int) {
	if pageSize <= 0 || pageSize > journalMaxPageSize {
		pageSize = journalDefaultPageSize
	}
	pages := (len(events) + pageSize - 1) / pageSize
	if pages < 1 {
		pages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > pages {
		page = pages
	}
	start := (page - 1) * pageSize
	if start >= len(events) {
		return []automationEvent{}, page, pages
	}
	end := start + pageSize
	if end > len(events) {
		end = len(events)
	}
	return append([]automationEvent(nil), events[start:end]...), page, pages
}

func retainedJournalBounds(events []automationEvent) (string, string) {
	if len(events) == 0 {
		return "", ""
	}
	return events[len(events)-1].At, events[0].At
}

func journalQueryRange(query journalQuery) (string, string) {
	from, to := "", ""
	if query.HasFrom {
		from = query.From.Format(time.RFC3339)
	}
	if query.HasTo {
		to = query.To.Format(time.RFC3339)
	}
	return from, to
}

// journalHealthFreshness is informational. It never triggers a network
// mutation or claims that the client-side route was checked. Busy checks
// do not advance HEALTH_LAST, so a stuck watchdog becomes visibly stale.
func journalHealthFreshness(enabled bool, interval time.Duration, state map[string]string, now time.Time) journalHealthStatus {
	status := journalHealthStatus{
		Enabled: enabled,
		LastCompleted: strings.TrimSpace(state["HEALTH_LAST"]),
		LastScheduled: strings.TrimSpace(state["HEALTH_SCHEDULE_LAST"]),
		Result: strings.ToLower(strings.TrimSpace(state["HEALTH_RESULT"])),
		Freshness: "unknown",
	}
	if interval > 0 {
		status.IntervalSeconds = int(interval / time.Second)
	}
	if !enabled {
		status.Freshness = "disabled"
		return status
	}
	if interval <= 0 || now.IsZero() {
		return status
	}
	threshold := 3 * interval
	if threshold < 2*time.Minute {
		threshold = 2 * time.Minute
	}
	checked, err := time.Parse(time.RFC3339, status.LastCompleted)
	if err != nil {
		// A started-but-never-completed health run is also actionable.
		scheduled, scheduleErr := time.Parse(time.RFC3339, status.LastScheduled)
		if scheduleErr == nil && !now.Before(scheduled) && now.Sub(scheduled) > threshold {
			status.Freshness = "stale"
		}
		return status
	}
	if checked.After(now.Add(time.Minute)) {
		return status
	}
	if now.Sub(checked) > threshold {
		status.Freshness = "stale"
	} else {
		status.Freshness = "fresh"
	}
	return status
}

func (a *app) journalWatchHealth(now time.Time) journalHealthStatus {
	settings := readAutomationSettings(a.cfg.ConfigPath)
	interval := automationHealthIntervalDuration(configuredAutomationHealthInterval(a.cfg.ConfigPath))
	return journalHealthFreshness(settings.Enabled, interval, v3ParseState(settingsV3StatePath()), now)
}

func (a *app) journalQueryResponse(r *http.Request) (journalResponse, error) {
	query, err := parseJournalQuery(r)
	if err != nil {
		return journalResponse{}, err
	}
	all := canonicalJournalEvents(journalCanonicalRetentionLimit, a.cfg.UpdateState)
	filtered := filterJournalEvents(all, query)
	pageEvents, page, pages := paginateJournalEvents(filtered, query.Page, query.PageSize)
	retainedFrom, retainedTo := retainedJournalBounds(all)
	rangeFrom, rangeTo := journalQueryRange(query)
	return journalResponse{
		Success: true,
		Events: pageEvents,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Total: len(all),
		FilteredTotal: len(filtered),
		Page: page,
		PageSize: query.PageSize,
		Pages: pages,
		RetainedFrom: retainedFrom,
		RetainedTo: retainedTo,
		RangeFrom: rangeFrom,
		RangeTo: rangeTo,
		Stats: journalStatsFor(filtered),
		Health: a.journalWatchHealth(time.Now().UTC()),
	}, nil
}

func (a *app) handleJournalGet(w http.ResponseWriter, r *http.Request) {
	response, err := a.journalQueryResponse(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, response)
}

func journalCSVCategoryLabel(event automationEvent) string {
	switch journalEventCategory(event) {
	case "vpn":
		return "VPN"
	case "auto":
		return "AUTO VPN"
	case "subscription":
		return "Подписка"
	default:
		return "Система"
	}
}

func journalCSVStageLabel(event automationEvent) string {
	category := journalEventCategory(event)
	kind := strings.ToLower(strings.TrimSpace(event.Kind))
	if category == "auto" {
		raw := strings.ToLower(strings.TrimSpace(event.Result))
		stage := raw
		if raw == "selection" {
			stage = "selection"
		} else if index := strings.Index(raw, ":"); index > 0 {
			stage = strings.TrimSpace(raw[:index])
		} else {
			stage = ""
		}
		if label, ok := map[string]string{
			"detect": "Проверка",
			"wan": "Обычный интернет",
			"confirm": "Подтверждение",
			"endpoint_refresh": "Endpoint",
			"candidate_selection": "Подбор",
			"candidate_scan": "Проверка кандидатов",
			"selection": "Решение",
			"apply": "Применение",
			"post_check": "Проверка после применения",
			"rollback": "Откат",
			"post_update_guard": "После обновления",
			"rollback_guard": "Защита отката",
			"incident": "Инцидент",
			"single_flight": "Защита параллельного восстановления",
		}[stage]; ok {
			return label
		}
		return "AUTO VPN"
	}
	if category == "vpn" {
		return "VPN"
	}
	if category == "subscription" {
		return "Подписка"
	}
	switch kind {
	case "geodata":
		return "GeoData / GeoIP"
	case "freenet":
		return "FreeNet"
	case "freenet_release_catalog":
		return "Каталог FreeNet"
	case "freenet_update", "freenet_update_recovery":
		return "Обновление FreeNet"
	case "backup":
		return "Резервная копия"
	case "xray_runtime":
		return "Xray runtime"
	}
	if value := strings.TrimSpace(event.Kind); value != "" {
		return value
	}
	return "Система"
}

func journalCSVResultLabel(event automationEvent) string {
	switch journalEventResultClass(event) {
	case "ok":
		return "Успешно"
	case "bad":
		return "Ошибка"
	default:
		return "Служебное / без изменений"
	}
}

func journalCSVDateTime(raw string) (string, string) {
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimSpace(raw), ""
	}
	at = at.UTC()
	return at.Format("02.01.2006"), at.Format("15:04:05")
}

func journalCSV(events []automationEvent) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.Write([]byte{0xEF, 0xBB, 0xBF})
	writer := csv.NewWriter(&buffer)
	writer.Comma = ';'
	writer.UseCRLF = true
	if err := writer.Write([]string{"Дата (UTC)", "Время (UTC)", "Категория", "Событие / этап", "Результат", "Описание"}); err != nil {
		return nil, err
	}
	for index := len(events) - 1; index >= 0; index-- {
		event := events[index]
		date, clock := journalCSVDateTime(event.At)
		if err := writer.Write([]string{
			date,
			clock,
			journalCSVCategoryLabel(event),
			journalCSVStageLabel(event),
			journalCSVResultLabel(event),
			event.Message,
		}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func journalExportFilename(query journalQuery) string {
	from := "retained"
	to := time.Now().UTC().Format("20060102")
	if query.HasFrom {
		from = query.From.Format("20060102")
	}
	if query.HasTo {
		to = query.To.Add(-time.Nanosecond).Format("20060102")
	}
	return "freenet-journal-" + from + "-" + to + ".csv"
}

func (a *app) handleJournalExport(w http.ResponseWriter, r *http.Request) {
	query, err := parseJournalQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "error": err.Error()})
		return
	}
	all := canonicalJournalEvents(journalCanonicalRetentionLimit, a.cfg.UpdateState)
	filtered := filterJournalEvents(all, query)
	payload, err := journalCSV(filtered)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": "Journal export failed"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+journalExportFilename(query)+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(payload)
}

func v3ShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "") + "'"
}

func buildManagedAutomationCronV3(a *app, existing []byte, values map[string]string) ([]byte, error) {
	lines := stripManagedAutomationCron(existing)
	lines = append(lines, "# BEGIN FREENET")
	bin := v3ShellQuote(automationUIBinary())
	configArg := " --config " + v3ShellQuote(a.cfg.ConfigPath)

	if values["AUTO_VPN_V1"] == "yes" {
		healthCron := automationHealthCron(values["AUTO_VPN_HEALTH_INTERVAL"])
		lines = append(lines, healthCron+" "+bin+" automation-health-watch"+configArg)
		mode := automationModeBest
		if strings.TrimSpace(values["AUTO_VPN_MODE"]) != "" {
			mode = normalizeAutomationMode(values["AUTO_VPN_MODE"])
		}
		if mode == automationModeEndpoint {
			interval := v3NormalizeInterval(values["AUTO_VPN_V1_INTERVAL"], "auto_vpn_endpoint")
			cron, ok := v3IntervalCronOffset(interval, 1)
			if !ok {
				return nil, errors.New("unsupported AUTO VPN endpoint interval")
			}
			lines = append(lines, cron+" "+bin+" settings-v3-endpoint-refresh"+configArg)
		}
	}
	appendJob := func(enabledKey, intervalKey, command string, minute int) error {
		if values[enabledKey] != "yes" {
			return nil
		}
		cron, ok := v3IntervalCronOffset(values[intervalKey], minute)
		if !ok {
			return fmt.Errorf("unsupported interval for %s", enabledKey)
		}
		lines = append(lines, cron+" "+bin+" "+command+configArg)
		return nil
	}
	if err := appendJob("AUTO_SUBSCRIPTION_REFRESH_ENABLED", "AUTO_SUBSCRIPTION_REFRESH_INTERVAL", "settings-v3-subscription", 2); err != nil {
		return nil, err
	}
	if err := appendJob("AUTO_GEODATA_ENABLED", "AUTO_GEODATA_INTERVAL", "settings-v3-geodata", 3); err != nil {
		return nil, err
	}
	if err := appendJob("AUTO_FREENET_CHECK_ENABLED", "AUTO_FREENET_CHECK_INTERVAL", "settings-v3-freenet-check", 4); err != nil {
		return nil, err
	}
	if err := appendJob("AUTO_BACKUP_ENABLED", "AUTO_BACKUP_INTERVAL", "settings-v3-backup", 6); err != nil {
		return nil, err
	}
	lines = append(lines, "# END FREENET")
	return []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"), nil
}

func settingsV3ManagedCronValuesFromConfig(configPath string) map[string]string {
	geodataDefault := automationConfigValue(configPath, "AUTO_XKEEN_GEODATA", "yes")
	return map[string]string{
		"AUTO_VPN_V1": automationConfigValue(configPath, "AUTO_VPN_V1", "no"),
		"AUTO_VPN_MODE": normalizeAutomationMode(automationConfigValue(configPath, "AUTO_VPN_MODE", automationModeBest)),
		"AUTO_VPN_HEALTH_INTERVAL": configuredAutomationHealthInterval(configPath),
		"AUTO_VPN_V1_INTERVAL": v3NormalizeInterval(
			automationConfigValue(configPath, "AUTO_VPN_V1_INTERVAL", v3DefaultInterval("auto_vpn_endpoint")),
			"auto_vpn_endpoint",
		),
		"AUTO_SUBSCRIPTION_REFRESH_ENABLED": automationConfigValue(configPath, "AUTO_SUBSCRIPTION_REFRESH_ENABLED", "yes"),
		"AUTO_SUBSCRIPTION_REFRESH_INTERVAL": v3NormalizeInterval(automationConfigValue(configPath, "AUTO_SUBSCRIPTION_REFRESH_INTERVAL", v3DefaultInterval("subscription")), "subscription"),
		"AUTO_GEODATA_ENABLED": automationConfigValue(configPath, "AUTO_GEODATA_ENABLED", geodataDefault),
		"AUTO_GEODATA_INTERVAL": v3NormalizeInterval(automationConfigValue(configPath, "AUTO_GEODATA_INTERVAL", v3DefaultInterval("geodata")), "geodata"),
		"AUTO_FREENET_CHECK_ENABLED": automationConfigValue(configPath, "AUTO_FREENET_CHECK_ENABLED", "no"),
		"AUTO_FREENET_CHECK_INTERVAL": v3NormalizeInterval(automationConfigValue(configPath, "AUTO_FREENET_CHECK_INTERVAL", v3DefaultInterval("freenet")), "freenet"),
		"AUTO_BACKUP_ENABLED": automationConfigValue(configPath, "AUTO_BACKUP_ENABLED", "no"),
		"AUTO_BACKUP_INTERVAL": v3NormalizeInterval(automationConfigValue(configPath, "AUTO_BACKUP_INTERVAL", v3DefaultInterval("backup")), "backup"),
	}
}

func (a *app) saveSettingsV3(req settingsV3SaveRequest) error {
	if req.AutoVPNEnabled == nil || req.SubscriptionEnabled == nil || req.GeoDataEnabled == nil || req.FreeNetEnabled == nil || req.BackupEnabled == nil {
		return errors.New("all automation switches are required")
	}
	currentAuto := readAutomationSettings(a.cfg.ConfigPath)
	mode := normalizeAutomationMode(automationConfigValue(a.cfg.ConfigPath, "AUTO_VPN_MODE", automationModeBest))
	if strings.TrimSpace(req.AutoVPNMode) != "" {
		mode = normalizeAutomationMode(req.AutoVPNMode)
	}
	healthInterval := normalizeAutomationHealthInterval(req.AutoVPNHealthInterval)
	if strings.TrimSpace(req.AutoVPNHealthInterval) != "" && automationHealthIntervalDuration(req.AutoVPNHealthInterval) <= 0 {
		return errors.New("unsupported AUTO VPN health interval")
	}

	endpointInterval := strings.TrimSpace(req.AutoVPNEndpointInterval)
	if v3IntervalDuration(endpointInterval) <= 0 {
		if mode == automationModeEndpoint && v3IntervalDuration(currentAuto.Interval) > 0 {
			endpointInterval = currentAuto.Interval
		} else {
			endpointInterval = v3DefaultInterval("auto_vpn_endpoint")
		}
	}
	scope := normalizeAutomationCountryScope(req.CountryScope)
	if scope != automationCountryCurrent && scope != automationCountryRegion && scope != automationCountryAllowlist {
		return errors.New("unsupported replacement geography")
	}
	countries := normalizeAutomationCountries(req.Countries)
	if mode == automationModeBest && scope == automationCountryAllowlist && len(countries) == 0 {
		return errors.New("selected countries list is empty")
	}
	autoInterval := "manual"
	autoEndpointUpdate := "no"
	autoEndpointCron := ""
	if mode == automationModeEndpoint {
		autoInterval = endpointInterval
		autoEndpointUpdate = v3Bool(*req.AutoVPNEnabled)
		if cron, ok := v3IntervalCron(endpointInterval); ok {
			autoEndpointCron = cron
		}
	}
	values := map[string]string{
		"AUTO_VPN_V1": v3Bool(*req.AutoVPNEnabled),
		"AUTO_VPN_V1_INTERVAL": autoInterval,
		"AUTO_VPN_MODE": mode,
		"AUTO_VPN_HEALTH_INTERVAL": healthInterval,
		"AUTO_VPN_POLICY": automationPolicyDegraded,
		"AUTO_VPN_COUNTRY_SCOPE": scope,
		"AUTO_VPN_COUNTRIES": strings.Join(countries, ","),
		"AUTO_VPN_AUTO_APPLY": "yes",
		"AUTO_VPN_FAILOVER": "no",
		"AUTO_ENDPOINT_UPDATE": autoEndpointUpdate,
		"AUTO_ENDPOINT_CRON": autoEndpointCron,
		"AUTO_SUBSCRIPTION_REFRESH_ENABLED": v3Bool(*req.SubscriptionEnabled),
		"AUTO_SUBSCRIPTION_REFRESH_INTERVAL": v3NormalizeInterval(req.SubscriptionInterval, "subscription"),
		"AUTO_GEODATA_ENABLED": v3Bool(*req.GeoDataEnabled),
		"AUTO_GEODATA_INTERVAL": v3NormalizeInterval(req.GeoDataInterval, "geodata"),
		"AUTO_XKEEN_GEODATA": v3Bool(*req.GeoDataEnabled),
		"AUTO_FREENET_CHECK_ENABLED": v3Bool(*req.FreeNetEnabled),
		"AUTO_FREENET_CHECK_INTERVAL": v3NormalizeInterval(req.FreeNetInterval, "freenet"),
		"AUTO_BACKUP_ENABLED": v3Bool(*req.BackupEnabled),
		"AUTO_BACKUP_INTERVAL": v3NormalizeInterval(req.BackupInterval, "backup"),
	}
	if cron, ok := v3IntervalCron(values["AUTO_GEODATA_INTERVAL"]); ok {
		values["AUTO_XKEEN_GEODATA_CRON"] = cron
	}
	beforeConfig, err := os.ReadFile(a.cfg.ConfigPath)
	if err != nil {
		return errors.New("FreeNet config is unavailable")
	}
	beforeCron := readAutomationCrontab()
	if err := writeAutomationConfigValues(a.cfg.ConfigPath, values); err != nil {
		return errors.New("cannot stage settings")
	}
	managed, err := buildManagedAutomationCronV3(a, beforeCron, values)
	if err == nil {
		err = installAutomationCrontab(managed)
	}
	if err == nil {
		return nil
	}
	rollbackConfigErr := os.WriteFile(a.cfg.ConfigPath, beforeConfig, 0600)
	rollbackCronErr := installAutomationCrontab(beforeCron)
	if rollbackConfigErr != nil || rollbackCronErr != nil {
		return errors.New("settings apply failed; rollback failed or is unknown")
	}
	return errors.New("settings apply failed; previous config and scheduler restored")
}

func (a *app) handleSettingsV3Save(w http.ResponseWriter, r *http.Request) {
	if a.mutationBlockedBySelfUpdate(w) {
		return
	}
	var req settingsV3SaveRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || strings.TrimSpace(req.Action) != "save" {
		writeJSON(w, http.StatusBadRequest, settingsV3Response{Success: false, Error: "invalid settings request"})
		return
	}
	if err := a.saveSettingsV3(req); err != nil {
		writeJSON(w, http.StatusBadGateway, settingsV3Response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, a.settingsV3Snapshot())
}

func v3Mark(kind, result, message string) {
	now := time.Now().UTC().Format(time.RFC3339)
	upper := strings.ToUpper(kind)
	_ = v3WriteState(map[string]string{upper + "_LAST": now, upper + "_RESULT": result, upper + "_MESSAGE": message})
	v3AppendEvent(kind, result, message)
}

func (a *app) runV3SubscriptionResult(ctx context.Context) (subscriptionRefreshResult, error) {
	result, err := a.refreshSubscriptionProfiles(ctx)
	visible := selectableSubscriptionProfiles(result.Profiles)
	if err != nil {
		if len(visible) > 0 && result.Stale {
			v3Mark("subscription", "failed", fmt.Sprintf("Свежий список VPN не получен. Используется последний успешный зарубежный Extra-каталог: %d.", len(visible)))
		} else {
			v3Mark("subscription", "failed", "Не удалось получить безопасный список VPN из подписки.")
		}
		return result, err
	}
	v3Mark("subscription", "success", fmt.Sprintf("Подписка проверена. Доступно зарубежных Extra-профилей: %d.", len(visible)))
	return result, nil
}

func (a *app) runV3Subscription(ctx context.Context) error {
	_, err := a.runV3SubscriptionResult(ctx)
	return err
}

func subscriptionHasFreshEndpointForCurrent(profiles []subscriptionProfile, currentEndpoint, currentFilter, exactLabel string) bool {
	currentEndpoint = strings.TrimSpace(currentEndpoint)
	currentFilter = strings.TrimSpace(currentFilter)
	if currentEndpoint == "" || currentFilter == "" {
		return false
	}
	matcher, err := regexp.Compile(currentFilter)
	if err != nil {
		return false
	}
	candidates := make([]bestServerInternalCandidate, 0, len(profiles))
	for _, profile := range profiles {
		candidates = append(candidates, bestServerInternalCandidate{Profile: profile})
	}
	_, ok := bestServerFreshCandidateForCurrent(candidates, matcher, exactLabel, currentEndpoint)
	return ok
}

var settingsV3ScheduledCurrentEndpointChanged = func(a *app, profiles []subscriptionProfile) bool {
	return subscriptionHasFreshEndpointForCurrent(
		profiles,
		readBestServerCurrentEndpoint(a.cfg.OutPath),
		readBestServerCurrentFilter(a.cfg.FilterPath),
		currentExactProfileLabel(a.cfg.FilterPath),
	)
}

var settingsV3ScheduledCurrentRefresh = func(a *app, ctx context.Context) (int, bestServerRefreshResponse) {
	return a.executeBestServerCurrentRefresh(ctx)
}

var settingsV3ScheduledEndpointRefresh = func(a *app, ctx context.Context) error {
	status, refresh := settingsV3ScheduledCurrentRefresh(a, ctx)
	message := strings.TrimSpace(refresh.Message)
	if message == "" {
		message = strings.TrimSpace(refresh.Error)
	}
	rollback := strings.TrimSpace(refresh.RollbackState)
	if rollback == "" {
		rollback = "NOT_APPLIED"
	}
	if status >= 200 && status < 300 && refresh.Success {
		if refresh.Applied {
			if message == "" {
				message = "AUTO VPN обновил endpoint текущего профиля и подтвердил VPN после применения."
			}
			writeAutomationStateV2("updated", message, rollback, false)
			appendAutomationHistoryV2("updated", message)
		}
		return nil
	}
	if rollback == "FAILED/UNKNOWN" || automationRollbackBlocksMutation(rollback) {
		if message == "" {
			message = "Плановое обновление endpoint завершилось с неподтверждённым rollback."
		}
		writeAutomationStateV2("blocked", message, rollback, false)
		appendAutomationHistoryV2("blocked", message+"; rollback="+rollback)
		return errors.New("scheduled endpoint refresh rollback failed or is unknown")
	}
	if status == 409 {
		return errAutomationBusy
	}
	if message == "" {
		message = "Плановое обновление текущего VPN не завершено."
	}
	return errors.New(message)
}

func (a *app) runV3ScheduledEndpointRefresh(ctx context.Context) error {
	settings := readAutomationSettings(a.cfg.ConfigPath)
	if !settings.Enabled || !settings.AutoApply || settings.Mode != automationModeEndpoint {
		return nil
	}
	release, lockErr := acquireAutomationHealthLock()
	if lockErr != nil {
		appendAutomationHistoryV2("busy", "Плановое обновление endpoint пропущено: другая AUTO VPN операция уже выполняется.")
		return nil
	}
	defer release()
	if automationMutationBlockedState() {
		appendAutomationHistoryV2("blocked", "Плановое обновление endpoint пропущено: активен аварийный запрет AUTO mutation после неподтверждённого rollback.")
		return nil
	}
	err := settingsV3ScheduledEndpointRefresh(a, ctx)
	if errors.Is(err, errAutomationBusy) {
		appendAutomationHistoryV2("busy", "Плановое обновление endpoint пропущено: другой безопасный updater уже выполняется.")
		return nil
	}
	return err
}

func (a *app) runV3ScheduledSubscription(ctx context.Context) error {
	subscription, err := a.runV3SubscriptionResult(ctx)
	if err != nil {
		return err
	}
	settings := readAutomationSettings(a.cfg.ConfigPath)
	if !settings.Enabled || !settings.AutoApply || !settingsV3ScheduledCurrentEndpointChanged(a, subscription.Profiles) {
		return nil
	}

	release, lockErr := acquireAutomationHealthLock()
	if lockErr != nil {
		message := "Свежий endpoint найден после обновления подписки, но AUTO VPN уже выполняет другую проверку. Текущий VPN не изменён."
		appendAutomationHistoryV2("busy", message)
		return nil
	}
	defer release()
	if automationMutationBlockedState() {
		appendAutomationHistoryV2("blocked", "Свежий endpoint обнаружен, но reconcile пропущен: активен аварийный запрет AUTO mutation после неподтверждённого rollback.")
		return nil
	}

	status, refresh := settingsV3ScheduledCurrentRefresh(a, ctx)
	message := strings.TrimSpace(refresh.Message)
	if message == "" {
		message = strings.TrimSpace(refresh.Error)
	}
	if refresh.Applied {
		if message == "" {
			message = "AUTO VPN обновил endpoint текущего профиля после обновления подписки."
		}
		writeAutomationStateV2("updated", message, refresh.RollbackState, false)
		appendAutomationHistoryV2("updated", message)
		return nil
	}
	if status >= 200 && status < 300 && refresh.Success {
		if refresh.Outcome == "no_new" {
			return nil
		}
		if message == "" {
			message = "Свежий endpoint текущего профиля не применён; рабочий VPN сохранён."
		}
		appendAutomationHistoryV2("same", message)
		return nil
	}

	rollback := strings.TrimSpace(refresh.RollbackState)
	if rollback == "" {
		rollback = "NOT_APPLIED"
	}
	if message == "" {
		message = "Проверка свежего endpoint после обновления подписки не завершена."
	}
	result := "uncertain"
	if refresh.Mutation != "NONE" || rollback == "FAILED/UNKNOWN" {
		result = "failed"
	}
	writeAutomationStateV2(result, message, rollback, false)
	appendAutomationHistoryV2(result, message+"; rollback="+rollback)
	if rollback == "FAILED/UNKNOWN" {
		return errors.New("scheduled current endpoint reconcile rollback failed or is unknown")
	}
	return nil
}

func (a *app) runV3GeoData(ctx context.Context) error {
	if _, err := os.Stat(a.cfg.XKeenPath); err != nil {
		v3Mark("geodata", "failed", "XKeen updater недоступен.")
		return errors.New("xkeen updater is unavailable")
	}
	cmd := exec.CommandContext(ctx, a.cfg.XKeenPath, "-ug")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = out
		v3Mark("geodata", "failed", "GeoData / GeoIP не обновлены.")
		return err
	}
	v3Mark("geodata", "success", "GeoData / GeoIP обновлены.")
	return nil
}

func (a *app) runV3FreeNetCheck(ctx context.Context) error {
	if _, err := os.Stat(a.cfg.SelfUpdatePath); err != nil {
		v3Mark("freenet", "failed", "Механизм проверки обновления FreeNet недоступен.")
		return err
	}
	cmd := exec.CommandContext(ctx, a.cfg.SelfUpdatePath, "plan")
	cmd.Env = append(os.Environ(),
		"FREENET_CURRENT_VERSION=v"+version,
		"FREENET_UPDATE_STATE_FILE="+a.cfg.UpdateState,
		"FREENET_UPDATE_LOCK_DIR="+a.cfg.UpdateLock,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		v3Mark("freenet", "failed", "Проверка обновления FreeNet не завершена.")
		return err
	}
	values := parseKVOutput(string(out))
	if strings.EqualFold(values["UPDATE_AVAILABLE"], "yes") {
		latest := sanitizeAutomationReason(values["LATEST_VERSION"])
		v3Mark("freenet", "available", "Доступно обновление FreeNet "+latest+". Установка требует подтверждения.")
	} else {
		v3Mark("freenet", "success", "Установлена актуальная версия FreeNet.")
	}
	return nil
}

func (a *app) createV3Backup(updateLatest bool) (string, error) {
	root := settingsV3BackupRoot()
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", errors.New("cannot prepare backup storage")
	}
	name := "backup-" + time.Now().Format("20060102-150405.000000000")
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", errors.New("cannot create backup directory")
	}
	if err := v3CaptureBackupSnapshot(dir, a.v3BackupTrackedFiles()); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	if updateLatest {
		if err := atomicWrite(filepath.Join(root, "latest"), []byte(name+"\n"), 0600); err != nil {
			_ = os.RemoveAll(dir)
			return "", errors.New("cannot commit backup restore reference")
		}
	}
	return dir, nil
}

func (a *app) createAndMarkV3Backup() (string, error) {
	dir, err := a.createV3Backup(true)
	if err != nil {
		v3Mark("backup", "failed", "Не удалось создать резервную копию настроек FreeNet.")
		return "", err
	}
	v3Mark("backup", "success", "Снимок настроек FreeNet создан: "+filepath.Base(dir)+".")
	return dir, nil
}

func (a *app) runV3Backup() error {
	_, err := a.createAndMarkV3Backup()
	return err
}

func (a *app) restoreV3Backup() error {
	_, source, err := latestV3BackupSnapshot()
	if err != nil {
		return err
	}

	rollback, err := a.createV3Backup(false)
	if err != nil {
		return errors.New("cannot create pre-restore snapshot")
	}
	files := a.v3BackupTrackedFiles()

	restoreErr := v3ApplyBackupSnapshotForRestore(source, files, false)
	if restoreErr == nil {
		restoreErr = v3VerifyBackupSnapshotForRestore(source, files, false)
	}
	if restoreErr == nil {
		return nil
	}

	if rollbackErr := v3RollbackBackupSnapshot(rollback, files); rollbackErr != nil {
		return errors.New("backup restore failed; rollback failed or is unknown")
	}
	return errors.New("backup restore failed; previous files restored and verified")
}

func (a *app) handleSettingsV3Action(w http.ResponseWriter, r *http.Request) {
	var req settingsV3ActionRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, settingsV3ActionResponse{Success: false, Error: "invalid action request"})
		return
	}
	if a.mutationBlockedBySelfUpdate(w) && req.Action != "freenet_check" {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	var err error
	var message string
	var backupInfo *settingsV3BackupInfo
	switch strings.TrimSpace(req.Action) {
	case "subscription_check":
		result, subscriptionErr := a.runV3SubscriptionResult(ctx)
		visible := selectableSubscriptionProfiles(result.Profiles)
		response := settingsV3ActionResponse{
			Success: subscriptionErr == nil, Message: "Подписка проверена и каталог Extra-профилей обновлён.",
			Profiles: visible, ProfilesAvailable: len(visible), ProfilesStale: result.Stale, ProfilesUpdatedAt: result.UpdatedAt,
		}
		if subscriptionErr != nil {
			response.Error = sanitizeAutomationReason(subscriptionErr.Error())
			writeJSON(w, http.StatusBadGateway, response)
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	case "geodata_update":
		err = a.runV3GeoData(ctx); message = "GeoData / GeoIP обновлены."
	case "freenet_check":
		err = a.runV3FreeNetCheck(ctx); message = "Проверка версии FreeNet завершена."
	case "backup_create":
		var dir string
		dir, err = a.createAndMarkV3Backup()
		if err == nil {
			info := a.v3BackupInfo()
			info.Latest = filepath.Base(dir)
			info.LatestPath = dir
			backupInfo = &info
			message = "Снимок настроек FreeNet создан."
		}
	case "backup_restore":
		err = a.restoreV3Backup()
		if err == nil {
			info := a.v3BackupInfo()
			backupInfo = &info
			message = "Последний снимок восстановлен и проверен."
		}
	default:
		writeJSON(w, http.StatusBadRequest, settingsV3ActionResponse{Success: false, Error: "unsupported action"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, settingsV3ActionResponse{Success: false, Error: sanitizeAutomationReason(err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, settingsV3ActionResponse{Success: true, Message: message, BackupInfo: backupInfo})
}

func recordSettingsV3Health(result automationHealthResult) {
	// "busy" is scheduler contention, not a health observation. It must not
	// advance HEALTH_LAST or postpone the next real liveness check.
	if result.State == "busy" {
		return
	}
	previous := v3ParseState(settingsV3StatePath())
	now := time.Now().UTC().Format(time.RFC3339)
	_ = v3WriteState(map[string]string{"HEALTH_LAST": now, "HEALTH_RESULT": result.State, "HEALTH_MESSAGE": result.Reason})

	// Keep the configured health timestamp current without flooding the Journal
	// with identical rows. Recovery transitions and reason changes are still
	// recorded immediately and remain visible much longer in the bounded log.
	if previous["HEALTH_RESULT"] == result.State && previous["HEALTH_MESSAGE"] == result.Reason {
		return
	}
	resultCode := result.State
	if result.State == automationHealthHealthy {
		resultCode = "success"
	}
	v3AppendEvent("auto_vpn", resultCode, result.Reason)
}

func runSettingsV3CLI(command string) int {
	a := &app{cfg: automationCLIConfig(), sem: make(chan struct{}, 1)}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	var err error
	switch command {
	case "settings-v3-reconcile":
		_, err = a.reconcileSettingsV3Scheduler()
	case "settings-v3-endpoint-refresh":
		err = a.runV3ScheduledEndpointRefresh(ctx)
	case "settings-v3-subscription":
		err = a.runV3ScheduledSubscription(ctx)
	case "settings-v3-geodata":
		err = a.runV3GeoData(ctx)
	case "settings-v3-freenet-check":
		err = a.runV3FreeNetCheck(ctx)
	case "settings-v3-backup":
		err = a.runV3Backup()
	default:
		return -1
	}
	if err != nil {
		fmt.Printf("RESULT=failed\nREASON=%s\n", sanitizeAutomationReason(err.Error()))
		return 1
	}
	fmt.Println("RESULT=success")
	return 0
}

func init() {
	if len(os.Args) < 2 {
		return
	}
	if rc := runSettingsV3CLI(os.Args[1]); rc >= 0 {
		os.Exit(rc)
	}
}
