package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func prepareXrayRuntimeWatchTest(t *testing.T) (*app, string, string) {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "freenet.conf")
	if err := os.WriteFile(configPath, []byte("SETUP_COMPLETE=yes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	history := filepath.Join(root, "history")
	intent := filepath.Join(root, "xray-runtime.intent")
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", history)
	t.Setenv("FREENET_XRAY_RUNTIME_INTENT", intent)
	return &app{
		cfg: config{
			ConfigPath: configPath,
			FilterPath: filepath.Join(root, "profile.filter"),
			OutPath: filepath.Join(root, "04_outbounds.json"),
			LockPath: filepath.Join(root, "updater.lock"),
			UpdateLock: filepath.Join(root, "update.lock"),
			UpdateState: filepath.Join(root, "update.state"),
		},
		sem: make(chan struct{}, 1),
	}, history, intent
}

func installXrayRuntimeMocks(t *testing.T, online *bool, start func(*app, context.Context) error) {
	t.Helper()
	oldRunning := xrayRuntimeProcessRunning
	oldStart := xrayRuntimeStartControlled
	xrayRuntimeProcessRunning = func(*app) bool { return *online }
	if start != nil {
		xrayRuntimeStartControlled = start
	}
	t.Cleanup(func() {
		xrayRuntimeProcessRunning = oldRunning
		xrayRuntimeStartControlled = oldStart
	})
}

func TestXrayRuntimeIntentPersistsObservedRunningState(t *testing.T) {
	a, history, intent := prepareXrayRuntimeWatchTest(t)
	online := true
	installXrayRuntimeMocks(t, &online, nil)

	a.initializeXrayRuntimeIntent()
	data, err := os.ReadFile(intent)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != xrayRuntimeDesiredRunning {
		t.Fatalf("intent=%q want running", string(data))
	}

	online = false
	a.initializeXrayRuntimeIntent()
	data, err = os.ReadFile(intent)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != xrayRuntimeDesiredRunning {
		t.Fatalf("existing persistent intent was overwritten by later observation: %q", string(data))
	}

	logData, err := os.ReadFile(history)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "desired=running") || !strings.Contains(string(logData), "auto-recovery=enabled") {
		t.Fatalf("initialization journal lacks runtime intent context: %s", logData)
	}
}

func TestXrayRuntimeManualStopSuppressesRecovery(t *testing.T) {
	a, history, intent := prepareXrayRuntimeWatchTest(t)
	if err := writeXrayRuntimeDesiredState(intent, xrayRuntimeDesiredStopped); err != nil {
		t.Fatal(err)
	}
	online := false
	starts := 0
	installXrayRuntimeMocks(t, &online, func(*app, context.Context) error {
		starts++
		online = true
		return nil
	})

	state := &xrayRuntimeWatchState{}
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	a.runXrayRuntimeWatchStep(now, state)
	a.runXrayRuntimeWatchStep(now.Add(30*time.Second), state)
	if starts != 0 {
		t.Fatalf("manual desired=stopped triggered %d recovery starts", starts)
	}
	if _, err := os.Stat(history); err == nil {
		data, readErr := os.ReadFile(history)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(data), "неожиданно остановлен") {
			t.Fatalf("manual stop was misclassified as incident: %s", data)
		}
	}
}

func TestXrayRuntimeUnexpectedStopRecoversAndJournals(t *testing.T) {
	a, history, intent := prepareXrayRuntimeWatchTest(t)
	if err := writeXrayRuntimeDesiredState(intent, xrayRuntimeDesiredRunning); err != nil {
		t.Fatal(err)
	}
	online := false
	starts := 0
	installXrayRuntimeMocks(t, &online, func(*app, context.Context) error {
		starts++
		online = true
		return nil
	})

	state := &xrayRuntimeWatchState{}
	now := time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)
	a.runXrayRuntimeWatchStep(now, state)
	if starts != 0 {
		t.Fatalf("recovery started before debounce: %d", starts)
	}
	a.runXrayRuntimeWatchStep(now.Add(11*time.Second), state)
	if starts != 1 || !online {
		t.Fatalf("unexpected stop recovery starts=%d online=%v want 1/true", starts, online)
	}

	data, err := os.ReadFile(history)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"Xray неожиданно остановлен",
		"VPN сейчас не подключен",
		"controlled Start Xray",
		"Xray автоматически восстановлен",
		"endpoint mutation=NONE",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("runtime recovery journal missing %q:\n%s", want, text)
		}
	}
}

func TestXrayRuntimeFailedRecoveryRecordsPrimaryErrorAndBackoff(t *testing.T) {
	a, history, intent := prepareXrayRuntimeWatchTest(t)
	if err := writeXrayRuntimeDesiredState(intent, xrayRuntimeDesiredRunning); err != nil {
		t.Fatal(err)
	}
	online := false
	starts := 0
	installXrayRuntimeMocks(t, &online, func(*app, context.Context) error {
		starts++
		return errors.New("test controlled start failed")
	})

	state := &xrayRuntimeWatchState{}
	now := time.Date(2026, 10, 7, 2, 0, 0, 0, time.UTC)
	a.runXrayRuntimeWatchStep(now, state)
	a.runXrayRuntimeWatchStep(now.Add(11*time.Second), state)
	a.runXrayRuntimeWatchStep(now.Add(30*time.Second), state)
	if starts != 1 {
		t.Fatalf("recovery retry ignored backoff: starts=%d want=1", starts)
	}

	data, err := os.ReadFile(history)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"PRIMARY ERROR: test controlled start failed", "следующая попытка через 60 с", "rollback=NOT_APPLICABLE"} {
		if !strings.Contains(text, want) {
			t.Fatalf("failed recovery journal missing %q:\n%s", want, text)
		}
	}
}

func TestXrayRuntimeHealthGateNeverUsesIsolatedProbeWhenProductionIsOffline(t *testing.T) {
	a, _, intent := prepareXrayRuntimeWatchTest(t)
	online := false
	installXrayRuntimeMocks(t, &online, nil)

	if err := writeXrayRuntimeDesiredState(intent, xrayRuntimeDesiredRunning); err != nil {
		t.Fatal(err)
	}
	probe, blocked := a.xrayRuntimeHealthGate()
	if !blocked || probe.State != automationHealthUncertain {
		t.Fatalf("running-intent offline gate=%v probe=%+v", blocked, probe)
	}
	if !strings.Contains(probe.Reason, "VPN сейчас не подключен") || !strings.Contains(probe.Reason, "watchdog") {
		t.Fatalf("unexpected running-intent health reason: %s", probe.Reason)
	}

	if err := writeXrayRuntimeDesiredState(intent, xrayRuntimeDesiredStopped); err != nil {
		t.Fatal(err)
	}
	probe, blocked = a.xrayRuntimeHealthGate()
	if !blocked || !strings.Contains(probe.Reason, "остановлен вручную") || !strings.Contains(probe.Reason, "AUTO VPN mutation отключены") {
		t.Fatalf("manual-stop health gate=%v probe=%+v", blocked, probe)
	}

	online = true
	if _, blocked = a.xrayRuntimeHealthGate(); blocked {
		t.Fatal("online production Xray must allow normal isolated quality probe")
	}
}

func TestManualXrayStopPersistsStoppedIntentAndDisablesAutoRecovery(t *testing.T) {
	a, marker := prepareXrayServiceTest(t, 0)
	oldRunning := xrayServiceProcessRunning
	xrayServiceProcessRunning = func(name string) bool {
		if name != "xray" {
			return false
		}
		_, err := os.Stat(marker)
		return os.IsNotExist(err)
	}
	t.Cleanup(func() { xrayServiceProcessRunning = oldRunning })

	req := httptest.NewRequest(http.MethodPost, "http://router/api/xray/service", strings.NewReader(`{"action":"stop"}`))
	req.Host = "router"
	req.Header.Set("Origin", "http://router")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.handleXrayServicePost(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stop status=%d body=%s", rec.Code, rec.Body.String())
	}
	desired, err := readXrayRuntimeDesiredState(xrayRuntimeIntentPath(a.cfg.ConfigPath))
	if err != nil {
		t.Fatal(err)
	}
	if desired != xrayRuntimeDesiredStopped {
		t.Fatalf("desired=%q want stopped", desired)
	}
	history, err := os.ReadFile(settingsV3HistoryPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(history), "автоподъём отключён до Start/Restart") {
		t.Fatalf("manual stop journal lacks explicit recovery suppression: %s", history)
	}
}

func TestManualXrayStartPersistsRunningIntentAndEnablesAutoRecovery(t *testing.T) {
	a, marker := prepareXrayServiceTest(t, 0)
	oldRunning := xrayServiceProcessRunning
	xrayServiceProcessRunning = func(name string) bool {
		if name != "xray" {
			return false
		}
		_, err := os.Stat(marker)
		return err == nil
	}
	t.Cleanup(func() { xrayServiceProcessRunning = oldRunning })

	req := httptest.NewRequest(http.MethodPost, "http://router/api/xray/service", strings.NewReader(`{"action":"start"}`))
	req.Host = "router"
	req.Header.Set("Origin", "http://router")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	a.handleXrayServicePost(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start status=%d body=%s", rec.Code, rec.Body.String())
	}
	desired, err := readXrayRuntimeDesiredState(xrayRuntimeIntentPath(a.cfg.ConfigPath))
	if err != nil {
		t.Fatal(err)
	}
	if desired != xrayRuntimeDesiredRunning {
		t.Fatalf("desired=%q want running", desired)
	}
	history, err := os.ReadFile(settingsV3HistoryPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(history), "аварийный автоподъём включён") {
		t.Fatalf("manual start journal lacks explicit recovery enablement: %s", history)
	}
}

func TestMainStartsXrayRuntimeWatchdog(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "a.initializeXrayRuntimeIntent()") || !strings.Contains(text, "a.startXrayRuntimeWatchdog()") {
		t.Fatal("FreeNet service must initialize persistent Xray intent and start runtime watchdog")
	}
}
