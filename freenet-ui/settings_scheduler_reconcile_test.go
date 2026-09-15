package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFakeCrontab(t *testing.T, dir string) (string, string) {
	t.Helper()
	state := filepath.Join(dir, "crontab.txt")
	bin := filepath.Join(dir, "crontab")
	script := `#!/bin/sh
state="$FREENET_TEST_CRONTAB_STATE"
if [ "$1" = "-l" ]; then
  if [ ! -f "$state" ]; then
    echo "no crontab for root" >&2
    exit 1
  fi
  cat "$state"
  exit 0
fi
cp "$1" "$state"
`
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	return bin, state
}

func TestReconcileSettingsV3SchedulerRepairsStaleAutoVPNWatchdog(t *testing.T) {
	dir := t.TempDir()
	bin, state := writeFakeCrontab(t, dir)
	t.Setenv("FREENET_CRONTAB_BIN", bin)
	t.Setenv("FREENET_TEST_CRONTAB_STATE", state)
	t.Setenv("FREENET_UI_BIN", "/opt/sbin/freenet-ui")

	configPath := filepath.Join(dir, "freenet.conf")
	configBody := strings.Join([]string{
		"AUTO_VPN_V1=yes",
		"AUTO_SUBSCRIPTION_REFRESH_ENABLED=no",
		"AUTO_GEODATA_ENABLED=no",
		"AUTO_FREENET_CHECK_ENABLED=no",
		"AUTO_BACKUP_ENABLED=no",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(configBody), 0600); err != nil {
		t.Fatal(err)
	}
	legacy := "17 2 * * * /opt/bin/custom-job\n# BEGIN FREENET\n*/5 * * * * /opt/bin/vpn failover\n# END FREENET\n"
	if err := os.WriteFile(state, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}

	a := &app{cfg: config{ConfigPath: configPath}}
	changed, err := a.reconcileSettingsV3Scheduler()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected stale scheduler to be repaired")
	}
	got, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "17 2 * * * /opt/bin/custom-job") {
		t.Fatalf("external cron entry was not preserved:\n%s", text)
	}
	if !strings.Contains(text, "*/5 * * * * '/opt/sbin/freenet-ui' automation-health-watch") {
		t.Fatalf("canonical AUTO VPN watchdog missing:\n%s", text)
	}
	if strings.Contains(text, "/opt/bin/vpn failover") {
		t.Fatalf("legacy failover survived reconciliation:\n%s", text)
	}

	changed, err = a.reconcileSettingsV3Scheduler()
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("reconciliation must be idempotent")
	}
}

func TestReconcileSettingsV3SchedulerKeepsWatchdogDisabled(t *testing.T) {
	dir := t.TempDir()
	bin, state := writeFakeCrontab(t, dir)
	t.Setenv("FREENET_CRONTAB_BIN", bin)
	t.Setenv("FREENET_TEST_CRONTAB_STATE", state)

	configPath := filepath.Join(dir, "freenet.conf")
	if err := os.WriteFile(configPath, []byte("AUTO_VPN_V1=no\nAUTO_GEODATA_ENABLED=no\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, []byte("# BEGIN FREENET\n*/5 * * * * /opt/sbin/freenet-ui automation-health-watch\n# END FREENET\n"), 0600); err != nil {
		t.Fatal(err)
	}

	a := &app{cfg: config{ConfigPath: configPath}}
	changed, err := a.reconcileSettingsV3Scheduler()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("expected stale enabled watchdog to be removed")
	}
	got, _ := os.ReadFile(state)
	if strings.Contains(string(got), "automation-health-watch") {
		t.Fatalf("watchdog must remain disabled:\n%s", got)
	}
}

func TestReconcileSettingsV3SchedulerStopsOnUnknownCrontabRead(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "crontab")
	script := "#!/bin/sh\necho 'permission denied' >&2\nexit 2\n"
	if err := os.WriteFile(bin, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_CRONTAB_BIN", bin)
	configPath := filepath.Join(dir, "freenet.conf")
	if err := os.WriteFile(configPath, []byte("AUTO_VPN_V1=yes\n"), 0600); err != nil {
		t.Fatal(err)
	}

	a := &app{cfg: config{ConfigPath: configPath}}
	changed, err := a.reconcileSettingsV3Scheduler()
	if err == nil {
		t.Fatal("unknown crontab read error must stop reconciliation")
	}
	if changed {
		t.Fatal("scheduler must not report mutation after unsafe read")
	}
}
