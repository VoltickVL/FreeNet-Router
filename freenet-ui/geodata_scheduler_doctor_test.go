package main

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

func TestGeoDataNextCronFireUsesFutureActualCron(t *testing.T) {
    cases := []struct {
        name, interval, now, want string
    }{
        {"12h", "12h", "2026-10-20T00:05:00Z", "2026-10-20T12:03:00Z"},
        {"24h", "24h", "2026-10-20T03:15:00Z", "2026-10-20T04:03:00Z"},
        {"30m", "30m", "2026-10-20T03:04:00Z", "2026-10-20T03:33:00Z"},
        {"past-day", "24h", "2026-10-20T04:05:00Z", "2026-10-21T04:03:00Z"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            now, err := time.Parse(time.RFC3339, tc.now)
            if err != nil { t.Fatal(err) }
            if got := nextGeoDataCronFire(tc.interval, now); got != tc.want {
                t.Fatalf("next cron fire got %q, want %q", got, tc.want)
            }
        })
    }
    if got := nextGeoDataCronFire("invalid", time.Now()); got != "" {
        t.Fatalf("invalid interval reported execution time %q", got)
    }
}

func TestGeoDataCronDaemonObservation(t *testing.T) {
    root := t.TempDir()
    if got := observedCronDaemon(root); got != "not_observed" {
        t.Fatalf("empty proc result=%q", got)
    }
    pidDir := filepath.Join(root, "1234")
    if err := os.Mkdir(pidDir, 0700); err != nil { t.Fatal(err) }
    if err := os.WriteFile(filepath.Join(pidDir, "comm"), []byte("crond\n"), 0600); err != nil { t.Fatal(err) }
    if got := observedCronDaemon(root); got != "observed" {
        t.Fatalf("crond result=%q", got)
    }
    if got := observedCronDaemon(filepath.Join(root, "does-not-exist")); got != "unknown" {
        t.Fatalf("unreadable proc result=%q", got)
    }
}

func TestGeoDataSchedulerDoctorReadOnlyEvidenceAndStaleReceipt(t *testing.T) {
    root := t.TempDir()
    configPath := filepath.Join(root, "freenet.conf")
    beforeConfig := "AUTO_GEODATA_ENABLED=yes\nAUTO_GEODATA_INTERVAL=12h\nAUTO_XKEEN_GEODATA=yes\n"
    if err := os.WriteFile(configPath, []byte(beforeConfig), 0600); err != nil { t.Fatal(err) }
    geoState := filepath.Join(root, "settings-v3.state")
    stateBody := "GEODATA_LAST=2026-10-13T00:24:40Z\nGEODATA_RESULT=success\n"
    if err := os.WriteFile(geoState, []byte(stateBody), 0600); err != nil { t.Fatal(err) }
    t.Setenv("FREENET_SETTINGS_V3_STATE", geoState)
    cronState := filepath.Join(root, "crontab.state")
    cronCount := filepath.Join(root, "crontab.count")
    writeSettingsV3FakeCrontab(t, cronState, cronCount)
    installed := "0 12 * * * /opt/sbin/unrelated\n# BEGIN FREENET\n3 */12 * * * '/opt/sbin/freenet-ui' settings-v3-geodata --config '" + configPath + "'\n# END FREENET\n"
    if err := os.WriteFile(cronState, []byte(installed), 0600); err != nil { t.Fatal(err) }

    procRoot := filepath.Join(root, "proc")
    if err := os.MkdirAll(filepath.Join(procRoot, "800"), 0700); err != nil { t.Fatal(err) }
    if err := os.WriteFile(filepath.Join(procRoot, "800", "comm"), []byte("crond\n"), 0600); err != nil { t.Fatal(err) }
    previous := geoDataCronProcRoot
    geoDataCronProcRoot = procRoot
    t.Cleanup(func(){ geoDataCronProcRoot = previous })

    now := time.Date(2026,10,20,0,5,0,0,time.UTC)
    a := &app{cfg: config{ConfigPath: configPath}}
    evidence := a.geoDataSchedulerDoctor(now)
    if !evidence.Success || evidence.Mutation != "NONE" ||
        !evidence.ScheduleVerified || !evidence.Installed || evidence.Daemon != "observed" ||
        !evidence.LastResultStale || evidence.LastResult != "success" ||
        evidence.NextPlannedAt != "2026-10-20T12:03:00Z" {
        t.Fatalf("unexpected scheduler doctor result: %+v", evidence)
    }
    if evidence.Diagnosis != "scheduler_observed_execution_not_proven" {
        t.Fatalf("doctor claimed execution: %q", evidence.Diagnosis)
    }
    afterConfig, err := os.ReadFile(configPath)
    if err != nil { t.Fatal(err) }
    afterCron, err := os.ReadFile(cronState)
    if err != nil { t.Fatal(err) }
    if string(afterConfig) != beforeConfig || string(afterCron) != installed {
        t.Fatal("read-only doctor mutated settings or managed cron")
    }
    if _, err := os.Stat(cronCount); !os.IsNotExist(err) {
        t.Fatal("doctor installed crontab instead of reading")
    }
    // A malformed, stale or duplicate job must not be certified as installed.
    mismatch := strings.Replace(installed, "3 */12", "4 */12", 1)
    if err := os.WriteFile(cronState, []byte(mismatch), 0600); err != nil { t.Fatal(err) }
    missing := a.geoDataSchedulerDoctor(now)
    if missing.ScheduleVerified || missing.Installed ||
        missing.Diagnosis != "managed_geodata_job_missing_or_mismatched" {
        t.Fatalf("incorrectly accepted mismatched job: %+v", missing)
    }
    duplicated := strings.Replace(installed, "# END FREENET", "3 */12 * * * '/opt/sbin/freenet-ui' settings-v3-geodata --config '"+configPath+"'\n# END FREENET",1)
    if err := os.WriteFile(cronState, []byte(duplicated), 0600); err != nil { t.Fatal(err) }
    duplicate := a.geoDataSchedulerDoctor(now)
    if duplicate.ScheduleVerified || duplicate.Installed {
        t.Fatalf("incorrectly accepted duplicate job: %+v", duplicate)
    }
}
