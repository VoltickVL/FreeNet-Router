package main

import (
	"os"
	"strconv"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQualityJournalThrottlesRTTJitterWithoutHidingRecovery(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "health.state")
	logPath := filepath.Join(root, "health.log")
	t.Setenv("FREENET_SETTINGS_V3_STATE", statePath)
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", logPath)

	quality := func(ms int) automationHealthResult {
		return automationHealthResult{
			State: automationHealthUncertain,
			Reason: "VPN отвечает, но качество соединения ухудшено: отклик " +
				strconv.Itoa(ms) + " мс, сервисы 4/4. AUTO VPN накапливает подтверждение деградации.",
		}
	}
	countLogs := func() []string {
		t.Helper()
		data, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}

	recordSettingsV3Health(quality(302))
	recordSettingsV3Health(quality(340))
	// Flapping near the quality boundary updates live health but must not
	// create a new Journal row for each scheduled 30s observation.
	recordSettingsV3Health(automationHealthResult{
		State: automationHealthHealthy, Reason: "Текущий VPN и сервисные маршруты работают стабильно.",
	})
	recordSettingsV3Health(quality(310))
	rows := countLogs()
	if len(rows) != 1 {
		t.Fatalf("RTT fluctuation flooded Journal: %d rows", len(rows))
	}
	live := v3ParseState(statePath)
	if live["HEALTH_RESULT"] != automationHealthUncertain || !strings.Contains(live["HEALTH_MESSAGE"], "310 мс") ||
		live["HEALTH_LAST"] == "" || live["HEALTH_QUALITY_LOG_LAST"] == "" {
		t.Fatalf("health sample/freshness lost while Journal compacted: %+v", live)
	}

	// A long-running quality episode still has occasional forensic samples.
	if err := v3WriteState(map[string]string{
		"HEALTH_QUALITY_LOG_LAST": time.Now().UTC().Add(-healthQualityJournalWindow-time.Minute).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	recordSettingsV3Health(quality(330))
	if len(countLogs()) != 2 {
		t.Fatalf("quality heartbeat was lost or duplicated: %d", len(countLogs()))
	}

	// Confirmed VPN failure and return to service are ALWAYS logged, even
	// inside the quality-only rate limit.
	recordSettingsV3Health(automationHealthResult{State: automationHealthCritical, Reason: "VPN недоступен: два независимых HTTPS FAIL"})
	recordSettingsV3Health(automationHealthResult{State: automationHealthHealthy, Reason: "VPN восстановлен после подтверждённого отказа"})
	rows = countLogs()
	if len(rows) != 4 ||
		!strings.Contains(rows[2], "\tauto_vpn\tcritical\t") ||
		!strings.Contains(rows[3], "\tauto_vpn\tsuccess\t") {
		t.Fatalf("critical/recovery transition hidden by quality limiter: %+v", rows)
	}
}

func TestQualityJournalNeverThrottlesOtherUncertainErrors(t *testing.T) {
	now := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	prev := map[string]string{
		"HEALTH_RESULT": "uncertain",
		"HEALTH_MESSAGE": "Не удалось запустить изолированную проверку VPN.",
		"HEALTH_QUALITY_LOG_LAST": now.Format(time.RFC3339),
	}
	next := automationHealthResult{State: "uncertain", Reason: "Rollback не подтверждён; STOP."}
	emit, qualityStamp := healthQualityJournalDecision(prev, next, now.Add(time.Second))
	if !emit || qualityStamp {
		t.Fatalf("non-quality uncertain/fail-safe state was suppressed: emit=%v, stamp=%v", emit, qualityStamp)
	}
	// More importantly, a quality warning must NOT mask an unrelated probe
	// failure arriving on the next watchdog tick.
	prev["HEALTH_MESSAGE"] = "VPN отвечает, но качество соединения ухудшено: отклик 333 мс, сервисы 4/4. AUTO VPN накапливает подтверждение деградации."
	emit, qualityStamp = healthQualityJournalDecision(prev, next, now.Add(time.Second))
	if !emit || qualityStamp {
		t.Fatalf("transition from quality to independent uncertainty was hidden: emit=%v stamp=%v", emit, qualityStamp)
	}
}

func TestQualityLatencyFluctuationsAndOutageAreIndependent(t *testing.T) {
	// Synthetic replay of the 156/174/200-300ms pattern reported for WORK.
	for _, ms := range []int{156, 174, 200, 225, 250, 280, 299} {
		result := classifyAutomationReachableQuality(ms, 4, 4)
		if result.State != automationHealthHealthy || result.QualityDegraded {
			t.Fatalf("working 4/4 at %dms must not trigger optimization: %+v", ms, result)
		}
	}
	for _, ms := range []int{300, 320, 420, 650} {
		result := classifyAutomationReachableQuality(ms, 4, 4)
		if result.State != automationHealthUncertain || !result.QualityDegraded {
			t.Fatalf("elevated latency at %dms must be quality-only: %+v", ms, result)
		}
	}
	down := classifyAutomationReachableQuality(180, 0, 4)
	if down.State != automationHealthFailed || down.QualityDegraded {
		t.Fatalf("real 0/4 outage must not be treated as latency noise: %+v", down)
	}
	if got := classifyAutomationHealth(down, true, down); got.State != automationHealthCritical {
		t.Fatalf("two confirmed failures with WAN available must still start recovery: %+v", got)
	}
}
