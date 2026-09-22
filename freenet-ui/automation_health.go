package main

import (
	"bytes"
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
	"time"
)

const (
	automationHealthHealthy   = "healthy"
	automationHealthFailed    = "failed"
	automationHealthUncertain = "uncertain"
	automationHealthCritical  = "critical"

	automationHealthProbeTimeout = 12 * time.Second
	automationHealthConfirmDelay = 2 * time.Second
	automationHealthRunTimeout   = 35 * time.Second
)

type automationHealthResult struct {
	State   string
	Reason  string
	Mutated bool
}

type automationHealthProbe struct {
	State  string
	Reason string
}

var automationEndpointUpdateCommand = func(a *app, ctx context.Context) ([]byte, error) {
	return runCommand(ctx, a.cfg.VPNPath, "update")
}

var automationEndpointPostProbe = func(a *app, ctx context.Context) automationHealthProbe {
	return a.probeAutomationCurrentVPN(ctx)
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
	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > 10*time.Minute {
		_ = os.RemoveAll(path)
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
	outbound, activeEndpoint, ok := readBestServerActiveOutbound(a.cfg.OutPath)
	currentEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	if !ok || currentEndpoint == "" || !endpointsEqual(activeEndpoint, currentEndpoint) {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Текущий VPN не удалось безопасно определить."}
	}
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

	env := append(os.Environ(), "XRAY_LOCATION_ASSET="+a.geoDataAssetDir())
	testCtx, cancelTest := context.WithTimeout(ctx, 4*time.Second)
	testCmd := exec.CommandContext(testCtx, xrayPath, "run", "-test", "-confdir", tmpDir)
	testCmd.Env = env
	testCmd.Stdout = io.Discard
	testCmd.Stderr = io.Discard
	testErr := testCmd.Run()
	cancelTest()
	if testErr != nil {
		return automationHealthProbe{State: automationHealthUncertain, Reason: "Конфигурация текущего VPN не прошла локальную проверку."}
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	cmd := exec.CommandContext(runCtx, xrayPath, "run", "-confdir", tmpDir)
	cmd.Env = env
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
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
	probeCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	output, err := exec.CommandContext(probeCtx, curlPath,
		"--socks5-hostname", socks,
		"-sS", "--connect-timeout", "3", "--max-time", "4",
		"-o", "/dev/null", "-w", "%{http_code}\t%{time_pretransfer}\t%{time_starttransfer}", bestServerQualityProbeURL,
	).Output()
	cancel()
	if err != nil {
		return automationHealthProbe{State: automationHealthFailed, Reason: "Текущий VPN не даёт доступ к интернету."}
	}
	applicationMS, ok := parseBestServerHTTPResponseMS(string(output))
	if !ok {
		return automationHealthProbe{State: automationHealthFailed, Reason: "Текущий VPN не подтвердил доступ к интернету."}
	}
	if !automationApplicationPathHealthy(applicationMS) {
		return automationHealthProbe{State: automationHealthFailed, Reason: fmt.Sprintf("Отклик текущего VPN слишком высокий: %d мс (допустимо до %d мс).", applicationMS, bestServerQualityMaxApplicationMS)}
	}
	serviceOK, serviceTotal := probeBestServerServiceReachability(ctx, curlPath, socks)
	if !automationServicePathHealthy(serviceOK, serviceTotal) {
		return automationHealthProbe{State: automationHealthFailed, Reason: fmt.Sprintf("Текущий VPN отвечает базово, но сервисные маршруты нестабильны: %d/%d.", serviceOK, serviceTotal)}
	}
	return automationHealthProbe{State: automationHealthHealthy, Reason: "Текущий VPN и сервисные маршруты работают стабильно."}
}

func automationCurrentCountry(a *app) string {
	if code := bestServerCountryCodeFromLabel(currentExactProfileLabel(a.cfg.FilterPath)); code != "" {
		return code
	}
	return strings.ToLower(strings.TrimSpace(a.status().CountryCode))
}

func (a *app) runAutomationBestEmergencyCycle(parent context.Context, settings automationSettings) (automationBestCycleResult, error) {
	if settings.Mode != automationModeBest {
		return automationBestCycleResult{Result: "same", Reason: "Автоматический поиск замены не применим к текущему режиму."}, nil
	}
	release, err := acquireAutomationBestLock()
	if err != nil {
		return automationBestCycleResult{Result: "busy", Reason: "Поиск замены пропущен: другая AUTO VPN операция уже выполняется."}, nil
	}
	defer release()

	ctx, cancel := context.WithTimeout(parent, automationBestTimeout)
	defer cancel()
	currentCountry := automationCurrentCountry(a)
	if currentCountry == "" {
		reason := "Страна текущего VPN не подтверждена; автоматическая замена отменена."
		writeAutomationStateV2("uncertain", reason, "no", false)
		appendAutomationHistoryV2("uncertain", reason)
		return automationBestCycleResult{Result: "uncertain", Reason: reason}, nil
	}
	candidates, err := a.scanBestServerForeignForAutomation(ctx, settings, currentCountry)
	if err != nil {
		reason := "Не удалось завершить безопасный поиск проверенной замены; текущие настройки сохранены."
		writeAutomationStateV2("failed", reason, "no", false)
		appendAutomationHistoryV2("failed", reason)
		return automationBestCycleResult{Result: "failed", Reason: reason}, err
	}
	candidate, ok := bestAutomationCandidate(candidates)
	if !ok {
		reason := "Подходящей полностью проверенной замены сейчас нет."
		writeAutomationStateV2("same", reason, "no", false)
		appendAutomationHistoryV2("same", reason)
		return automationBestCycleResult{Result: "same", Reason: reason}, nil
	}
	if !settings.AutoApply {
		reason := "Найдена проверенная замена, но автоматическое применение выключено."
		writeAutomationStateV2("candidate", reason, "no", false)
		appendAutomationHistoryV2("candidate", reason)
		return automationBestCycleResult{Result: "candidate", Reason: reason, ProfileID: candidate.ID}, nil
	}

	status, applied := a.executeProviderProfileApply(networkApplyRequest{Operation: "provider", ProfileID: candidate.ID, Confirm: true})
	if status < 200 || status >= 300 || !applied.Success {
		reason := "Проверенная замена VPN не применена: " + strings.TrimSpace(applied.Error)
		rollback := applied.RollbackState
		if rollback == "" {
			rollback = "unknown"
		}
		writeAutomationStateV2("failed", reason, rollback, false)
		appendAutomationHistoryV2("failed", reason+"; rollback="+rollback)
		return automationBestCycleResult{Result: "failed", Reason: reason, RollbackState: rollback, ProfileID: candidate.ID}, errors.New("AUTO VPN emergency apply failed")
	}
	reason := "Текущий VPN заменён на проверенный вариант: " + candidate.Name
	writeAutomationStateV2("switched", reason, "yes", true)
	appendAutomationHistoryV2("success", reason)
	return automationBestCycleResult{Result: "switched", Reason: reason, Mutated: true, RollbackState: applied.RollbackState, ProfileID: candidate.ID}, nil
}

func (a *app) runAutomationEndpointEmergency(parent context.Context, settings automationSettings) (automationHealthResult, error) {
	if settings.Mode != automationModeEndpoint {
		return automationHealthResult{State: automationHealthUncertain, Reason: "Восстановление текущего VPN не применимо к текущему режиму."}, nil
	}
	if !settings.AutoApply {
		return automationHealthResult{State: automationHealthCritical, Reason: "Текущий VPN недоступен, но автоматическое восстановление выключено."}, nil
	}

	before, _ := os.ReadFile(a.cfg.OutPath)
	ctx, cancel := context.WithTimeout(parent, 150*time.Second)
	defer cancel()
	out, err := automationEndpointUpdateCommand(a, ctx)
	if err != nil {
		if automationEndpointUpdateBusy(out) {
			return automationHealthResult{State: automationHealthUncertain, Reason: "Обновление текущего VPN уже выполняется другой операцией; следующая mutation отменена."}, errAutomationBusy
		}
		if automationEndpointRollbackUnknown(out) {
			return automationHealthResult{State: automationHealthCritical, Reason: "Штатное обновление текущего VPN завершилось ошибкой; rollback failed or unknown."}, err
		}
		return automationHealthResult{State: automationHealthCritical, Reason: "Штатное обновление текущего VPN не завершено; текущая конфигурация не считается восстановленной."}, err
	}

	after, _ := os.ReadFile(a.cfg.OutPath)
	mutated := len(before) > 0 && len(after) > 0 && !bytes.Equal(before, after)
	probeCtx, cancelProbe := context.WithTimeout(parent, automationHealthProbeTimeout)
	probe := automationEndpointPostProbe(a, probeCtx)
	cancelProbe()
	switch probe.State {
	case automationHealthHealthy:
		reason := "Текущий VPN восстановлен штатным обновлением профиля и подтверждён проверкой доступа через VPN."
		return automationHealthResult{State: automationHealthHealthy, Reason: reason, Mutated: mutated}, nil
	case automationHealthUncertain:
		return automationHealthResult{State: automationHealthUncertain, Reason: "После штатного обновления состояние VPN не удалось однозначно подтвердить; следующая mutation отменена.", Mutated: mutated}, nil
	default:
		return automationHealthResult{State: automationHealthFailed, Reason: "Штатное обновление профиля выполнено, но доступ через текущий VPN не восстановился.", Mutated: mutated}, nil
	}
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

func (a *app) runAutomationHealthWatch(parent context.Context) (automationHealthResult, error) {
	settings := readAutomationSettings(a.cfg.ConfigPath)
	if !settings.Enabled {
		return recordAndReturnHealth(automationHealthResult{State: "disabled", Reason: "AUTO VPN выключен."}, nil)
	}
	release, err := acquireAutomationHealthLock()
	if err != nil {
		return recordAndReturnHealth(automationHealthResult{State: "busy", Reason: "Проверка пропущена: предыдущая AUTO VPN операция ещё выполняется."}, nil)
	}
	defer release()

	probeCtx, cancel := context.WithTimeout(parent, automationHealthRunTimeout)
	first := a.probeAutomationCurrentVPN(probeCtx)
	if first.State == automationHealthHealthy || first.State == automationHealthUncertain {
		cancel()
		return recordAndReturnHealth(automationHealthResult{State: first.State, Reason: first.Reason}, nil)
	}
	appendAutomationRecoveryStage("detect", first.State, first.Reason)
	wanHealthy := probeAutomationWAN(probeCtx)
	if !wanHealthy {
		cancel()
		decision := classifyAutomationHealth(first, false, automationHealthProbe{})
		appendAutomationRecoveryStage("wan", decision.State, decision.Reason)
		return recordAndReturnHealth(automationHealthResult{State: decision.State, Reason: decision.Reason}, nil)
	}
	appendAutomationRecoveryStage("wan", "healthy", "Обычный интернет подтверждён; AUTO VPN продолжает восстановление.")
	select {
	case <-time.After(automationHealthConfirmDelay):
	case <-probeCtx.Done():
		cancel()
		reason := "Повторная проверка VPN не успела завершиться; изменений нет."
		appendAutomationRecoveryStage("confirm", automationHealthUncertain, reason)
		return recordAndReturnHealth(automationHealthResult{State: automationHealthUncertain, Reason: reason}, nil)
	}
	second := a.probeAutomationCurrentVPN(probeCtx)
	cancel()
	decision := classifyAutomationHealth(first, true, second)
	appendAutomationRecoveryStage("confirm", decision.State, decision.Reason)
	if decision.State != automationHealthCritical {
		return recordAndReturnHealth(automationHealthResult{State: decision.State, Reason: decision.Reason}, nil)
	}

	// Magic AUTO VPN recovery order: first try a fresh endpoint for the exact
	// current VPN. Only when that cannot restore service do we search a fully
	// validated replacement inside the user's allowed geography.
	endpointSettings := settings
	endpointSettings.Mode = automationModeEndpoint
	endpointSettings.AutoApply = true
	appendAutomationRecoveryStage("endpoint_refresh", "start", "Пробуем штатно обновить текущий VPN перед заменой сервера.")
	endpointResult, endpointErr := a.runAutomationEndpointEmergency(parent, endpointSettings)
	appendAutomationRecoveryStage("endpoint_refresh", endpointResult.State, endpointResult.Reason)
	if endpointErr == nil && endpointResult.State == automationHealthHealthy {
		appendAutomationRecoveryStage("post_check", "success", endpointResult.Reason)
		return recordAndReturnHealth(endpointResult, nil)
	}
	if endpointResult.State == automationHealthUncertain {
		appendAutomationRecoveryStage("post_check", automationHealthUncertain, endpointResult.Reason)
		return recordAndReturnHealth(endpointResult, endpointErr)
	}
	if endpointErr != nil {
		lower := strings.ToLower(endpointResult.Reason)
		if strings.Contains(lower, "rollback failed") || strings.Contains(lower, "rollback unknown") || strings.Contains(lower, "rollback=failed") || strings.Contains(lower, "rollback=unknown") {
			endpointResult.State = automationHealthCritical
			appendAutomationRecoveryStage("rollback", "failed", endpointResult.Reason)
			return recordAndReturnHealth(endpointResult, endpointErr)
		}
	}

	bestSettings := settings
	bestSettings.Mode = automationModeBest
	bestSettings.Policy = automationPolicyDegraded
	bestSettings.AutoApply = true
	appendAutomationRecoveryStage("candidate_selection", "start", "Endpoint refresh не восстановил VPN; ищем полностью проверенную замену.")
	best, bestErr := a.runAutomationBestEmergencyCycle(parent, bestSettings)
	appendAutomationRecoveryStage("apply", best.Result, best.Reason)
	if best.RollbackState != "" && best.RollbackState != "NOT_NEEDED" && best.RollbackState != "yes" {
		appendAutomationRecoveryStage("rollback", best.RollbackState, best.Reason)
	}
	result := automationHealthResult{State: best.Result, Reason: best.Reason, Mutated: best.Mutated}
	return recordAndReturnHealth(result, bestErr)
}

func init() {
	if len(os.Args) < 2 || os.Args[1] != "automation-health-watch" {
		return
	}
	a := &app{cfg: automationCLIConfig(), sem: make(chan struct{}, 1)}
	result, err := a.runAutomationHealthWatch(context.Background())
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
