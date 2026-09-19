package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	rebootAcceptanceMarkerVersion = 1
	rebootAcceptanceMarkerMaxSize = 256 << 10
	rebootAcceptanceDelay         = 1500 * time.Millisecond
)

var (
	rebootReadBootID = func() (string, error) {
		b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
		if err != nil {
			return "", err
		}
		id := strings.TrimSpace(string(b))
		if id == "" || len(id) > 128 {
			return "", errors.New("invalid boot id")
		}
		for _, r := range id {
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') && r != '-' {
				return "", errors.New("invalid boot id")
			}
		}
		return id, nil
	}
	rebootProcessRunning = processRunning
	rebootCommandRunner = func() error {
		cmd := exec.Command(rebootSystemBinary())
		cmd.Stdin = nil
		cmd.Stdout = nil
		cmd.Stderr = nil
		return cmd.Run()
	}
	rebootCommandDelay = rebootAcceptanceDelay
)

type rebootAcceptanceCheck struct {
	Key    string `json:"key"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type rebootAcceptancePlanResponse struct {
	Success         bool                    `json:"success"`
	Ready           bool                    `json:"ready"`
	Checks          []rebootAcceptanceCheck `json:"checks"`
	ExpectedDelta   string                  `json:"expected_delta,omitempty"`
	ExpectedNoDelta string                  `json:"expected_no_delta,omitempty"`
	Mutation        string                  `json:"mutation"`
}

type rebootAcceptanceApplyRequest struct {
	Confirm string `json:"confirm"`
}

type rebootAcceptanceApplyResponse struct {
	Success   bool   `json:"success"`
	State     string `json:"state"`
	Message   string `json:"message,omitempty"`
	Mutation  string `json:"mutation"`
	Error     string `json:"error,omitempty"`
}

type rebootAcceptanceStateResponse struct {
	Success    bool                    `json:"success"`
	State      string                  `json:"state"`
	Complete   bool                    `json:"complete"`
	Passed     bool                    `json:"passed"`
	PreparedAt string                  `json:"prepared_at,omitempty"`
	Checks     []rebootAcceptanceCheck `json:"checks,omitempty"`
	Message    string                  `json:"message,omitempty"`
	Mutation   string                  `json:"mutation"`
}

type rebootAcceptanceMarker struct {
	Version         int               `json:"version"`
	State           string            `json:"state"`
	PreparedAt      string            `json:"prepared_at"`
	BootID          string            `json:"boot_id"`
	FreeNetVersion  string            `json:"freenet_version"`
	InstallScenario string            `json:"install_scenario"`
	SetupComplete   bool              `json:"setup_complete"`
	AuthConfigured  bool              `json:"auth_configured"`
	ConfigSHA256    string            `json:"config_sha256"`
	XraySHA256      map[string]string `json:"xray_sha256"`
}

func registerRebootAcceptanceAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/system/reboot/plan", a.requireAuth(a.handleRebootAcceptancePlan))
	mux.HandleFunc("POST /api/system/reboot/apply", a.requireAuth(a.handleRebootAcceptanceApply))
	mux.HandleFunc("GET /api/system/reboot/state", a.requireAuth(a.handleRebootAcceptanceState))
}

func rebootSystemBinary() string {
	if path := strings.TrimSpace(os.Getenv("FREENET_REBOOT_BIN")); path != "" {
		return path
	}
	return "/sbin/reboot"
}

func rebootFreeNetInitPath() string {
	if path := strings.TrimSpace(os.Getenv("FREENET_UI_INIT_PATH")); path != "" {
		return path
	}
	return "/opt/etc/init.d/S99freenet-ui"
}

func rebootXKeenInitPaths() []string {
	if path := strings.TrimSpace(os.Getenv("FREENET_XKEEN_INIT_PATH")); path != "" {
		return []string{path}
	}
	return []string{"/opt/etc/init.d/S99xkeen", "/opt/etc/init.d/S05xkeen"}
}

func (a *app) rebootAcceptancePath() string {
	return filepath.Join(filepath.Dir(a.cfg.ConfigPath), "reboot-acceptance.json")
}

func regularExecutable(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm()&0111 != 0
}

func rebootFreeNetInitReady() bool {
	path := rebootFreeNetInitPath()
	if !regularExecutable(path) {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) > 128<<10 {
		return false
	}
	text := string(b)
	return strings.Contains(text, "ENABLED=yes") &&
		strings.Contains(text, "PROCS=freenet-ui") &&
		strings.Contains(text, "rc.func")
}

func rebootXKeenAutostartOn() bool {
	for _, path := range rebootXKeenInitPaths() {
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		b, err := os.ReadFile(path)
		if err != nil || len(b) > 256<<10 {
			return false
		}
		for _, line := range strings.Split(strings.ReplaceAll(string(b), "\r", ""), "\n") {
			line = strings.TrimSpace(line)
			line = strings.Trim(line, "\"'")
			if strings.HasPrefix(line, "start_auto=") {
				value := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "start_auto=")), "\"'")
				return value == "on"
			}
		}
		return false
	}
	return false
}

func hashRegularFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func xrayConfigHashes(dir string) (map[string]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for _, entry := range entries {
		if strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, errors.New("no Xray JSON configs")
	}
	hashes := make(map[string]string, len(names))
	for _, name := range names {
		if filepath.Base(name) != name {
			return nil, errors.New("invalid Xray config name")
		}
		hash, err := hashRegularFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		hashes[name] = hash
	}
	return hashes, nil
}

func equalHashMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

func (a *app) rebootOperationIdle() bool {
	if a == nil {
		return false
	}
	if len(a.sem) != 0 {
		return false
	}
	a.updateMu.Lock()
	launching := a.updateLaunching
	a.updateMu.Unlock()
	if launching {
		return false
	}
	for _, path := range []string{a.cfg.UpdateLock, a.cfg.LockPath} {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		_, err := os.Stat(path)
		if err == nil {
			return false
		}
		if !os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func (a *app) rebootSnapshot() (rebootAcceptanceMarker, error) {
	bootID, err := rebootReadBootID()
	if err != nil {
		return rebootAcceptanceMarker{}, errors.New("cannot read boot identity")
	}
	configHash, err := hashRegularFile(a.cfg.ConfigPath)
	if err != nil {
		return rebootAcceptanceMarker{}, errors.New("cannot hash FreeNet config")
	}
	xrayHashes, err := xrayConfigHashes(filepath.Dir(a.cfg.OutPath))
	if err != nil {
		return rebootAcceptanceMarker{}, errors.New("cannot hash Xray configs")
	}
	installScenario, setupComplete := readSetupState(a.cfg.ConfigPath)
	return rebootAcceptanceMarker{
		Version:         rebootAcceptanceMarkerVersion,
		State:           "PREPARED",
		PreparedAt:      time.Now().UTC().Format(time.RFC3339),
		BootID:          bootID,
		FreeNetVersion:  "v" + version,
		InstallScenario: installScenario,
		SetupComplete:   setupComplete,
		AuthConfigured:  a.credentialConfigured(),
		ConfigSHA256:    configHash,
		XraySHA256:      xrayHashes,
	}, nil
}

func (a *app) rebootPlan() rebootAcceptancePlanResponse {
	checks := make([]rebootAcceptanceCheck, 0, 9)
	add := func(key string, ok bool, good, bad string) {
		detail := bad
		if ok {
			detail = good
		}
		checks = append(checks, rebootAcceptanceCheck{Key: key, OK: ok, Detail: detail})
	}

	installScenario, setupComplete := readSetupState(a.cfg.ConfigPath)
	add("setup", setupComplete && installScenario != "unknown", "Browser Setup завершён", "Browser Setup не завершён или install scenario неизвестен")
	add("auth", a.credentialConfigured(), "Учётные данные администратора сохранены", "Учётные данные администратора не подтверждены")
	add("xray", rebootProcessRunning("xray"), "Xray сейчас работает", "Xray сейчас не работает")
	add("freenet_init", rebootFreeNetInitReady(), "FreeNet autostart init готов", "FreeNet autostart init не подтверждён")
	add("xkeen_autostart", rebootXKeenAutostartOn(), "XKeen autostart включён", "XKeen autostart не подтверждён")
	add("operations", a.rebootOperationIdle(), "Нет активных mutation/update операций", "Есть активная или неизвестная mutation/update операция")

	schedulerOK, schedulerErr := a.settingsV3SchedulerCurrent()
	add("scheduler", schedulerErr == nil && schedulerOK, "Планировщик соответствует Settings", "Планировщик не подтверждён как canonical")

	_, bootErr := rebootReadBootID()
	add("boot_id", bootErr == nil, "Boot identity доступна", "Boot identity недоступна")

	snapshotOK := false
	if _, err := a.rebootSnapshot(); err == nil {
		snapshotOK = true
	}
	add("snapshot", snapshotOK, "Non-secret acceptance snapshot готов", "Acceptance snapshot не может быть безопасно собран")

	rebootReady := regularExecutable(rebootSystemBinary())
	add("reboot_command", rebootReady, "Системная команда reboot доступна", "Системная команда reboot недоступна")

	ready := true
	for _, check := range checks {
		if !check.OK {
			ready = false
			break
		}
	}
	return rebootAcceptancePlanResponse{
		Success:         true,
		Ready:           ready,
		Checks:          checks,
		ExpectedDelta:   "one controlled router reboot; FreeNet/XKeen/Xray restart through existing autostart",
		ExpectedNoDelta: "FreeNet version; VPN/DNS/routing policy; subscription secret; Xray config bytes; administrator credential",
		Mutation:        "NONE",
	}
}

func (a *app) writeRebootMarker(marker rebootAcceptanceMarker) error {
	path := a.rebootAcceptancePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(path, data, 0600)
}

func (a *app) readRebootMarker() (rebootAcceptanceMarker, bool, error) {
	var marker rebootAcceptanceMarker
	path := a.rebootAcceptancePath()
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return marker, false, nil
	}
	if err != nil {
		return marker, false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > rebootAcceptanceMarkerMaxSize {
		return marker, true, errors.New("invalid reboot acceptance marker")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return marker, true, err
	}
	if err := json.Unmarshal(data, &marker); err != nil || marker.Version != rebootAcceptanceMarkerVersion || marker.BootID == "" || marker.FreeNetVersion == "" {
		return rebootAcceptanceMarker{}, true, errors.New("invalid reboot acceptance marker")
	}
	return marker, true, nil
}

func (a *app) scheduleReboot(marker rebootAcceptanceMarker) {
	go func() {
		time.Sleep(rebootCommandDelay)
		if err := rebootCommandRunner(); err != nil {
			marker.State = "FAILED_START"
			_ = a.writeRebootMarker(marker)
		}
	}()
}

func (a *app) handleRebootAcceptancePlan(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.rebootPlan())
}

func (a *app) handleRebootAcceptanceApply(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, rebootAcceptanceApplyResponse{Success: false, State: "STOP", Mutation: "NONE", Error: "request rejected"})
		return
	}
	if ct := strings.ToLower(r.Header.Get("Content-Type")); !strings.HasPrefix(ct, "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, rebootAcceptanceApplyResponse{Success: false, State: "STOP", Mutation: "NONE", Error: "application/json required"})
		return
	}
	body := http.MaxBytesReader(w, r.Body, 1024)
	defer body.Close()
	var req rebootAcceptanceApplyRequest
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || req.Confirm != "REBOOT" {
		writeJSON(w, http.StatusBadRequest, rebootAcceptanceApplyResponse{Success: false, State: "STOP", Mutation: "NONE", Error: "explicit reboot confirmation required"})
		return
	}

	plan := a.rebootPlan()
	if !plan.Ready {
		writeJSON(w, http.StatusConflict, rebootAcceptanceApplyResponse{Success: false, State: "STOP", Mutation: "NONE", Error: "reboot preflight is not ready"})
		return
	}
	marker, err := a.rebootSnapshot()
	if err != nil {
		writeJSON(w, http.StatusConflict, rebootAcceptanceApplyResponse{Success: false, State: "STOP", Mutation: "NONE", Error: "cannot create reboot acceptance snapshot"})
		return
	}
	if err := a.writeRebootMarker(marker); err != nil {
		writeJSON(w, http.StatusInternalServerError, rebootAcceptanceApplyResponse{Success: false, State: "STOP", Mutation: "NONE", Error: "cannot commit reboot acceptance snapshot"})
		return
	}
	a.scheduleReboot(marker)
	writeJSON(w, http.StatusAccepted, rebootAcceptanceApplyResponse{
		Success:  true,
		State:    "REBOOT_SCHEDULED",
		Message:  "Перезагрузка запланирована. После возврата FreeNet выполнит read-only acceptance.",
		Mutation: "REBOOT_ONLY",
	})
}

func (a *app) rebootPostBootChecks(marker rebootAcceptanceMarker) []rebootAcceptanceCheck {
	checks := make([]rebootAcceptanceCheck, 0, 9)
	add := func(key string, ok bool, good, bad string) {
		detail := bad
		if ok {
			detail = good
		}
		checks = append(checks, rebootAcceptanceCheck{Key: key, OK: ok, Detail: detail})
	}

	add("version", marker.FreeNetVersion == "v"+version, "Версия FreeNet сохранена", "Версия FreeNet изменилась")

	installScenario, setupComplete := readSetupState(a.cfg.ConfigPath)
	add("setup", setupComplete == marker.SetupComplete && setupComplete && installScenario == marker.InstallScenario, "Setup/install state сохранён", "Setup/install state изменился")
	add("auth", marker.AuthConfigured && a.credentialConfigured(), "Administrator credential доступен после reboot", "Administrator credential не подтверждён после reboot")
	add("xray", rebootProcessRunning("xray"), "Xray работает после reboot", "Xray не работает после reboot")
	add("freenet_init", rebootFreeNetInitReady(), "FreeNet autostart init подтверждён", "FreeNet autostart init не подтверждён")
	add("xkeen_autostart", rebootXKeenAutostartOn(), "XKeen autostart подтверждён", "XKeen autostart не подтверждён")

	configHash, err := hashRegularFile(a.cfg.ConfigPath)
	add("config", err == nil && configHash == marker.ConfigSHA256, "FreeNet config bytes не изменились", "FreeNet config bytes изменились или недоступны")

	xrayHashes, err := xrayConfigHashes(filepath.Dir(a.cfg.OutPath))
	add("xray_config", err == nil && equalHashMap(xrayHashes, marker.XraySHA256), "Xray config bytes не изменились", "Xray config bytes изменились или недоступны")

	schedulerOK, schedulerErr := a.settingsV3SchedulerCurrent()
	add("scheduler", schedulerErr == nil && schedulerOK, "Планировщик canonical после startup", "Планировщик не подтверждён после startup")
	return checks
}

func (a *app) handleRebootAcceptanceState(w http.ResponseWriter, _ *http.Request) {
	marker, exists, err := a.readRebootMarker()
	if err != nil {
		writeJSON(w, http.StatusOK, rebootAcceptanceStateResponse{
			Success: true, State: "UNKNOWN", Complete: true, Passed: false,
			Message: "Reboot acceptance marker повреждён или недоступен.", Mutation: "NONE",
		})
		return
	}
	if !exists {
		writeJSON(w, http.StatusOK, rebootAcceptanceStateResponse{
			Success: true, State: "IDLE", Complete: false, Passed: false,
			Message: "Проверка после перезагрузки ещё не запускалась.", Mutation: "NONE",
		})
		return
	}
	if marker.State == "FAILED_START" {
		writeJSON(w, http.StatusOK, rebootAcceptanceStateResponse{
			Success: true, State: "FAILED_START", Complete: true, Passed: false,
			PreparedAt: marker.PreparedAt,
			Message: "Системная команда перезагрузки не запустилась. Runtime не считается перезагруженным.", Mutation: "NONE",
		})
		return
	}
	currentBootID, err := rebootReadBootID()
	if err != nil {
		writeJSON(w, http.StatusOK, rebootAcceptanceStateResponse{
			Success: true, State: "UNKNOWN", Complete: true, Passed: false,
			PreparedAt: marker.PreparedAt,
			Message: "Не удалось подтвердить boot identity.", Mutation: "NONE",
		})
		return
	}
	if currentBootID == marker.BootID {
		writeJSON(w, http.StatusOK, rebootAcceptanceStateResponse{
			Success: true, State: "AWAITING_REBOOT", Complete: false, Passed: false,
			PreparedAt: marker.PreparedAt,
			Message: "Snapshot сохранён; ожидается новый boot.", Mutation: "NONE",
		})
		return
	}

	checks := a.rebootPostBootChecks(marker)
	passed := true
	for _, check := range checks {
		if !check.OK {
			passed = false
			break
		}
	}
	state := "FAIL"
	message := "Роутер загрузился, но один или несколько acceptance checks не прошли. Автоматические исправления не выполнялись."
	if passed {
		state = "PASS"
		message = "Роутер загрузился; FreeNet/XKeen/Xray и сохранённое состояние прошли post-boot acceptance."
	}
	writeJSON(w, http.StatusOK, rebootAcceptanceStateResponse{
		Success: true, State: state, Complete: true, Passed: passed,
		PreparedAt: marker.PreparedAt, Checks: checks, Message: message, Mutation: "NONE",
	})
}

func rebootAwaitContext(parent context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-parent.Done():
		return parent.Err()
	case <-timer.C:
		return nil
	}
}

func rebootAcceptanceSummary(checks []rebootAcceptanceCheck) string {
	var failed []string
	for _, check := range checks {
		if !check.OK {
			failed = append(failed, check.Key)
		}
	}
	sort.Strings(failed)
	return strings.Join(failed, ",")
}

func formatRebootError(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%T", err)
}
