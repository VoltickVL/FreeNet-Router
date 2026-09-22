package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeXrayServiceExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0755); err != nil { t.Fatal(err) }
}

func prepareXrayServiceTest(t *testing.T, validatorExit int) (*app, string) {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	if err := os.MkdirAll(configDir, 0755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(configDir, "01_log.json"), []byte("{\"log\":{}}\n"), 0600); err != nil { t.Fatal(err) }
	validator := filepath.Join(root, "xray")
	writeXrayServiceExecutable(t, validator, "#!/bin/sh\nexit "+string(rune('0'+validatorExit))+"\n")
	marker := filepath.Join(root, "xkeen.calls")
	xkeen := filepath.Join(root, "xkeen")
	writeXrayServiceExecutable(t, xkeen, "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \""+marker+"\"\nexit 0\n")
	t.Setenv("FREENET_XRAY_BIN", validator)
	t.Setenv("FREENET_ROUTING_CONFIG_DIR", configDir)
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", filepath.Join(root, "history"))
	return &app{cfg: config{XKeenPath: xkeen, GeoDataDir: root}, sem: make(chan struct{}, 1)}, marker
}

func TestRestartXrayControlledValidatesAndRestartsOnce(t *testing.T) {
	a, marker := prepareXrayServiceTest(t, 0)
	previous := xrayServiceProcessRunning
	xrayServiceProcessRunning = func(name string) bool { return name == "xray" }
	defer func() { xrayServiceProcessRunning = previous }()
	if err := a.restartXrayControlled(context.Background()); err != nil { t.Fatalf("restart failed: %v", err) }
	data, err := os.ReadFile(marker)
	if err != nil { t.Fatal(err) }
	lines := strings.Fields(strings.TrimSpace(string(data)))
	if len(lines) != 1 || lines[0] != "-restart" { t.Fatalf("expected exactly one -restart call, got %q", string(data)) }
}

func TestRestartXrayControlledStopsBeforeRestartOnInvalidConfig(t *testing.T) {
	a, marker := prepareXrayServiceTest(t, 1)
	previous := xrayServiceProcessRunning
	xrayServiceProcessRunning = func(string) bool { return true }
	defer func() { xrayServiceProcessRunning = previous }()
	err := a.restartXrayControlled(context.Background())
	if err == nil || !strings.Contains(err.Error(), "перезапуск отменён") { t.Fatalf("expected validation stop, got %v", err) }
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) { t.Fatalf("xkeen restart must not run after validation failure") }
}


func TestXrayServiceRestartStopsWhenSharedMutationLockIsBusy(t *testing.T) {
	a, marker := prepareXrayServiceTest(t, 0)
	lock := filepath.Join(t.TempDir(), "vpn.lock")
	t.Setenv("FREENET_LOCK_DIR", lock)
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lock, "pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "http://router/api/xray/service", strings.NewReader(`{"action":"restart"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://router")
	rec := httptest.NewRecorder()
	a.handleXrayServicePost(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("XKeen restart ran while canonical mutation lock was busy: %v", err)
	}
}

func TestXrayServiceEventsFilterUserFacingHistory(t *testing.T) {
	root := t.TempDir()
	history := filepath.Join(root, "history")
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", history)
	body := "2026-09-17T10:00:00Z\tauto_vpn\tsuccess\tOK\n" +
		"2026-09-17T10:01:00Z\txray\tsuccess\tXray перезапущен через FreeNet.\n" +
		"2026-09-17T10:02:00Z\tconfig_studio\tsuccess\tКонфигурация сохранена.\n"
	if err := os.WriteFile(history, []byte(body), 0600); err != nil { t.Fatal(err) }
	events := xrayServiceEvents(8)
	if len(events) != 2 { t.Fatalf("expected 2 service events, got %d", len(events)) }
	if events[0].Kind != "config_studio" || events[1].Kind != "xray" { t.Fatalf("unexpected event order: %#v", events) }
}
