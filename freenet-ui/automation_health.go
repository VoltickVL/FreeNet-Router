package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	automationHealthHealthy   = "healthy"
	automationHealthFailed    = "failed"
	automationHealthUncertain = "uncertain"
	automationHealthCritical  = "critical"

	automationHealthProbeTimeout      = 12 * time.Second
	automationHealthRunTimeout        = 35 * time.Second
	automationEndpointRecoveryTimeout = 20 * time.Second

	// Reachable quality degradation must be repeated before Full AUTO spends
	// bandwidth on a heavy Best Server comparison. A completed optimization
	// attempt is also cooled down independently from the existing post-switch
	// cooldown, so a noisy line cannot trigger repeated Top-3 scans.
	automationQualityStrikeWindow          = 30 * time.Minute
	automationQualityStrikeThreshold       = 3
	automationQualityOptimizationCooldown  = 1 * time.Hour
	automationQualitySevereCooldown        = 15 * time.Minute
	automationQualityLatencyMildMS         = 200
	automationQualityLatencyStrongMS       = 250
	automationQualityLatencySevereMS       = 500
)

type automationHealthResult struct {
	State   string
	Reason  string
	Mutated bool
}

type automationHealthProbe struct {
	State           string
	Reason          string
	QualityDegraded bool
	QualityPoints   int
}

var automationEndpointCurrentRefresh = func(a *app, ctx context.Context) (int, bestServerRefreshResponse) {
	return a.executeBestServerCurrentRefresh(ctx)
}

func automationEndpointUpdateBusy(out []byte) bool {
	text := strings.ToLower(sanitizeOutput(string(out)))
	return strings.Contains(text, "another updater instance is already running") ||
		strings.Contains(text, "updater is already busy") ||
		strings.Contains(text, "another freenet operation is already running")
}

func automationEndpointRollbackUnknown(out []byte) bool {
	text := strings.ToLower(sanitizeOutput(string(out)))
	return strings.Contains(text, "rollback error") ||
		strings.Contains(text, "rollback failed") ||
		strings.Contains(text, "rollback unknown") ||
		strings.Contains(text, "failed_unknown")
}

func classifyAutomationHealth(first automationHealthProbe, wanHealthy bool, second automationHealthProbe) automationHealthProbe {
	if first.State == automationHealthHealthy {
		return first
	}
	if first.State != automationHealthFailed {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Состояние текущего VPN не удалось подтвердить; изменений нет."}
	}
	if !wanHealthy {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Обычный доступ в интернет не подтверждён; автоматическая смена VPN отменена."}
	}
	if second.State == automationHealthHealthy {
		return automationHealthProbe{State: automationHealthHealthy, Reason: "Текущий VPN восстановился на повторной проверке; изменений нет."}
	}
	if second.State != automationHealthFailed {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Повторная проверка текущего VPN неоднозначна; изменений нет."}
	}
	return automationHealthProbe{State: automationHealthCritical, Reason: "Текущий VPN дважды не прошёл проверку при рабочем обычном интернете."}
}

func automationHealthLockPath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_AUTO_HEALTH_LOCK")); value != "" {
		return value
	}
	return "/tmp/freenet-auto-health.lock"
}

func automationHealthLockOwnerState(path string) (alive bool, known bool) {
	data, err := os.ReadFile(filepath.Join(path, "pid"))
	if err != nil {
		return false, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false, false
	}
	err = syscall.Kill(pid, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return true, true
	}
	if errors.Is(err, syscall.ESRCH) {
		return false, true
	}
	return false, false
}

func reclaimAutomationHealthLockIfStale(path string) bool {
	alive, ownerKnown := automationHealthLockOwnerState(path)
	if ownerKnown {
		if alive {
			return false
		}
		_ = os.RemoveAll(path)
		return true
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return true
	}
	if err == nil && time.Since(info.ModTime()) > 10*time.Minute {
		_ = os.RemoveAll(path)
		return true
	}
	return false
}

func acquireAutomationHealthLock() (func(), error) {
	path := automationHealthLockPath()
	try := func() error {
		if err := os.Mkdir(path, 0700); err != nil {
			return err
		}
		_ = os.WriteFile(filepath.Join(path, "pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0600)
		return nil
	}
	if err := try(); err == nil {
		return func() { _ = os.RemoveAll(path) }, nil
	}
	if reclaimAutomationHealthLockIfStale(path) {
		if err := try(); err == nil {
			return func() { _ = os.RemoveAll(path) }, nil
		}
	}
	return nil, errAutomationBusy
}

func automationServicePathHealthy(ok, total int) bool {
	return total >= 3 && ok == total
}

func automationApplicationPathHealthy(ms int) bool {
	return ms > 0 && ms <= bestServerQualityMaxApplicationMS
}

func automationApplicationQualityPoints(ms int) int {
	switch {
	case ms <= 0:
		return 0
	case ms > automationQualityLatencySevereMS:
		return 3
	case ms > automationQualityLatencyStrongMS:
		return 2
	case ms > automationQualityLatencyMildMS:
		return 1
	default:
		return 0
	}
}

func automationServiceQualityPoints(ok, total int) int {
	if total < 3 || ok >= total {
		return 0
	}
	if ok <= 0 {
		return 3
	}
	if ok*2 <= total {
		return 2
	}
	return 1
}

func classifyAutomationReachableQuality(applicationMS, serviceOK, serviceTotal int) automationHealthProbe {
	latencyPoints := automationApplicationQualityPoints(applicationMS)
	servicePoints := automationServiceQualityPoints(serviceOK, serviceTotal)
	points := latencyPoints
	if servicePoints > points {
		points = servicePoints
	}
	if points == 0 {
		return automationHealthProbe{State: automationHealthHealthy, Reason: "Текущий VPN и сервисные маршруты работают стабильно."}
	}

	reason := fmt.Sprintf("VPN отвечает, но качество соединения ухудшено: отклик %d мс, сервисы %d/%d. AUTO VPN накапливает подтверждение деградации.", applicationMS, serviceOK, serviceTotal)
	if points >= automationQualityStrikeThreshold {
		reason = fmt.Sprintf("VPN отвечает, но качество соединения критически ухудшено: отклик %d мс, сервисы %d/%d. AUTO VPN запускает ускоренную проверку замены.", applicationMS, serviceOK, serviceTotal)
	}
	return automationHealthProbe{
		State:           automationHealthUncertain,
		Reason:          reason,
		QualityDegraded: true,
		QualityPoints:   points,
	}
}

func automationQualityOptimizationPlan(state map[string]string, now time.Time, points int) (map[string]string, bool) {
	updates := map[string]string{}
	parseStamp := func(key string) time.Time {
		value := strings.TrimSpace(state[key])
		if value == "" {
			return time.Time{}
		}
		stamp, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return time.Time{}
		}
		return stamp
	}
	count, _ := strconv.Atoi(strings.TrimSpace(state["QUALITY_DEGRADED_COUNT"]))
	if count < 0 {
		count = 0
	}
	start := parseStamp("QUALITY_DEGRADED_SINCE")
	lastOptimization := parseStamp("QUALITY_OPTIMIZATION_LAST")

	if points <= 0 {
		// A confirmed healthy observation breaks the degradation sequence.
		// Runtime v0.5.0 evidence showed that carrying mild strikes across an
		// intervening healthy sample caused expensive Top-3 scans for transient
		// 3/4 or short latency spikes. Keep the optimization cooldown, but clear
		// only the pending quality evidence.
		if count != 0 || !start.IsZero() {
			updates["QUALITY_DEGRADED_COUNT"] = "0"
			updates["QUALITY_DEGRADED_SINCE"] = ""
		}
		return updates, false
	}

	if start.IsZero() || now.Before(start) || now.Sub(start) > automationQualityStrikeWindow {
		start = now
		count = 0
	}
	count += points
	updates["QUALITY_DEGRADED_COUNT"] = strconv.Itoa(count)
	updates["QUALITY_DEGRADED_SINCE"] = start.UTC().Format(time.RFC3339)

	cooldown := automationQualityOptimizationCooldown
	if points >= automationQualityStrikeThreshold {
		cooldown = automationQualitySevereCooldown
	}
	if !lastOptimization.IsZero() && now.Before(lastOptimization.Add(cooldown)) {
		return updates, false
	}
	if count < automationQualityStrikeThreshold {
		return updates, false
	}

	updates["QUALITY_DEGRADED_COUNT"] = "0"
	updates["QUALITY_DEGRADED_SINCE"] = ""
	updates["QUALITY_OPTIMIZATION_LAST"] = now.UTC().Format(time.RFC3339)
	return updates, true
}

func automationQualityOptimizationDue(now time.Time, points int) bool {
	state := v3ParseState(settingsV3StatePath())
	updates, due := automationQualityOptimizationPlan(state, now, points)
	if len(updates) > 0 {
		_ = v3WriteState(updates)
	}
	return due
}

func automationQualityOptimizationStartReason(probe automationHealthProbe) string {
	trigger := strings.TrimSpace(probe.Reason)
	if trigger == "" {
		trigger = "Текущая quality-проверка не содержит подробностей."
	}
	if probe.QualityPoints >= automationQualityStrikeThreshold {
		return "Критическая деградация качества подтверждена одним severe-наблюдением. Триггер: " + trigger + " Сравниваем текущий VPN с fully measured Top-3."
	}
	return "Последовательная деградация качества подтверждена накоплением mild/strong evidence. Триггер: " + trigger + " Сравниваем текущий VPN с fully measured Top-3."
}

func classifyAutomationApplicationFailure(transportOK bool) automationHealthProbe {
	if transportOK {
		return automationHealthProbe{
			State: automationHealthUncertain,
			Reason: "VPN-транспорт отвечает, но независимые HTTPS/DNS проверки по именам не подтверждены. AUTO VPN сохраняет текущее подключение без изменений.",
		}
	}
	return automationHealthProbe{
		State: automationHealthFailed,
		Reason: "Текущий VPN не подтвердил доступ ни через независимые HTTPS-проверки, ни через IP-транспорт.",
	}
}

func probeAutomationWAN(ctx context.Context) bool {
	targets := []string{"1.1.1.1:443", "77.88.8.8:53"}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	for _, target := range targets {
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		conn, err := dialer.DialContext(probeCtx, "tcp", target)
		cancel()
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}

func (a *app) probeAutomationCurrentVPN(ctx context.Context) automationHealthProbe {
	if runtimeGate, blocked := a.xrayRuntimeHealthGate(); blocked {
		return runtimeGate
	}
	outbound, activeEndpoint, ok := readBestServerActiveOutbound(a.cfg.OutPath)
	currentEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	if !ok || currentEndpoint == "" || !endpointsEqual(activeEndpoint, currentEndpoint) {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Текущий VPN не удалось безопасно определить."}
	}
	isolatedOutbound, err := prepareIsolatedProbeOutbound(outbound)
	if err != nil {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Не удалось безопасно подготовить изолированную проверку VPN."}
	}
	releaseProbe, acquired := acquireIsolatedXrayProbe(ctx)
	if !acquired {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Ресурс изолированной проверки VPN сейчас занят."}
	}
	defer releaseProbe()
	outbound = isolatedOutbound
	xrayPath := strings.TrimSpace(os.Getenv("FREENET_XRAY_BIN"))
	if xrayPath == "" {
		xrayPath = defaultBestServerXrayPath
	}
	if _, err := os.Stat(xrayPath); err != nil {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Xray недоступен для проверки VPN."}
	}
	curlPath, err := exec.LookPath("curl")
	if err != nil {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Средство проверки подключения недоступно."}
	}
	port, err := reserveBestServerPort()
	if err != nil {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Не удалось подготовить локальную проверку VPN."}
	}
	tmpDir, err := os.MkdirTemp("", "freenet-auto-health-")
	if err != nil {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Не удалось подготовить временную проверку VPN."}
	}
	defer os.RemoveAll(tmpDir)
	_ = os.Chmod(tmpDir, 0700)
	probeXrayPath, err := isolatedXrayProbePath(tmpDir, xrayPath)
	if err != nil {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Не удалось изолировать процесс проверки VPN."}
	}

	config := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"listen": "127.0.0.1", "port": port, "protocol": "socks",
			"settings": map[string]any{"udp": false}, "tag": "freenet-auto-health",
		}},
		"outbounds": []any{outbound},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{map[string]any{
				"type": "field", "inboundTag": []string{"freenet-auto-health"}, "outboundTag": "vless-reality",
			}},
		},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Не удалось собрать конфигурацию проверки VPN."}
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "00_probe.json"), encoded, 0600); err != nil {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Не удалось записать конфигурацию проверки VPN."}
	}

	env := isolatedXrayProbeEnv(append(os.Environ(), "XRAY_LOCATION_ASSET="+a.geoDataAssetDir()))
	testCtx, cancelTest := context.WithTimeout(ctx, 4*time.Second)
	testCmd := exec.CommandContext(testCtx, probeXrayPath, "run", "-test", "-confdir", tmpDir)
	testCmd.Env = env
	testCmd.Stdout = io.Discard
	testCmd.Stderr = io.Discard
	testCmd.WaitDelay = 2 * time.Second
	testErr := testCmd.Run()
	cancelTest()
	if testErr != nil {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Конфигурация текущего VPN не прошла локальную проверку."}
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	cmd := exec.CommandContext(runCtx, probeXrayPath, "run", "-confdir", tmpDir)
	cmd.Env = env
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Не удалось запустить изолированную проверку VPN."}
	}
	defer func() {
		cancelRun()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	if !waitBestServerSOCKS(ctx, port) {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Локальная проверка VPN не запустилась."}
	}

	socks := fmt.Sprintf("127.0.0.1:%d", port)
	applicationMS, _, ok := probeBestServerHTTPAny(ctx, curlPath, socks)
	if !ok {
		return classifyAutomationApplicationFailure(probeBestServerTransportIP(ctx, curlPath, socks))
	}
	serviceOK, serviceTotal := probeBestServerServiceReachability(ctx, curlPath, socks)
	return classifyAutomationReachableQuality(applicationMS, serviceOK, serviceTotal)
}

func automationCurrentCountry(a *app) string {
	if code := bestServerCountryCodeFromLabel(currentExactProfileLabel(a.cfg.FilterPath)); code != "" {
		return code
	}
	return strings.ToLower(strings.TrimSpace(a.status().CountryCode))
}

func (a *app) runAutomationBestEmergencyCycle(parent context.Context, settings automationSettings) (automationBestCycleResult, error) {
	release, err := acquireAutomationBestLock()
	if err != nil {
		return automationBestCycleResult{Result: "busy", Reason: "Поиск аварийной замены пропущен: другая Best Server / recovery операция уже выполняется."}, nil
	}
	defer release()
	result, _, runErr := a.runAutomationBestEmergencyCycleLocked(parent, settings)
	return result, runErr
}

func (a *app) runAutomationEndpointEmergency(parent context.Context, settings automationSettings) (automationHealthResult, error) {
	if settings.Mode != automationModeEndpoint {
		return automationHealthResult{State: automationHealthUncertain, Reason: "Восстановление текущего VPN не применимо к текущему режиму."}, nil
	}
	if !settings.AutoApply {
		return automationHealthResult{State: automationHealthCritical, Reason: "Текущий VPN недоступен, но автоматическое восстановление выключено."}, nil
	}

	// Use the same transactional current-profile refresh as manual refresh and
	// scheduled subscription reconciliation. It fetches a fresh provider
	// revision, validates it off-path, snapshots runtime state, applies it,
	// performs the post-apply VPN Internet acceptance and rolls back on failure.
	// The legacy `vpn update` path must not be a second recovery engine.
	ctx, cancel := context.WithTimeout(parent, automationEndpointRecoveryTimeout)
	status, refresh := automationEndpointCurrentRefresh(a, ctx)
	cancel()

	message := strings.TrimSpace(refresh.Message)
	if message == "" {
		message = strings.TrimSpace(refresh.Error)
	}
	rollback := strings.TrimSpace(refresh.RollbackState)
	if rollback == "" {
		rollback = "NOT_APPLIED"
	}

	if status >= 200 && status < 300 && refresh.Success && refresh.Applied {
		if message == "" {
			message = "Текущий VPN восстановлен штатным обновлением текущего профиля и подтверждён post-apply проверкой."
		}
		return automationHealthResult{State: automationHealthHealthy, Reason: message, Mutated: true}, nil
	}

	if rollback == "FAILED/UNKNOWN" || automationRollbackBlocksMutation(rollback) {
		if message == "" {
			message = "Штатное обновление текущего VPN завершилось с неизвестным состоянием rollback."
		}
		writeAutomationStateV2("blocked", message, rollback, false)
		appendAutomationHistoryV2("blocked", message+"; rollback="+rollback)
		return automationHealthResult{State: automationHealthCritical, Reason: message}, errors.New("current VPN refresh rollback failed or is unknown")
	}

	// A conflict means the current identity/runtime changed underneath the
	// decision. Fail closed: do not launch a second mutation in this cycle.
	if status == 409 {
		if message == "" {
			message = "Состояние текущего VPN изменилось во время восстановления; следующая mutation отменена."
		}
		return automationHealthResult{State: automationHealthUncertain, Reason: message}, errAutomationBusy
	}

	if status >= 200 && status < 300 && refresh.Success {
		if message == "" {
			message = "Свежий вариант текущего VPN не восстановил соединение; требуется полностью проверенная замена."
		}
		return automationHealthResult{State: automationHealthFailed, Reason: message}, nil
	}

	if message == "" {
		message = "Штатное обновление текущего VPN не завершено; текущая конфигурация не считается восстановленной."
	}
	return automationHealthResult{State: automationHealthFailed, Reason: message}, errors.New("canonical current VPN refresh failed")
}

func recordAndReturnHealth(result automationHealthResult, err error) (automationHealthResult, error) {
	recordSettingsV3Health(result)
	return result, err
}

func appendAutomationRecoveryStage(stage, result, reason string) {
	stage = sanitizeAutomationReason(stage)
	result = sanitizeAutomationReason(result)
	reason = sanitizeAutomationReason(reason)
	if stage == "" {
		stage = "stage"
	}
	if result == "" {
		result = "unknown"
	}
	if reason == "" {
		reason = "без подробностей"
	}
	appendAutomationHistoryV2(stage+":"+result, reason)
}

func automationHealthDue(configPath string, now time.Time) bool {
	interval := automationHealthIntervalDuration(configuredAutomationHealthInterval(configPath))
	if interval <= 0 {
		return false
	}
	state := v3ParseState(settingsV3StatePath())
	last := strings.TrimSpace(state["HEALTH_SCHEDULE_LAST"])
	if last == "" {
		last = strings.TrimSpace(state["HEALTH_LAST"])
	}
	if last == "" {
		return true
	}
	stamp, err := time.Parse(time.RFC3339, last)
	if err != nil {
		return true
	}
	return !now.Before(stamp.Add(interval))
}

func (a *app) runScheduledAutomationHealthWatch(parent context.Context) (bool, automationHealthResult, error) {
	settings := readAutomationSettings(a.cfg.ConfigPath)
	if !settings.Enabled || !automationHealthDue(a.cfg.ConfigPath, time.Now().UTC()) {
		return false, automationHealthResult{}, nil
	}
	result, err := a.runAutomationHealthWatch(parent)
	return true, result, err
}

func (a *app) startAutomationHealthScheduler() {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if configuredAutomationHealthInterval(a.cfg.ConfigPath) != "30s" {
				continue
			}
			if !automationHealthDue(a.cfg.ConfigPath, time.Now().UTC()) {
				continue
			}
			_, _, _ = a.runScheduledAutomationHealthWatch(context.Background())
		}
	}()
}

func automationPostUpdateGuardResult(a *app, first automationHealthProbe) (automationHealthResult, bool) {
	target := automationPendingPostUpdateTarget(a)
	if target == "" {
		return automationHealthResult{}, false
	}
	if first.State == automationHealthHealthy {
		setAutomationPostUpdateAck(target)
		reason := "После обновления текущий VPN подтверждён read-only проверкой; AUTO recovery снова разрешён."
		appendAutomationRecoveryStage("post_update_guard", "cleared", reason)
		appendAutomationHistoryV2("post_update_guard_cleared", reason)
		return automationHealthResult{State: automationHealthHealthy, Reason: reason}, true
	}
	if first.State == automationHealthFailed {
		// A confirmed failed VPN must not create an impossible post-update
		// deadlock. The normal health state machine still requires healthy WAN
		// and a second failed VPN probe before any recovery/mutation.
		reason := "После обновления текущий VPN явно не подтверждает доступ. Post-update hold не блокирует штатный двойной health-check и безопасное recovery."
		if strings.TrimSpace(first.Reason) != "" {
			reason += " " + strings.TrimSpace(first.Reason)
		}
		appendAutomationRecoveryStage("post_update_guard", "recovery_allowed", reason)
		return automationHealthResult{}, false
	}
	reason := "После обновления состояние текущего VPN неоднозначно; AUTO mutation удерживается до определённого результата. Ручная диагностика остаётся доступна."
	if strings.TrimSpace(first.Reason) != "" {
		reason += " " + strings.TrimSpace(first.Reason)
	}
	appendAutomationRecoveryStage("post_update_guard", "blocked", reason)
	return automationHealthResult{State: automationHealthUncertain, Reason: reason}, true
}

func automationRollbackStateKnownFailed(a *app, first automationHealthProbe) bool {
	if a == nil || first.State != automationHealthFailed {
		return false
	}
	_, activeEndpoint, ok := readBestServerActiveOutbound(a.cfg.OutPath)
	currentEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	if !ok || currentEndpoint == "" || !endpointsEqual(activeEndpoint, currentEndpoint) {
		return false
	}
	// The provider transaction owns outbound + exact active filter as one
	// rollback unit. Do not retire an unknown rollback latch unless both pieces
	// are factually readable and identify the same current logical VPN state.
	return strings.TrimSpace(readBestServerCurrentFilter(a.cfg.FilterPath)) != "" &&
		strings.TrimSpace(currentExactProfileLabel(a.cfg.FilterPath)) != ""
}

func automationRollbackGuardResult(a *app, first automationHealthProbe) (automationHealthResult, bool) {
	if !automationMutationBlockedState() {
		return automationHealthResult{}, false
	}
	if first.State == automationHealthHealthy {
		setAutomationMutationBlocked(false)
		reason := "Фактическое состояние текущего VPN подтверждено read-only проверкой; аварийный запрет AUTO mutation снят."
		appendAutomationRecoveryStage("rollback_guard", "cleared", reason)
		appendAutomationHistoryV2("guard_cleared", reason)
		return automationHealthResult{State: automationHealthHealthy, Reason: reason}, true
	}
	if automationRollbackStateKnownFailed(a, first) {
		// UNKNOWN rollback means STOP only until the factual state is known.
		// A deterministic failed probe over a readable outbound + exact filter
		// establishes that state without mutation. Clear only the stale latch;
		// normal health logic must still confirm WAN + a second failed VPN probe
		// before any recovery is allowed.
		setAutomationMutationBlocked(false)
		reason := "После неподтверждённого rollback фактическое состояние установлено read-only как known failed; stale mutation block снят, дальнейшее recovery требует обычного двойного подтверждения."
		appendAutomationRecoveryStage("rollback_guard", "reconciled_failed", reason)
		appendAutomationHistoryV2("guard_reconciled_failed", reason)
		return automationHealthResult{}, false
	}
	reason := "AUTO VPN mutation заблокирована после неподтверждённого rollback. Read-only проверка не установила однозначное фактическое состояние; изменений нет."
	if strings.TrimSpace(first.Reason) != "" {
		reason += " " + strings.TrimSpace(first.Reason)
	}
	appendAutomationRecoveryStage("rollback_guard", "blocked", reason)
	return automationHealthResult{State: automationHealthUncertain, Reason: reason}, true
}

func (a *app) runAutomationHealthWatch(parent context.Context) (automationHealthResult, error) {
	settings := readAutomationSettings(a.cfg.ConfigPath)
	if !settings.Enabled {
		return recordAndReturnHealth(automationHealthResult{State: "disabled", Reason: "AUTO VPN выключен."}, nil)
	}
	_ = v3WriteState(map[string]string{"HEALTH_SCHEDULE_LAST": time.Now().UTC().Format(time.RFC3339)})

	// Read-only observation must never monopolize the AUTO health fence. Manual
	// Current VPN / Best Server diagnostics are the recovery path when a router
	// is already degraded, especially immediately after an update. Only take the
	// exclusive fence when AUTO is actually about to enter recovery/mutation.
	probeCtx, cancel := context.WithTimeout(parent, automationHealthRunTimeout)
	first := a.probeAutomationCurrentVPN(probeCtx)
	if guarded, blocked := automationPostUpdateGuardResult(a, first); blocked {
		cancel()
		return recordAndReturnHealth(guarded, nil)
	}
	if guarded, blocked := automationRollbackGuardResult(a, first); blocked {
		cancel()
		return recordAndReturnHealth(guarded, nil)
	}
	if first.State == automationHealthHealthy {
		_ = automationQualityOptimizationDue(time.Now().UTC(), 0)
		cancel()
		return recordAndReturnHealth(automationHealthResult{State: first.State, Reason: first.Reason}, nil)
	}
	if first.State == automationHealthUncertain {
		qualityDue := false
		if settings.Mode == automationModeBest && first.QualityDegraded {
			qualityDue = automationQualityOptimizationDue(time.Now().UTC(), first.QualityPoints)
		}
		// A non-quality uncertain probe is not proof of recovery and must not
		// clear pending quality evidence. Only a confirmed healthy probe resets
		// the mild/strong strike sequence.
		cancel()
		if qualityDue {
			qualitySettings := settings
			qualitySettings.Policy = automationPolicyBetter
			appendAutomationRecoveryStage("quality_optimization", "start", automationQualityOptimizationStartReason(first))
			best, bestErr := a.runAutomationBestCycleWithSettings(parent, qualitySettings, false)
			appendAutomationRecoveryStage("quality_optimization", best.Result, best.Reason)
			return recordAndReturnHealth(automationHealthResult{State: best.Result, Reason: best.Reason, Mutated: best.Mutated}, bestErr)
		}
		return recordAndReturnHealth(automationHealthResult{State: first.State, Reason: first.Reason}, nil)
	}
	cancel()

	release, err := acquireAutomationHealthLock()
	if err != nil {
		return recordAndReturnHealth(automationHealthResult{State: "busy", Reason: "Проверка пропущена: ручная диагностика или другая AUTO VPN recovery операция уже выполняется."}, nil)
	}
	defer release()

	// State may have changed while manual diagnostics had priority. Re-probe
	// exactly once under the recovery fence before any mutation. The initial
	// read-only FAIL plus this fenced FAIL are the two required confirmations;
	// do not add a third duplicate VPN probe before recovery.
	initialFailure := first
	probeCtx, cancel = context.WithTimeout(parent, automationHealthRunTimeout)
	confirm := a.probeAutomationCurrentVPN(probeCtx)
	if guarded, blocked := automationPostUpdateGuardResult(a, confirm); blocked {
		cancel()
		return recordAndReturnHealth(guarded, nil)
	}
	if guarded, blocked := automationRollbackGuardResult(a, confirm); blocked {
		cancel()
		return recordAndReturnHealth(guarded, nil)
	}
	if confirm.State == automationHealthHealthy || confirm.State == automationHealthUncertain {
		cancel()
		return recordAndReturnHealth(automationHealthResult{State: confirm.State, Reason: confirm.Reason}, nil)
	}
	appendAutomationRecoveryStage("detect", confirm.State, confirm.Reason)
	wanHealthy := probeAutomationWAN(probeCtx)
	cancel()
	decision := classifyAutomationHealth(initialFailure, wanHealthy, confirm)
	if !wanHealthy {
		appendAutomationRecoveryStage("wan", decision.State, decision.Reason)
		return recordAndReturnHealth(automationHealthResult{State: decision.State, Reason: decision.Reason}, nil)
	}
	appendAutomationRecoveryStage("wan", "healthy", "Обычный интернет подтверждён; два read-only FAIL текущего VPN подтверждены.")
	appendAutomationRecoveryStage("confirm", decision.State, decision.Reason)
	if decision.State != automationHealthCritical {
		return recordAndReturnHealth(automationHealthResult{State: decision.State, Reason: decision.Reason}, nil)
	}

	// Critical recovery is single-flight across the entire endpoint + replacement
	// sequence. Acquire the Best/replacement fence before any endpoint mutation,
	// so a competing manual/quality scan cannot make every 30-second health tick
	// repeat the same endpoint refresh while replacement selection is busy.
	incident := newAutomationRecoveryIncident(a, time.Now().UTC())
	appendAutomationRecoveryIncidentStage(
		incident, "incident", "start",
		"wan=healthy; double_vpn_fail=confirmed; recovery=single-flight", time.Now().UTC(),
	)

	releaseBest, bestLockErr := acquireAutomationBestLock()
	if bestLockErr != nil {
		reason := "Аварийное восстановление ждёт завершения другой Best Server / replacement операции; endpoint refresh и новый foreign scan не повторяются."
		appendAutomationRecoveryIncidentStage(incident, "single_flight", "busy", reason, time.Now().UTC())
		return recordAndReturnHealth(automationHealthResult{State: "busy", Reason: reason}, nil)
	}
	defer releaseBest()
	appendAutomationRecoveryIncidentStage(incident, "single_flight", "acquired", "exclusive recovery fence acquired", time.Now().UTC())

	// First keep the exact current logical VPN if one bounded fresh endpoint can
	// restore service. This remains transactional and precedes any profile swap.
	endpointSettings := settings
	endpointSettings.Mode = automationModeEndpoint
	endpointSettings.AutoApply = true
	endpointStarted := time.Now()
	appendAutomationRecoveryIncidentStage(incident, "endpoint_refresh", "start", "Пробуем один bounded fresh endpoint текущего VPN.", endpointStarted)
	endpointResult, endpointErr := a.runAutomationEndpointEmergency(parent, endpointSettings)
	endpointDuration := time.Since(endpointStarted).Round(time.Millisecond)
	appendAutomationRecoveryStage("endpoint_refresh", endpointResult.State, endpointResult.Reason)
	appendAutomationRecoveryIncidentStage(
		incident, "endpoint_refresh", endpointResult.State,
		fmt.Sprintf("duration=%s; %s", endpointDuration, endpointResult.Reason), time.Now().UTC(),
	)
	if endpointErr == nil && endpointResult.State == automationHealthHealthy {
		appendAutomationRecoveryStage("post_check", "success", endpointResult.Reason)
		appendAutomationRecoveryIncidentStage(
			incident, "incident", "recovered",
			"method=current-endpoint; total_downtime_since_detection="+time.Since(incident.StartedAt).Round(time.Second).String(),
			time.Now().UTC(),
		)
		return recordAndReturnHealth(endpointResult, nil)
	}
	if endpointResult.State == automationHealthUncertain {
		appendAutomationRecoveryStage("post_check", automationHealthUncertain, endpointResult.Reason)
		appendAutomationRecoveryIncidentStage(incident, "incident", "uncertain", endpointResult.Reason, time.Now().UTC())
		return recordAndReturnHealth(endpointResult, endpointErr)
	}
	if automationMutationBlockedState() {
		endpointResult.State = automationHealthCritical
		appendAutomationRecoveryStage("rollback", "failed", "Rollback не подтверждён; persistent AUTO mutation block активирован.")
		appendAutomationRecoveryIncidentStage(
			incident, "rollback", "failed",
			"PRIMARY ERROR: rollback не подтверждён; дальнейшая mutation остановлена", time.Now().UTC(),
		)
		return recordAndReturnHealth(endpointResult, endpointErr)
	}

	if settings.Mode == automationModeEndpoint {
		reason := "Режим «Только текущий VPN»: endpoint текущего профиля не восстановил соединение; автоматическая смена страны или VPN-профиля запрещена."
		appendAutomationRecoveryStage("candidate_selection", "blocked", reason)
		appendAutomationRecoveryIncidentStage(incident, "candidate_selection", "blocked", reason, time.Now().UTC())
		result := automationHealthResult{State: automationHealthCritical, Reason: reason, Mutated: endpointResult.Mutated}
		return recordAndReturnHealth(result, endpointErr)
	}

	// Emergency recovery deliberately does not run full throughput/stability
	// Best Server quality. It validates a small, country-diverse cohort through
	// real application HTTPS, then relies on the existing transactional provider
	// plan/apply/post-check contract. Full quality optimization remains separate.
	bestSettings := settings
	bestSettings.Mode = automationModeBest
	bestSettings.Policy = automationPolicyDegraded
	bestSettings.AutoApply = true
	appendAutomationRecoveryStage("candidate_selection", "start", "Endpoint fast-path не восстановил VPN; запускаем bounded application-ready emergency replacement.")
	appendAutomationRecoveryIncidentStage(
		incident, "candidate_selection", "start",
		fmt.Sprintf("budget=%s; cohort_limit=%d", automationEmergencyScanTimeout, automationEmergencyCandidateLimit),
		time.Now().UTC(),
	)
	best, scan, bestErr := a.runAutomationBestEmergencyCycleLocked(parent, bestSettings)
	selected := "none"
	if name := profileDisplayName(scan.Candidate.Profile.Name); name != "" {
		selected = name
	}
	appendAutomationRecoveryIncidentStage(
		incident, "candidate_scan", best.Result,
		fmt.Sprintf(
			"duration=%s; pool=%d; checked=%d; reachable=%d; selected=%s",
			scan.ScanDuration, scan.Total, scan.Checked, scan.Reachable, selected,
		),
		time.Now().UTC(),
	)
	if scan.ApplyDuration > 0 {
		appendAutomationRecoveryIncidentStage(
			incident, "apply", best.Result,
			fmt.Sprintf("duration=%s; rollback=%s; %s", scan.ApplyDuration, strings.TrimSpace(best.RollbackState), best.Reason),
			time.Now().UTC(),
		)
	}
	appendAutomationRecoveryStage("apply", best.Result, best.Reason)
	if best.RollbackState != "" && best.RollbackState != "NOT_NEEDED" && best.RollbackState != "yes" {
		appendAutomationRecoveryStage("rollback", best.RollbackState, best.Reason)
		appendAutomationRecoveryIncidentStage(
			incident, "rollback", best.RollbackState,
			"PRIMARY ERROR: "+best.Reason, time.Now().UTC(),
		)
	}
	if best.Mutated && best.Result == "switched" {
		appendAutomationRecoveryIncidentStage(
			incident, "incident", "recovered",
			"method=emergency-replacement; total_downtime_since_detection="+time.Since(incident.StartedAt).Round(time.Second).String(),
			time.Now().UTC(),
		)
	} else if bestErr != nil {
		appendAutomationRecoveryIncidentStage(
			incident, "incident", "failed",
			"PRIMARY ERROR: "+best.Reason+"; total_elapsed="+time.Since(incident.StartedAt).Round(time.Second).String(),
			time.Now().UTC(),
		)
	}
	result := automationHealthResult{State: best.Result, Reason: best.Reason, Mutated: best.Mutated}
	return recordAndReturnHealth(result, bestErr)
}

func init() {
	if len(os.Args) < 2 || os.Args[1] != "automation-health-watch" {
		return
	}
	a := &app{cfg: automationCLIConfig(), sem: make(chan struct{}, 1)}
	ran, result, err := a.runScheduledAutomationHealthWatch(context.Background())
	if !ran {
		fmt.Println("RESULT=not_due")
		fmt.Println("REASON=VPN health watchdog is not due yet")
		fmt.Println("MUTATION=NONE")
		os.Exit(0)
	}
	fmt.Printf("RESULT=%s\n", sanitizeAutomationReason(result.State))
	fmt.Printf("REASON=%s\n", sanitizeAutomationReason(result.Reason))
	if result.Mutated {
		fmt.Println("MUTATION=APPLIED")
	} else {
		fmt.Println("MUTATION=NONE")
	}
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
