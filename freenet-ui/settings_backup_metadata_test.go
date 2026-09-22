package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSettingsV3BackupActionReturnsSafeSnapshotMetadata(t *testing.T) {
	root := filepath.Join(t.TempDir(), "custom-backups")
	t.Setenv("FREENET_SETTINGS_BACKUP_ROOT", root)
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(t.TempDir(), "settings.state"))
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", filepath.Join(t.TempDir(), "settings.history"))

	dir := t.TempDir()
	configPath := filepath.Join(dir, "freenet.conf")
	subPath := filepath.Join(dir, "subscription.url")
	filterPath := filepath.Join(dir, "profile_filter.regex")
	outPath := filepath.Join(dir, "04_outbounds.json")
	files := map[string]string{
		configPath: "UI_PORT=1001\n",
		subPath: "https://secret.invalid/subscription?token=DO_NOT_LEAK\n",
		filterPath: "Extra\n",
		outPath: "{\"outbounds\":[]}\n",
	}
	for path, data := range files {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}

	a := &app{cfg: config{
		ConfigPath: configPath,
		SubPath: subPath,
		FilterPath: filterPath,
		OutPath: outPath,
		UpdateLock: filepath.Join(t.TempDir(), "no-update-lock"),
	}}

	req := httptest.NewRequest(http.MethodPost, "/api/settings-v3/action", strings.NewReader(`{"action":"backup_create"}`))
	rec := httptest.NewRecorder()
	a.handleSettingsV3Action(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var response settingsV3ActionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.BackupInfo == nil {
		t.Fatalf("backup metadata missing: %+v", response)
	}
	info := response.BackupInfo
	if info.Root != root {
		t.Fatalf("backup root=%q want=%q", info.Root, root)
	}
	if !strings.HasPrefix(info.Latest, "backup-") {
		t.Fatalf("snapshot id=%q", info.Latest)
	}
	if info.LatestPath != filepath.Join(root, info.Latest) {
		t.Fatalf("snapshot path=%q want=%q", info.LatestPath, filepath.Join(root, info.Latest))
	}
	if info.Tracked != 4 {
		t.Fatalf("tracked=%d want=4", info.Tracked)
	}
	if stat, err := os.Stat(info.LatestPath); err != nil || !stat.IsDir() {
		t.Fatalf("snapshot directory unavailable: stat=%v err=%v", stat, err)
	}
	latestRaw, err := os.ReadFile(filepath.Join(root, "latest"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(latestRaw)) != info.Latest {
		t.Fatalf("latest pointer=%q snapshot=%q", strings.TrimSpace(string(latestRaw)), info.Latest)
	}
	for _, forbidden := range []string{"DO_NOT_LEAK", "secret.invalid", "subscription?token"} {
		if strings.Contains(rec.Body.String(), forbidden) {
			t.Fatalf("backup action leaked secret material %q: %s", forbidden, rec.Body.String())
		}
	}

	snapshot := a.v3BackupInfo()
	if snapshot.Root != root || snapshot.Latest != info.Latest || snapshot.LatestPath != info.LatestPath || snapshot.Tracked != 4 {
		t.Fatalf("read-only backup metadata mismatch: %+v", snapshot)
	}
}

func TestSettingsV3RestoreActionReturnsRestoredSnapshotMetadata(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FREENET_SETTINGS_BACKUP_ROOT", root)
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(t.TempDir(), "settings.state"))
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", filepath.Join(t.TempDir(), "settings.history"))

	dir := t.TempDir()
	configPath := filepath.Join(dir, "freenet.conf")
	subPath := filepath.Join(dir, "subscription.url")
	filterPath := filepath.Join(dir, "profile_filter.regex")
	outPath := filepath.Join(dir, "04_outbounds.json")
	for path, data := range map[string]string{
		configPath: "before\n",
		subPath: "https://secret.invalid/original\n",
		filterPath: "Extra\n",
		outPath: "{\"before\":true}\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a := &app{cfg: config{
		ConfigPath: configPath,
		SubPath: subPath,
		FilterPath: filterPath,
		OutPath: outPath,
		UpdateLock: filepath.Join(t.TempDir(), "no-update-lock"),
	}}
	dirCreated, err := a.createV3Backup(true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("mutated\n"), 0600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/settings-v3/action", strings.NewReader(`{"action":"backup_restore"}`))
	rec := httptest.NewRecorder()
	a.handleSettingsV3Action(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response settingsV3ActionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.BackupInfo == nil || response.BackupInfo.LatestPath != dirCreated {
		t.Fatalf("restore metadata=%+v want path=%q", response.BackupInfo, dirCreated)
	}
	if !strings.Contains(response.Message, "восстановлен и проверен") {
		t.Fatalf("restore result must state verification: %q", response.Message)
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "before\n" {
		t.Fatalf("restored bytes=%q", got)
	}
	if strings.Contains(rec.Body.String(), "secret.invalid") {
		t.Fatalf("restore response leaked secret-bearing backup content: %s", rec.Body.String())
	}
}

func TestSettingsV3RestoreStopsBeforeMutationWhenSharedLockIsBusy(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FREENET_SETTINGS_BACKUP_ROOT", root)
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(t.TempDir(), "settings.state"))
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", filepath.Join(t.TempDir(), "settings.history"))

	dir := t.TempDir()
	configPath := filepath.Join(dir, "freenet.conf")
	subPath := filepath.Join(dir, "subscription.url")
	filterPath := filepath.Join(dir, "profile_filter.regex")
	outPath := filepath.Join(dir, "04_outbounds.json")
	for path, data := range map[string]string{
		configPath: "before\n",
		subPath: "https://secret.invalid/original\n",
		filterPath: "Extra\n",
		outPath: "{\"before\":true}\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a := &app{cfg: config{
		ConfigPath: configPath, SubPath: subPath, FilterPath: filterPath, OutPath: outPath,
		UpdateLock: filepath.Join(t.TempDir(), "no-update-lock"),
	}}
	if _, err := a.createV3Backup(true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("mutated\n"), 0600); err != nil {
		t.Fatal(err)
	}

	lock := filepath.Join(t.TempDir(), "vpn.lock")
	t.Setenv("FREENET_LOCK_DIR", lock)
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lock, "pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/settings-v3/action", strings.NewReader(`{"action":"backup_restore"}`))
	rec := httptest.NewRecorder()
	a.handleSettingsV3Action(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "mutated\n" {
		t.Fatalf("backup restore mutated files while shared lock was busy: %q", got)
	}
}

