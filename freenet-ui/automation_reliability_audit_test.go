package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAutoVPNWatchdogMediaBudgetPreservesAdmission(t *testing.T) {
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(t.TempDir(), "settings.state"))
	now := time.Date(2026, 10, 28, 15, 0, 0, 0, time.UTC)
	if automationMediaSegmentBytes != 256*1024 || automationWatchdogMediaInterval != 90*time.Second {
		t.Fatal("watchdog media cost budget changed unexpectedly")
	}
	first := automationScheduledWatchdogMediaContext(context.Background(), now)
	if !automationMediaProbeAllowed(first) {
		t.Fatal("first watchdog observation should measure media")
	}
	for _, advance := range []time.Duration{30 * time.Second, 60 * time.Second, 89 * time.Second} {
		light := automationScheduledWatchdogMediaContext(context.Background(), now.Add(advance))
		if automationMediaProbeAllowed(light) {
			t.Fatalf("unbudgeted body downloads authorized after %s", advance)
		}
	}
	due := automationScheduledWatchdogMediaContext(context.Background(), now.Add(90*time.Second))
	if !automationMediaProbeAllowed(due) {
		t.Fatal("full media verification did not resume after 90 seconds")
	}
	if !automationMediaProbeAllowed(context.Background()) {
		t.Fatal("explicit candidate admission must retain full media verification")
	}
	if raw := v3ParseState(settingsV3StatePath())["HEALTH_MEDIA_LAST"]; raw != now.Add(90*time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("watchdog media slot was not persisted: %q", raw)
	}
}

func TestAutoVPNBusyRefundsOnlyExactQualityReservation(t *testing.T) {
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(t.TempDir(), "settings.state"))
	identity := bestServerCurrentQualityIdentityHash("203.0.113.10:443", "DE Synthetic, Extra")
	stamp := time.Date(2026, 10, 28, 15, 3, 0, 0, time.UTC).Format(time.RFC3339)
	trigger := automationQualityTrigger{CurrentIdentity: identity, MediaDegraded: true, ReservedAt: stamp}
	if err := v3UpdateState(func(map[string]string) map[string]string {
		return map[string]string{"QUALITY_OPTIMIZATION_LAST": stamp}
	}); err != nil { t.Fatal(err) }
	automationReleaseQualityOptimizationReservation(trigger)
	if value := v3ParseState(settingsV3StatePath())["QUALITY_OPTIMIZATION_LAST"]; value != "" {
		t.Fatalf("BUSY consumed the 10-minute retry slot: %q", value)
	}
	newer := time.Date(2026, 10, 28, 15, 7, 0, 0, time.UTC).Format(time.RFC3339)
	if err := v3UpdateState(func(map[string]string) map[string]string {
		return map[string]string{"QUALITY_OPTIMIZATION_LAST": newer}
	}); err != nil { t.Fatal(err) }
	automationReleaseQualityOptimizationReservation(trigger)
	if value := v3ParseState(settingsV3StatePath())["QUALITY_OPTIMIZATION_LAST"]; value != newer {
		t.Fatalf("old BUSY attempt cleared another scan's cooldown: %q", value)
	}
	badTrigger := automationQualityTrigger{CurrentIdentity: "invalid", MediaDegraded: true, ReservedAt: newer}
	automationReleaseQualityOptimizationReservation(badTrigger)
	if value := v3ParseState(settingsV3StatePath())["QUALITY_OPTIMIZATION_LAST"]; value != newer {
		t.Fatalf("unverified trigger cleared cooldown: %q", value)
	}
}

func TestAutoVPNNoCandidateReportIsBoundedAndSecretFree(t *testing.T) {
	result := bestServerQualityResponse{ProfilesScanned: 16, DeepChecked: 2, Partial: true}
	result.Candidates = []bestServerQualityCandidate{
		{Tested: true, MediaSamples: automationMediaSegmentCount, ThroughputSource: bestServerThroughputAutoMedia,
			MediaMbps: 2.5, MediaGrade: "poor", DownloadIssue: "PRIVATE_SECRET_NEVER_LOG"},
		{Tested: true, MediaSamples: 1, DownloadIssue: "PRIVATE_SECRET_NEVER_LOG"},
	}
	summary := automationQualityScanSummary(result)
	for _, want := range []string{"16", "2", "1 полных", "1 медленных", "1 незавершённых", "Бюджет"} {
		if !strings.Contains(summary, want) { t.Fatalf("missing %q in %q", want, summary) }
	}
	if strings.Contains(summary, "PRIVATE_SECRET_NEVER_LOG") || len(summary) > 320 {
		t.Fatalf("report leaked private data or was unbounded: %q", summary)
	}
}

func TestAutoVPNDeadLockDoesNotRequireKillingCurrentXray(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "best.lock")
	if err := os.Mkdir(lock, 0700); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(lock, "pid"), []byte("99999999\n"), 0600); err != nil { t.Fatal(err) }
	info, err := os.Stat(lock)
	if err != nil { t.Fatal(err) }
	if _, err := os.Stat("/proc/99999999"); os.IsNotExist(err) && !automationBestLockReclaimable(lock, info) {
		t.Fatal("dead scan owner prevented a new quality comparison")
	}
}
