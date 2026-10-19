package main

import (
    "context"
    "net/http"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "time"
)

type geoDataSchedulerDoctorResponse struct {
    Success            bool   `json:"success"`
    Mutation           string `json:"mutation"`
    Enabled            bool   `json:"enabled"`
    Interval           string `json:"interval"`
    ExpectedCron       string `json:"expected_cron,omitempty"`
    Installed          bool   `json:"installed"`
    Daemon             string `json:"daemon"`
    ScheduleVerified   bool   `json:"schedule_verified"`
    NextPlannedAt      string `json:"next_planned_at,omitempty"`
    LastRun            string `json:"last_run,omitempty"`
    LastResult         string `json:"last_result,omitempty"`
    LastResultStale    bool   `json:"last_result_stale"`
    Diagnosis          string `json:"diagnosis"`
    Error              string `json:"error,omitempty"`
}

// A cron expression is a plan, not evidence that crond actually executed it.
// Match the canonical minute offset from buildManagedAutomationCronV3.
func nextGeoDataCronFire(interval string, now time.Time) string {
    if _, ok := v3IntervalCronOffset(interval, 3); !ok {
        return ""
    }
    candidate := now.Truncate(time.Minute).Add(time.Minute)
    for i := 0; i < 48*60; i++ {
        hour, minute := candidate.Hour(), candidate.Minute()
        match := false
        switch interval {
        case "30m":
            match = minute == 3 || minute == 33
        case "1h":
            match = minute == 3
        case "3h":
            match = minute == 3 && hour%3 == 0
        case "6h":
            match = minute == 3 && hour%6 == 0
        case "12h":
            match = minute == 3 && hour%12 == 0
        case "24h":
            match = minute == 3 && hour == 4
        }
        if match {
            return candidate.UTC().Format(time.RFC3339)
        }
        candidate = candidate.Add(time.Minute)
    }
    return ""
}

// Only the existence of a cron daemon is observed. No /proc cmdline or
// privileged crontab contents are ever returned to the browser.
var geoDataCronProcRoot = "/proc"

func observedCronDaemon(procRoot string) string {
    entries, err := os.ReadDir(procRoot)
    if err != nil {
        return "unknown"
    }
    unreadable := false
    for _, entry := range entries {
        if !entry.IsDir() {
            continue
        }
        if _, err := strconv.Atoi(entry.Name()); err != nil {
            continue
        }
        name, err := os.ReadFile(filepath.Join(procRoot, entry.Name(), "comm"))
        if err != nil {
            if !os.IsNotExist(err) {
                unreadable = true
            }
            continue
        }
        switch strings.TrimSpace(string(name)) {
        case "cron", "crond":
            return "observed"
        }
    }
    if unreadable {
        return "unknown"
    }
    return "not_observed"
}

func (a *app) geoDataSchedulerDoctor(now time.Time) geoDataSchedulerDoctorResponse {
    setting := v3ScheduleFromConfig(a.cfg.ConfigPath, "AUTO_GEODATA", "geodata",
        automationConfigValue(a.cfg.ConfigPath, "AUTO_XKEEN_GEODATA", "yes") == "yes")
    out := geoDataSchedulerDoctorResponse{
        Mutation: "NONE", Enabled: setting.Enabled, Interval: setting.Interval,
        LastRun: setting.LastRun, LastResult: setting.Result,
        Daemon: observedCronDaemon(geoDataCronProcRoot),
    }
    if setting.LastRun != "" {
        t, err := time.Parse(time.RFC3339, setting.LastRun)
        if err != nil || now.Sub(t) > v3IntervalDuration(setting.Interval)+time.Hour {
            out.LastResultStale = true
        }
    }
    schedule, ok := v3IntervalCronOffset(setting.Interval, 3)
    if !ok {
        out.Diagnosis = "unsupported_geodata_interval"
        return out
    }
    out.ExpectedCron = schedule
    if setting.Enabled {
        out.NextPlannedAt = nextGeoDataCronFire(setting.Interval, now)
    }
    ctx, cancel := context.WithTimeout(context.Background(), settingsSchedulerReconcileTimeout)
    installed, err := readAutomationCrontabStrict(ctx)
    cancel()
    if err != nil {
        out.Diagnosis = "crontab_unreadable"
        out.Error = "Невозможно подтвердить установленное расписание FreeNet."
        return out
    }
    expectedLine := schedule + " " + v3ShellQuote(automationUIBinary()) +
        " settings-v3-geodata --config " + v3ShellQuote(a.cfg.ConfigPath)
    inBlock, exact, anyGeo := false, 0, 0
    for _, raw := range strings.Split(string(installed), "\n") {
        line := strings.TrimSpace(raw)
        switch line {
        case "# BEGIN FREENET":
            inBlock = true
            continue
        case "# END FREENET":
            inBlock = false
            continue
        }
        if !inBlock || !strings.Contains(line, "settings-v3-geodata") {
            continue
        }
        anyGeo++
        if line == expectedLine {
            exact++
        }
    }
    out.Installed = exact == 1 && anyGeo == 1
    out.Success = true // The inspection succeeded; never means GeoData was updated.
    switch {
    case !setting.Enabled && anyGeo == 0:
        out.Diagnosis = "disabled"
    case !setting.Enabled:
        out.Diagnosis = "disabled_job_still_present"
    case !out.Installed:
        out.Diagnosis = "managed_geodata_job_missing_or_mismatched"
    case out.Daemon == "not_observed":
        out.Diagnosis = "cron_daemon_not_observed"
    case out.Daemon == "unknown":
        out.Diagnosis = "cron_daemon_unknown"
    default:
        out.ScheduleVerified = true
        out.Diagnosis = "scheduler_observed_execution_not_proven"
    }
    return out
}

func (a *app) handleGeoDataSchedulerDoctor(w http.ResponseWriter, _ *http.Request) {
    w.Header().Set("Cache-Control", "no-store")
    writeJSON(w, http.StatusOK, a.geoDataSchedulerDoctor(time.Now()))
}
