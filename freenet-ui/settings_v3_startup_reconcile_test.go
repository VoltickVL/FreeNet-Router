package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSettingsV3FakeCrontab(t *testing.T, statePath, countPath string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "crontab")
	script := `#!/bin/sh
set -eu
STATE="$FREENET_TEST_CRON_STATE"
COUNT="$FREENET_TEST_CRON_COUNT"
FAIL_MARKER="$FREENET_TEST_CRON_FAIL_MARKER"
if [ "${1:-}" = "-l" ]; then
  if [ -f "$STATE" ]; then cat "$STATE"; exit 0; fi
  exit 1
fi
if [ -n "$FAIL_MARKER" ] && [ ! -f "$FAIL_MARKER" ]; then
  : > "$FAIL_MARKER"
  exit 1
fi
cp "$1" "$STATE"
N=0
if [ -f "$COUNT" ]; then N="$(cat "$COUNT")"; fi
N=$((N+1))
printf '%s\n' "$N" > "$COUNT"
`
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_CRONTAB_BIN", path)
	t.Setenv("FREENET_TEST_CRON_STATE", statePath)
	t.Setenv("FREENET_TEST_CRON_COUNT", countPath)
	t.Setenv("FREENET_TEST_CRON_FAIL_MARKER", "")
	t.Setenv("FREENET_UI_BIN", "/opt/sbin/freenet-ui")
	return path
}

func TestSettingsV3StartupReconcilesVPNWatchdogIdempotently(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "freenet.conf")
	statePath := filepath.Join(dir, "crontab.state")
	countPath := filepath.Join(dir, "crontab.count")
	if err := os.WriteFile(configPath, []byte(strings.Join([]string{
		"AUTO_VPN_V1=yes",
		"AUTO_SUBSCRIPTION_REFRESH_ENABLED=no",
		"AUTO_GEODATA_ENABLED=no",
		"AUTO_XKEEN_GEODATA=no",
		"AUTO_FREENET_CHECK_ENABLED=no",
		"AUTO_BACKUP_ENABLED=no",
	}, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	const unrelated = "17 2 * * * /opt/bin/custom-job\n"
	if err := os.WriteFile(statePath, []byte(unrelated), 0600); err != nil {
		t.Fatal(err)
	}
	writeSettingsV3FakeCrontab(t, statePath, countPath)
	a := &app{cfg: config{ConfigPath: configPath}}

	changed, err := a.reconcileSettingsV3Scheduler()
	if err != nil || !changed {
		t.Fatalf("startup reconcile changed=%v err=%v", changed, err)
	}
	got, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, unrelated[:len(unrelated)-1]) {
		t.Fatalf("unrelated user cron was lost:\n%s", text)
	}
	if !strings.Contains(text, "*/5 * * * * '/opt/sbin/freenet-ui' automation-health-watch") {
		t.Fatalf("AUTO VPN watchdog was not restored:\n%s", text)
	}
	if strings.Contains(text, "automation-best-run") || strings.Contains(text, "/opt/bin/vpn failover") {
		t.Fatalf("startup reconcile restored obsolete scheduler entries:\n%s", text)
	}

	changed, err = a.reconcileSettingsV3Scheduler()
	if err != nil || changed {
		t.Fatalf("second reconcile must be idempotent changed=%v err=%v", changed, err)
	}
	count, err := os.ReadFile(countPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(count)) != "1" {
		t.Fatalf("idempotent reconcile rewrote crontab: count=%q", strings.TrimSpace(string(count)))
	}
}

func TestSettingsV3StartupCronFailureRestoresPreviousCrontab(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "freenet.conf")
	statePath := filepath.Join(dir, "crontab.state")
	countPath := filepath.Join(dir, "crontab.count")
	failMarker := filepath.Join(dir, "fail.once")
	if err := os.WriteFile(configPath, []byte("AUTO_VPN_V1=yes\nAUTO_XKEEN_GEODATA=no\nAUTO_GEODATA_ENABLED=no\n"), 0600); err != nil {
		t.Fatal(err)
	}
	before := []byte("23 1 * * * /opt/bin/user-job\n")
	if err := os.WriteFile(statePath, before, 0600); err != nil {
		t.Fatal(err)
	}
	writeSettingsV3FakeCrontab(t, statePath, countPath)
	t.Setenv("FREENET_TEST_CRON_FAIL_MARKER", failMarker)
	a := &app{cfg: config{ConfigPath: configPath}}

	changed, err := a.reconcileSettingsV3Scheduler()
	if err == nil || changed {
		t.Fatalf("failed install must report error without success changed=%v err=%v", changed, err)
	}
	if !strings.Contains(err.Error(), "previous scheduler restored") {
		t.Fatalf("rollback result not explicit: %v", err)
	}
	after, readErr := os.ReadFile(statePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != string(before) {
		t.Fatalf("failed reconcile changed user crontab: before=%q after=%q", before, after)
	}
}
