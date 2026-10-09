package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	xrayRuntimeDesiredRunning = "running"
	xrayRuntimeDesiredStopped = "stopped"

	xrayRuntimeWatchInterval = 5 * time.Second
	xrayRuntimeOfflineDebounce = 10 * time.Second
	xrayRuntimeRecoveryRetry = 60 * time.Second
	xrayRuntimeRecoveryTimeout = 50 * time.Second
)

type xrayRuntimeWatchState struct {
	OfflineSince      time.Time
	IncidentOpen      bool
	NextRecovery      time.Time
	IntentFaultLogged bool
}

var xrayRuntimeProcessRunning = func(a *app) bool {
	if a == nil {
		return false
	}
	return a.liveXrayRunning()
}

var xrayRuntimeStartControlled = func(a *app, ctx context.Context) error {
	if a == nil {
		return errors.New("FreeNet app is unavailable")
	}
	return a.startXrayControlled(ctx)
}

func xrayRuntimeIntentPath(configPath string) string {
	if value := strings.TrimSpace(os.Getenv("FREENET_XRAY_RUNTIME_INTENT")); value != "" {
		return value
	}
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		configPath = defaultConfigPath
	}
	return filepath.Join(filepath.Dir(configPath), "xray-runtime.intent")
}

func validXrayRuntimeDesiredState(value string) bool {
	return value == xrayRuntimeDesiredRunning || value == xrayRuntimeDesiredStopped
}

func readXrayRuntimeDesiredState(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(data))
	if !validXrayRuntimeDesiredState(value) {
		return "", errors.New("invalid Xray runtime desired state")
	}
	return value, nil
}

func writeXrayRuntimeDesiredState(path, desired string) error {
	if !validXrayRuntimeDesiredState(desired) {
		return errors.New("invalid Xray runtime desired state")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return atomicWrite(path, []byte(desired+"\n"), 0600)
}

func (a *app) xrayRuntimeDesiredState() string {
	if a == nil {
		return "unknown"
	}
	desired, err := readXrayRuntimeDesiredState(xrayRuntimeIntentPath(a.cfg.ConfigPath))
	if err != nil {
		return "unknown"
	}
	return desired
}

func (a *app) setXrayRuntimeDesiredState(desired string) error {
	if a == nil {
		return errors.New("FreeNet app is unavailable")
	}
	return writeXrayRuntimeDesiredState(xrayRuntimeIntentPath(a.cfg.ConfigPath), desired)
}

func (a *app) initializeXrayRuntimeIntent() {
	if a == nil {
		return
	}
	path := xrayRuntimeIntentPath(a.cfg.ConfigPath)
	if _, err := readXrayRuntimeDesiredState(path); err == nil {
		return
	}

	desired := xrayRuntimeDesiredStopped
	observed := "stopped"
	recovery := "disabled"
	if xrayRuntimeProcessRunning(a) {
		desired = xrayRuntimeDesiredRunning
		observed = "running"
		recovery = "enabled"
	}
	if err := writeXrayRuntimeDesiredState(path, desired); err != nil {
		v3AppendEvent("xray_runtime", "failed", "Не удалось инициализировать persistent desired state Xray. AUTO runtime recovery не будет выполнять mutation без подтверждённого intent.")
		return
	}
	v3AppendEvent("xray_runtime", "success", fmt.Sprintf(
		"Контроль Xray runtime инициализирован: observed=%s; desired=%s; auto-recovery=%s.",
		observed, desired, recovery,
	))
}

func (a *app) xrayRuntimeMutationBusy() bool {
	// Never start an offline core against uncommitted VPN files after a
	// provider timeout. A read-only recovery must reconcile this first.
	if automationMutationBlockedState() {
		return true
	}
	if a == nil {
		return true
	}
	if a.sem != nil && len(a.sem) > 0 {
		return true
	}
	if a.updateLockHeld() {
		return true
	}
	if path := strings.TrimSpace(a.cfg.LockPath); path != "" {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

func (a *app) xrayRuntimeSafeContext() string {
	if a == nil {
		return "profile=unknown; endpoint=unknown"
	}
	profile := strings.TrimSpace(currentExactProfileLabel(a.cfg.FilterPath))
	if profile == "" {
		profile = "unknown"
	}
	endpoint := strings.TrimSpace(readBestServerCurrentEndpoint(a.cfg.OutPath))
	if endpoint == "" {
		endpoint = "unknown"
	}
	return "profile=" + profile + "; endpoint=" + endpoint
}

func xrayRuntimeElapsed(since, now time.Time) time.Duration {
	if since.IsZero() || now.Before(since) {
		return 0
	}
	return now.Sub(since).Round(time.Second)
}

func resetXrayRuntimeIncident(state *xrayRuntimeWatchState) {
	if state == nil {
		return
	}
	state.OfflineSince = time.Time{}
	state.IncidentOpen = false
	state.NextRecovery = time.Time{}
}

func (a *app) xrayRuntimeHealthGate() (automationHealthProbe, bool) {
	if a == nil || xrayRuntimeProcessRunning(a) {
		return automationHealthProbe{}, false
	}
	desired, err := readXrayRuntimeDesiredState(xrayRuntimeIntentPath(a.cfg.ConfigPath))
	if err != nil {
		return automationHealthProbe{
			State: automationHealthUncertain,
			Reason: "Основной Xray остановлен, а desired state недоступен. VPN сейчас не подключен; AUTO VPN не подменяет production runtime isolated-проверкой и не выполняет mutation.",
		}, true
	}
	if desired == xrayRuntimeDesiredStopped {
		return automationHealthProbe{
			State: automationHealthUncertain,
			Reason: "Xray остановлен вручную через FreeNet. VPN сейчас не подключен; автоматический подъём и AUTO VPN mutation отключены до Start/Restart.",
		}, true
	}
	return automationHealthProbe{
		State: automationHealthUncertain,
		Reason: "Основной Xray неожиданно остановлен. VPN сейчас не подключен; Xray runtime watchdog выполняет recovery, а AUTO VPN endpoint mutation временно подавлена.",
	}, true
}

func (a *app) runXrayRuntimeWatchStep(now time.Time, state *xrayRuntimeWatchState) {
	if a == nil || state == nil {
		return
	}
	desired, err := readXrayRuntimeDesiredState(xrayRuntimeIntentPath(a.cfg.ConfigPath))
	if err != nil {
		if !state.IntentFaultLogged {
			v3AppendEvent("xray_runtime", "failed", "Persistent desired state Xray недоступен. AUTO runtime recovery остановлен fail-closed; VPN runtime mutation не выполняется.")
			state.IntentFaultLogged = true
		}
		return
	}
	state.IntentFaultLogged = false

	online := xrayRuntimeProcessRunning(a)
	if desired == xrayRuntimeDesiredStopped {
		resetXrayRuntimeIncident(state)
		return
	}

	if online {
		if state.IncidentOpen {
			v3AppendEvent("xray_runtime", "success", fmt.Sprintf(
				"Xray runtime снова работает после incident; VPN подключение восстановлено без endpoint mutation; downtime≈%s; %s.",
				xrayRuntimeElapsed(state.OfflineSince, now), a.xrayRuntimeSafeContext(),
			))
		}
		resetXrayRuntimeIncident(state)
		return
	}

	if a.xrayRuntimeMutationBusy() {
		return
	}
	if state.OfflineSince.IsZero() {
		state.OfflineSince = now
		return
	}
	if now.Sub(state.OfflineSince) < xrayRuntimeOfflineDebounce {
		return
	}

	if !state.IncidentOpen {
		state.IncidentOpen = true
		v3AppendEvent("xray_runtime", "failed", fmt.Sprintf(
			"Xray неожиданно остановлен при desired=running. VPN сейчас не подключен; подтверждение offline=%s; auto-recovery=enabled; %s.",
			xrayRuntimeElapsed(state.OfflineSince, now), a.xrayRuntimeSafeContext(),
		))
	}
	if !state.NextRecovery.IsZero() && now.Before(state.NextRecovery) {
		return
	}

	if a.sem == nil {
		state.NextRecovery = now.Add(xrayRuntimeRecoveryRetry)
		v3AppendEvent("xray_runtime", "failed", "AUTO recovery Xray не запущен. PRIMARY ERROR: mutation semaphore недоступен; следующая попытка через 60 с.")
		return
	}
	select {
	case a.sem <- struct{}{}:
	default:
		state.NextRecovery = now.Add(xrayRuntimeWatchInterval)
		return
	}

	v3AppendEvent("xray_runtime", "started", fmt.Sprintf(
		"Запущен controlled Start Xray после неожиданной остановки; endpoint mutation=NONE; %s.",
		a.xrayRuntimeSafeContext(),
	))
	ctx, cancel := context.WithTimeout(context.Background(), xrayRuntimeRecoveryTimeout)
	recoveryErr := xrayRuntimeStartControlled(a, ctx)
	cancel()
	<-a.sem

	if recoveryErr != nil || !xrayRuntimeProcessRunning(a) {
		primary := "Xray не перешёл в рабочее состояние после controlled Start"
		if recoveryErr != nil {
			primary = sanitizeAutomationReason(recoveryErr.Error())
		}
		state.NextRecovery = now.Add(xrayRuntimeRecoveryRetry)
		v3AppendEvent("xray_runtime", "failed", fmt.Sprintf(
			"AUTO recovery Xray не завершён. PRIMARY ERROR: %s; VPN сейчас не подключен; rollback=NOT_APPLICABLE; endpoint mutation=NONE; следующая попытка через 60 с.",
			primary,
		))
		return
	}

	v3AppendEvent("xray_runtime", "success", fmt.Sprintf(
		"Xray автоматически восстановлен и post-check подтверждён; VPN runtime снова подключен; downtime≈%s; endpoint mutation=NONE; %s.",
		xrayRuntimeElapsed(state.OfflineSince, now), a.xrayRuntimeSafeContext(),
	))
	resetXrayRuntimeIncident(state)
}

func (a *app) startXrayRuntimeWatchdog() {
	if a == nil {
		return
	}
	go func() {
		state := &xrayRuntimeWatchState{}
		ticker := time.NewTicker(xrayRuntimeWatchInterval)
		defer ticker.Stop()
		for now := range ticker.C {
			a.runXrayRuntimeWatchStep(now.UTC(), state)
		}
	}()
}
