package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	automationHealthHealthy   = "healthy"
	automationHealthFailed    = "failed"
	automationHealthUncertain = "uncertain"
	automationHealthCritical  = "critical"

	automationHealthProbeTimeout      = 20 * time.Second
	automationHealthRunTimeout        = 55 * time.Second
	automationEndpointRecoveryTimeout = 20 * time.Second

	// Reachable quality degradation must be repeated before Full AUTO spends
	// bandwidth on a heavy Best Server comparison. A completed optimization
	// attempt is also cooled down independently from the existing post-switch
	// cooldown, so a noisy line cannot trigger repeated Top-3 scans.
	// 300 ms is a *quality* hint, never evidence of a VPN outage. Two or
	// more independent degraded observations within twenty minutes are required
	// even when a single latency sample is extreme. A good sample clears them.
	automationQualityStrikeWindow          = 20 * time.Minute
	automationQualityStrikeThreshold       = 4
	automationQualityMinObservations       = 2
	automationQualityOptimizationCooldown  = 1 * time.Hour
	automationQualitySevereCooldown        = 15 * time.Minute
	automationQualityLatencyMildMS         = 300
	automationQualityLatencyStrongMS       = 420
	automationQualityLatencySevereMS       = 650

	// Pending rollback is a latched incident, not a new outage every 30 s.
	automationRollbackGuardJournalInterval = 15 * time.Minute
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
	CurrentIdentity string
	ServiceOKMask   uint8
	ServiceBadMask  uint8
	ServiceComplete bool
	MediaComplete   bool
	MediaDegraded   bool
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
	case ms >= automationQualityLatencySevereMS:
		return 3
	case ms >= automationQualityLatencyStrongMS:
		return 2
	case ms >= automationQualityLatencyMildMS:
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
	// A successful generic VPN HTTPS request proves the VPN path is alive.
	// Instagram, Telegram Web, WhatsApp Web and YouTube can independently
	// reject a proxy due to geoblocking, method policy or ISP DIRECT routing.
	// They are QoE signals; even 0/4 cannot declare a global VPN outage.
	latencyPoints := automationApplicationQualityPoints(applicationMS)
	mediaImpaired := serviceTotal >= 3 && serviceOK < serviceTotal
	if latencyPoints == 0 && !mediaImpaired {
		return automationHealthProbe{State: automationHealthHealthy, Reason: "Текущий VPN и сервисные маршруты работают стабильно."}
	}

	if latencyPoints == 0 && mediaImpaired {
		return automationHealthProbe{
			State: automationHealthUncertain,
			Reason: fmt.Sprintf("VPN отвечает, но качество соединения ухудшено: отдельные сервисы %d/%d. Это не отказ общего VPN; AUTO VPN проверяет, повторяется ли проблема перед сравнением серверов.", serviceOK, serviceTotal),
			QualityDegraded: true,
			QualityPoints: 0,
		}
	}
	reason := fmt.Sprintf("VPN отвечает, но качество соединения ухудшено: отклик %d мс, сервисы %d/%d. AUTO VPN накапливает подтверждение деградации.", applicationMS, serviceOK, serviceTotal)
	return automationHealthProbe{
		State: automationHealthUncertain,
		Reason: reason,
		QualityDegraded: true,
		QualityPoints: latencyPoints,
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
	samples, _ := strconv.Atoi(strings.TrimSpace(state["QUALITY_DEGRADED_SAMPLES"]))
	if count < 0 || count > 100 {
		count = 0
	}
	if samples < 0 || samples > 100 {
		samples = 0
	}
	start := parseStamp("QUALITY_DEGRADED_SINCE")
	lastOptimization := parseStamp("QUALITY_OPTIMIZATION_LAST")

	if points <= 0 {
		// A confirmed healthy observation breaks the degradation sequence.
		// Runtime v0.5.0 evidence showed that carrying mild strikes across an
		// intervening healthy sample caused expensive Top-3 scans for transient
		// 3/4 or short latency spikes. Keep the optimization cooldown, but clear
		// only the pending quality evidence.
		if count != 0 || samples != 0 || !start.IsZero() {
			updates["QUALITY_DEGRADED_COUNT"] = "0"
			updates["QUALITY_DEGRADED_SAMPLES"] = "0"
			updates["QUALITY_DEGRADED_SINCE"] = ""
		}
		return updates, false
	}

	// An unreadable cooldown cannot authorize another expensive comparison.
	// Healthy observations above may still clear pending evidence, but must
	// leave this unknown reservation untouched.
	if raw := strings.TrimSpace(state["QUALITY_OPTIMIZATION_LAST"]); raw != "" {
		if _, err := time.Parse(time.RFC3339, raw); err != nil {
			return updates, false
		}
	}

	if start.IsZero() || now.Before(start) || now.Sub(start) > automationQualityStrikeWindow {
		start = now
		count = 0
		samples = 0
	}
	count += points
	if count > automationQualityStrikeThreshold {
		count = automationQualityStrikeThreshold
	}
	samples++
	if samples > automationQualityMinObservations {
		samples = automationQualityMinObservations
	}
	updates["QUALITY_DEGRADED_COUNT"] = strconv.Itoa(count)
	updates["QUALITY_DEGRADED_SAMPLES"] = strconv.Itoa(samples)
	updates["QUALITY_DEGRADED_SINCE"] = start.UTC().Format(time.RFC3339)

	cooldown := automationQualityOptimizationCooldown
	// Severity uses the three-point scale; the four-point strike threshold
	// must not accidentally lengthen severe quality cooldown to one hour.
	if points >= 3 {
		cooldown = automationQualitySevereCooldown
	}
	if !lastOptimization.IsZero() && now.Before(lastOptimization.Add(cooldown)) {
		return updates, false
	}
	// No one-off RTT spike may launch a heavy Top-3 scan.
	if count < automationQualityStrikeThreshold || samples < automationQualityMinObservations {
		return updates, false
	}

	updates["QUALITY_DEGRADED_COUNT"] = "0"
	updates["QUALITY_DEGRADED_SAMPLES"] = "0"
	updates["QUALITY_DEGRADED_SINCE"] = ""
	updates["QUALITY_OPTIMIZATION_LAST"] = now.UTC().Format(time.RFC3339)
	return updates, true
}

func automationQualityOptimizationDue(now time.Time, points int) bool {
	due := false
	err := v3UpdateState(func(state map[string]string) map[string]string {
		updates, plannedDue := automationQualityOptimizationPlan(state, now, points)
		due = plannedDue
		return updates
	})
	return err == nil && due
}

func automationQualityOptimizationStartReason(probe automationHealthProbe) string {
	trigger := strings.TrimSpace(probe.Reason)
	if trigger == "" {
		trigger = "Текущая quality-проверка не содержит подробностей."
	}
	if probe.QualityPoints >= 3 {
		return "Повторная выраженная деградация качества подтверждена минимум двумя наблюдениями. Триггер: " + trigger + " Сравниваем текущий VPN с fully measured Top-3."
	}
	return "Устойчивая деградация качества подтверждена несколькими наблюдениями. Триггер: " + trigger + " Сравниваем текущий VPN с fully measured Top-3."
}

// A fixed-IP response is not evidence that VPN-routed applications work.
// Failure of BOTH independent named HTTPS origins is application-path
// failure even if fixed-IP transport still responds. A recovery is allowed
// only after a second complete failed observation, verified DIRECT WAN,
// transaction fences and a separately accepted replacement. Any incomplete
// sample or cancelled parent remains UNKNOWN (no mutation).
func classifyAutomationApplicationFailure(transportOK, namedFailureComplete bool) automationHealthProbe {
	if !namedFailureComplete {
		return automationHealthProbe{
			State: automationHealthUncertain,
			Reason: "Независимые HTTPS/DNS проверки VPN завершены не полностью; без однозначного результата AUTO VPN сохраняет текущее подключение.",
		}
	}
	if transportOK {
		return automationHealthProbe{
			State: automationHealthFailed,
			Reason: "VPN отвечает на IP-запрос, но оба независимых HTTPS-адреса через него недоступны. Это подтверждённая неисправность прикладного VPN-пути; AUTO требует второй независимый FAIL, рабочий DIRECT и проверенную замену.",
		}
	}
	return automationHealthProbe{
		State: automationHealthFailed,
		Reason: "Текущий VPN не подтвердил доступ ни через оба независимых HTTPS/DNS-адреса, ни через IP-транспорт.",
	}
}

// Count attempted independent named-origin probes, not merely failed curl
// calls. A cancelled/partial probe is UNKNOWN and cannot authorize recovery.
func probeAutomationNamedHTTPSWith(ctx context.Context, curlPath, socks string, runner bestServerHTTPProbeRunner) (int, bool, bool) {
	if runner == nil || len(bestServerApplicationProbeURLs) < 2 {
		return 0, false, false
	}
	attempts := 0
	recordingRunner := func(ctx context.Context, curlPath, socks, target string, timeout time.Duration) (int, bool) {
		attempts++
		return runner(ctx, curlPath, socks, target, timeout)
	}
	ms, _, ok := probeBestServerHTTPAnyWith(ctx, curlPath, socks, bestServerApplicationProbeURLs, bestServerApplicationProbePerTargetTimeout, recordingRunner)
	completeFailure := !ok && attempts == len(bestServerApplicationProbeURLs) && ctx.Err() == nil
	return ms, ok, completeFailure
}

func probeAutomationNamedHTTPS(ctx context.Context, curlPath, socks string) (int, bool, bool) {
	return probeAutomationNamedHTTPSWith(ctx, curlPath, socks, runBestServerHTTPProbeURL)
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
	currentFilter, filterErr := os.ReadFile(a.cfg.FilterPath)
	currentIdentity := ""
	if filterErr == nil && strings.TrimSpace(string(currentFilter)) != "" {
		currentIdentity = bestServerCurrentQualityIdentityHash(currentEndpoint, string(currentFilter))
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
	applicationMS, ok, namedFailureComplete := probeAutomationNamedHTTPSDetailed(ctx, curlPath, socks)
	if !ok {
		if ctx.Err() != nil {
			return automationHealthProbe{State: automationHealthUncertain, Reason: "VPN health probe прерван до завершения HTTPS/DNS проверок; AUTO mutation запрещена."}
		}
		transport := runAutomationVPNHTTPProbe(ctx, curlPath, socks, bestServerTransportProbeURL, automationHealthTransportTimeout, false)
		if ctx.Err() != nil {
			return automationHealthProbe{State: automationHealthUncertain, Reason: "VPN health probe истёк до подтверждения транспортного состояния; AUTO mutation запрещена."}
		}
		// A timeout or an inconclusive TCP/TLS result is not evidence of
		// a dead VPN, even when both named-origin checks also failed.
		if !transport.Reachable && !transport.ConclusiveFailure {
			namedFailureComplete = false
		}
		return classifyAutomationApplicationFailure(transport.Reachable, namedFailureComplete)
	}
	if currentIdentity == "" {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Не удалось подтвердить профиль текущего VPN для проверки качества."}
	}
	services := probeBestServerServiceReachabilityDetailed(ctx, curlPath, socks)
	if ctx.Err() != nil {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Сервисная проверка VPN прервана; текущее подключение сохраняется."}
	}
	// HTTP authentication/anti-bot restrictions are not video failures and
	// must not block an independent successful data transfer measurement.
	finalEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	finalFilter, filterErr := os.ReadFile(a.cfg.FilterPath)
	if ctx.Err() != nil || filterErr != nil || finalEndpoint == "" ||
		bestServerCurrentQualityIdentityHash(finalEndpoint, string(finalFilter)) != currentIdentity {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Текущий VPN изменился во время проверки; AUTO VPN ожидает новое измерение."}
	}
	// Count only actually verified service responses. Unknown HTTP statuses
	// are neither GOOD nor BAD; they cannot fabricate a false degradation.
	knownServices := bits.OnesCount8(services.OKMask | services.BadMask)
	probe := classifyAutomationReachableQuality(applicationMS, bits.OnesCount8(services.OKMask), knownServices)
	probe.CurrentIdentity = currentIdentity
	probe.ServiceOKMask = services.OKMask
	probe.ServiceBadMask = services.BadMask
	probe.ServiceComplete = services.Complete

	// At the regular health cadence, prefer lightweight availability checks.
	// Complete sequential transfers run at most once per media-budget interval.
	// Final provider admission and explicit diagnostics never carry this limit.
	if !automationMediaProbeAllowed(ctx) {
		if !services.Complete && probe.State == automationHealthHealthy {
			probe.Reason = "VPN отвечает; не все сервисные HTTP-ответы однозначны. Фоновый тест медиапередачи выполняется по отдельному расписанию."
		}
		return probe
	}
	media := probeAutomationMediaSegments(ctx, curlPath, socks)
	if ctx.Err() != nil {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Проверка медиапотока не завершена; текущее соединение сохраняется."}
	}
	finalEndpoint = readBestServerCurrentEndpoint(a.cfg.OutPath)
	finalFilter, filterErr = os.ReadFile(a.cfg.FilterPath)
	if filterErr != nil || finalEndpoint == "" ||
		bestServerCurrentQualityIdentityHash(finalEndpoint, string(finalFilter)) != currentIdentity {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Профиль VPN изменился при проверке медиапотока; результат отброшен."}
	}
	probe = classifyAutomationMediaQuality(probe, media)
	if !services.Complete && probe.State == automationHealthHealthy {
		probe.Reason = "VPN отвечает; проверка передачи сегментов выполнена, но часть сервисных HTTP-проверок не даёт достоверного ответа. Автоматическая смена без подтверждённой деградации не требуется."
	}
	return probe
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

// Complete bounded media over the exact configured VPN is evidence of a
// reachable but degraded connection. It must not be confused with an unknown
// Xray/rollback state or require a healthy video stream before AutoVPN can
// select a replacement.
func automationExactDegradedMedia(a *app, probe automationHealthProbe) bool {
	if a == nil || probe.State != automationHealthUncertain ||
		!probe.QualityDegraded || !probe.MediaComplete || !probe.MediaDegraded ||
		probe.ServiceOKMask == 0 || !validAutomationQualityIdentity(probe.CurrentIdentity) {
		return false
	}
	endpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	filter := readBestServerCurrentFilter(a.cfg.FilterPath)
	return endpoint != "" && strings.TrimSpace(filter) != "" &&
		bestServerCurrentQualityIdentityHash(endpoint, filter) == probe.CurrentIdentity
}

// beta.47 could record UNKNOWN for a writer semaphore conflict that happened
// BEFORE provider admission, while self-update owned the writer. Reconcile
// only that exact old incident, only after completed read-only media evidence
// for the same live VPN and with no pending checkpoint or writer/update lock.
// A genuine UNKNOWN/FAILED rollback, missing evidence or new operation is STOP.
func (a *app) reconcileLegacyBusyBeforeMutation(probe automationHealthProbe) bool {
	if !automationExactDegradedMedia(a, probe) || vpnTransactionPending() ||
		vpnMutationLockState(a.cfg.LockPath, selfUpdateProcRoot()) != "absent" ||
		a.updateLockHeld() {
		return false
	}
	updating, _, _ := a.currentUpdateActivity()
	if updating || automationPendingPostUpdateTarget(a) == "" {
		return false
	}
	state := parseAutomationState(automationStatePath())
	if state["LAST_RESULT"] != "failed" ||
		state["LAST_REASON"] != "Подтверждённый VPN не применён: another FreeNet operation is already running" ||
		!strings.EqualFold(state["ROLLBACK_READY"], "unknown") ||
		!strings.EqualFold(state["MUTATION_BLOCKED"], "yes") {
		return false
	}
	setAutomationMutationBlocked(false)
	if automationMutationBlockedState() {
		return false // persistence failed or checkpoint appeared
	}
	appendAutomationRecoveryStage("rollback_guard", "reconciled_pre_mutation_busy",
		"Конфликт с обновлением произошёл до изменения VPN; текущий профиль подтвердил HTTPS и медиапередачу. Ложная блокировка снята.")
	return true
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
	// Quality regression is precisely why AUTO exists. Once the current VPN
	// has provided two complete slow media segments over its unchanged
	// identity, acknowledge a successful FreeNet update and CONTINUE into
	// the normal repeated-quality comparison; do not label the VPN healthy.
	if automationExactDegradedMedia(a, first) && !automationMutationBlockedState() &&
		!a.updateLockHeld() {
		updating, _, _ := a.currentUpdateActivity()
		if !updating {
			setAutomationPostUpdateAck(target)
			if automationPendingPostUpdateTarget(a) == "" {
				appendAutomationRecoveryStage("post_update_guard", "degraded_allowed",
					"FreeNet обновлён; VPN доступен, но медиапередача медленная. AUTO продолжает сравнение серверов.")
				return automationHealthResult{}, false
			}
		}
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

// Each watchdog pass must still publish current health, but an unchanged
// protected rollback marker cannot produce thousands of duplicate events.
// A new guard type and the first observation after restart are reported.
var automationRollbackGuardJournal struct {
	sync.Mutex
	lastTime time.Time
	lastKind string
}

func automationRollbackGuardShouldJournal(kind string, now time.Time) bool {
	automationRollbackGuardJournal.Lock()
	defer automationRollbackGuardJournal.Unlock()
	if automationRollbackGuardJournal.lastKind != kind ||
		automationRollbackGuardJournal.lastTime.IsZero() ||
		now.Before(automationRollbackGuardJournal.lastTime) ||
		now.Sub(automationRollbackGuardJournal.lastTime) >= automationRollbackGuardJournalInterval {
		automationRollbackGuardJournal.lastKind = kind
		automationRollbackGuardJournal.lastTime = now
		return true
	}
	return false
}

func automationRollbackGuardResult(a *app, first automationHealthProbe) (automationHealthResult, bool) {
	if !automationMutationBlockedState() {
		return automationHealthResult{}, false
	}
	// A durable provider checkpoint contains the pre-apply snapshot, but
	// proves neither COMMIT nor ROLLBACK. A single healthy probe cannot safely
	// discard that evidence, and a failed probe cannot assume on-disk/live
	// Xray identities match. Only explicit verified reconciliation may clear it.
	if vpnTransactionPending() {
		reason := "Постоянный checkpoint VPN-транзакции не закрыт. AUTO mutation заблокирована до проверенного read-only восстановления исходного состояния."
		if automationRollbackGuardShouldJournal("blocked_pending", time.Now().UTC()) {
			appendAutomationRecoveryStage("rollback_guard", "blocked_pending", reason)
		}
		return automationHealthResult{State: automationHealthUncertain, Reason: reason}, true
	}
	if a.reconcileLegacyBusyBeforeMutation(first) {
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
	if automationRollbackGuardShouldJournal("blocked", time.Now().UTC()) {
		appendAutomationRecoveryStage("rollback_guard", "blocked", reason)
	}
	return automationHealthResult{State: automationHealthUncertain, Reason: reason}, true
}

func (a *app) runAutomationHealthWatch(parent context.Context) (automationHealthResult, error) {
	settings := readAutomationSettings(a.cfg.ConfigPath)
	if !settings.Enabled {
		return recordAndReturnHealth(automationHealthResult{State: "disabled", Reason: "AUTO VPN выключен."}, nil)
	}
	_ = v3WriteState(map[string]string{"HEALTH_SCHEDULE_LAST": time.Now().UTC().Format(time.RFC3339)})
	// A complete candidate scan already consumes the isolated-Xray and network
	// budget. Do not launch competing health downloads while that comparison is
	// running. Dead-owner locks are reclaimed by the next Best scan.
	bestLockPath := automationBestLockPath()
	if info, err := os.Stat(bestLockPath); err == nil && !automationBestLockReclaimable(bestLockPath, info) {
		return recordAndReturnHealth(automationHealthResult{State: "busy", Reason: "AUTO VPN уже сравнивает серверы; новая нагрузочная проверка пропущена."}, nil)
	}

	// Read-only observation must never monopolize the AUTO health fence. Manual
	// Current VPN / Best Server diagnostics are the recovery path when a router
	// is already degraded, especially immediately after an update. Only take the
	// exclusive fence when AUTO is actually about to enter recovery/mutation.
	probeCtx, cancel := context.WithTimeout(parent, automationHealthRunTimeout)
	probeCtx = automationScheduledWatchdogMediaContext(probeCtx, time.Now().UTC())
	first := a.probeAutomationCurrentVPN(probeCtx)
	if guarded, blocked := automationRollbackGuardResult(a, first); blocked {
		cancel()
		return recordAndReturnHealth(guarded, nil)
	}
	if guarded, blocked := automationPostUpdateGuardResult(a, first); blocked {
		cancel()
		return recordAndReturnHealth(guarded, nil)
	}
	qualityProbe := first
	if settings.Mode != automationModeBest && first.State != automationHealthHealthy {
		qualityProbe = automationHealthProbe{}
	}
	qualityTrigger, qualityDue := automationReachableQualityOptimizationDue(time.Now().UTC(), qualityProbe)
	if first.State == automationHealthHealthy {
		if err := clearAutomationEmergencyCooldown(); err != nil {
			appendAutomationRecoveryStage("retry_cooldown", "clear_failed", "Не удалось очистить паузу после восстановления VPN.")
		}
		cancel()
		return recordAndReturnHealth(automationHealthResult{State: first.State, Reason: first.Reason}, nil)
	}
	if first.State == automationHealthUncertain {
		// Unknown observations do not become bad quality evidence. Latency
		// strikes retain their existing policy; the consecutive service sequence
		// is reset unless this whole identity-bound observation completed.
		cancel()
		if qualityDue {
			appendAutomationRecoveryStage("quality_optimization", "start", automationQualityOptimizationStartReason(first))
			best, bestErr := a.runAutomationQualityCycle(parent, settings, qualityTrigger)
			if best.Result == "busy" {
				// Busy is not a completed scan. Preserve the next opportunity
				// instead of consuming 10 minutes of video QoE retry cooldown.
				automationReleaseQualityOptimizationReservation(qualityTrigger)
			}
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
	if guarded, blocked := automationRollbackGuardResult(a, confirm); blocked {
		cancel()
		return recordAndReturnHealth(guarded, nil)
	}
	if guarded, blocked := automationPostUpdateGuardResult(a, confirm); blocked {
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

	// A failed bounded scan must not start another expensive scan on every
	// 30-second health tick. Read-only VPN/WAN observations continue normally.
	// Cooldown never overrides the separate rollback/mutation safety fence.
	if until, active, cooldownErr := automationEmergencyCooldownDue(a, time.Now().UTC()); cooldownErr != nil {
		reason := "Не удалось проверить сохранённую паузу аварийного восстановления; AUTO mutation остановлена, текущая конфигурация сохранена."
		appendAutomationRecoveryStage("retry_cooldown", "stopped", reason)
		return recordAndReturnHealth(automationHealthResult{State: automationHealthUncertain, Reason: reason}, cooldownErr)
	} else if active {
		reason := "VPN всё ещё недоступен; повторный тяжёлый поиск резервов отложен до " + until.Format(time.RFC3339) + ". Лёгкие проверки продолжаются."
		return recordAndReturnHealth(automationHealthResult{State: automationHealthCritical, Reason: reason}, nil)
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
		appendAutomationRecoveryIncidentStage(incident, "incident", "uncertain", "PRIMARY ERROR: "+reason, time.Now().UTC())
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
		_ = clearAutomationEmergencyCooldown()
		appendAutomationRecoveryStage("post_check", "success", endpointResult.Reason)
		appendAutomationRecoveryIncidentStage(
			incident, "incident", "recovered",
			"method=current-endpoint; recovery_cycle_elapsed="+time.Since(incident.StartedAt).Round(time.Second).String(),
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
		appendAutomationRecoveryIncidentStage(incident, "incident", "stopped", "PRIMARY ERROR: состояние rollback не подтверждено", time.Now().UTC())
		return recordAndReturnHealth(endpointResult, endpointErr)
	}

	if settings.Mode == automationModeEndpoint {
		reason := "Режим «Только текущий VPN»: endpoint текущего профиля не восстановил соединение; автоматическая смена страны или VPN-профиля запрещена."
		appendAutomationRecoveryStage("candidate_selection", "blocked", reason)
		appendAutomationRecoveryIncidentStage(incident, "candidate_selection", "blocked", reason, time.Now().UTC())
		appendAutomationRecoveryIncidentStage(incident, "incident", "failed", "PRIMARY ERROR: "+reason, time.Now().UTC())
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
		fmt.Sprintf("budget=%s; cohort_limit=%d; fresh_rtt_budget=%s; fresh_rtt_per_candidate=%s", automationEmergencyScanTimeout, automationEmergencyCandidateLimit, automationEmergencyRTTSweepTimeout, automationEmergencyRTTCandidateTimeout),
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
			"duration=%s; rtt_prefilter=%s; pool=%d; rtt_checked=%d; rtt_reachable=%d; best_rtt_ms=%d; app_checked=%d; app_reachable=%d; selected=%s",
			scan.ScanDuration, scan.RTTPrefilterDuration, scan.Total, scan.RTTChecked, scan.RTTReachable, scan.BestRTTMS, scan.Checked, scan.Reachable, selected,
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
		_ = clearAutomationEmergencyCooldown()
		appendAutomationRecoveryIncidentStage(
			incident, "incident", "recovered",
			"method=emergency-replacement; recovery_cycle_elapsed="+time.Since(incident.StartedAt).Round(time.Second).String(),
			time.Now().UTC(),
		)
	} else {
		// Even (Result=failed, err=nil) must have one terminal event.
		terminal := "failed"
		if best.Result == "uncertain" || best.Result == "busy" {
			terminal = "uncertain"
		}
		appendAutomationRecoveryIncidentStage(
			incident, "incident", terminal,
			"PRIMARY ERROR: "+best.Reason+"; total_elapsed="+time.Since(incident.StartedAt).Round(time.Second).String(),
			time.Now().UTC(),
		)
		if best.Result == "failed" && !best.Mutated && !automationMutationBlockedState() &&
			(best.RollbackState == "" || best.RollbackState == "NOT_NEEDED" || best.RollbackState == "yes" || best.RollbackState == "SUCCESS") {
			if retryAt, retryErr := recordAutomationEmergencyFailure(a, time.Now().UTC()); retryErr != nil {
				reason := "Не удалось сохранить ограничение повторных аварийных попыток; AUTO mutation остановлена. Текущие настройки сохранены."
				appendAutomationRecoveryStage("retry_cooldown", "stopped", reason)
				return recordAndReturnHealth(automationHealthResult{State: automationHealthUncertain, Reason: reason}, retryErr)
			} else {
				appendAutomationRecoveryStage("retry_cooldown", "scheduled", "Повторный тяжёлый поиск не ранее "+retryAt.Format(time.RFC3339))
			}
		}
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
