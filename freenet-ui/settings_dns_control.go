package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	settingsDNSApplyPathDefault   = "/opt/lib/freenet/settings_dns_apply.sh"
	settingsDNSRestorePathDefault = "/opt/lib/freenet/settings_dns_restore.sh"
)

//go:embed settings_dns_apply.sh
var settingsDNSApplyScript []byte

//go:embed settings_dns_restore.sh
var settingsDNSRestoreScript []byte

type settingsDNSControlResponse struct {
	Success           bool                        `json:"success"`
	Applied           bool                        `json:"applied,omitempty"`
	Mode              string                      `json:"mode"`
	ActiveMode        string                      `json:"active_mode"`
	DirectProvider    string                      `json:"direct_provider"`
	VPNProvider       string                      `json:"vpn_provider"`
	ActiveDirect      string                      `json:"active_direct_provider,omitempty"`
	ActiveVPN         string                      `json:"active_vpn_provider,omitempty"`
	DirectOptions     []settingsDNSProviderOption `json:"direct_options"`
	VPNOptions        []settingsDNSProviderOption `json:"vpn_options"`
	RuntimeState      string                      `json:"runtime_state,omitempty"`
	SplitSupported    bool                        `json:"split_supported"`
	ApplySupported    bool                        `json:"apply_supported"`
	Message           string                      `json:"message,omitempty"`
	Warning           string                      `json:"warning,omitempty"`
	PrimaryError      string                      `json:"primary_error,omitempty"`
	RollbackState     string                      `json:"rollback_state,omitempty"`
	Error             string                      `json:"error,omitempty"`
}

type settingsDNSControlRequest struct {
	Mode           string `json:"mode"`
	DirectProvider string `json:"direct_provider"`
	VPNProvider    string `json:"vpn_provider"`
	Confirm        bool   `json:"confirm"`
}

func registerSettingsDNSControlAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/settings-v3/dns/control", a.requireAuth(a.handleSettingsDNSControlGet))
	mux.HandleFunc("POST /api/settings-v3/dns/control", a.requireAuth(a.handleSettingsDNSControlPost))
	mux.HandleFunc("GET /api/settings-v3/assets/dns-ui.js", serveSettingsDNSUIAsset)
}

func settingsDNSRuntimeState() (direct, vpn, state string) {
	data, err := os.ReadFile(filepath.Join(settingsDNSConfigDir(), "02_dns.json"))
	if err != nil {
		return "", "", "unknown"
	}
	var cfg settingsDNSConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", "", "unknown"
	}
	var directAddress, vpnAddress string
	var directSeen, vpnSeen bool
	for _, server := range cfg.DNS.Servers {
		address := strings.TrimSpace(server.Address)
		switch server.Tag {
		case "dns-direct":
			if address == "" || (directAddress != "" && directAddress != address) {
				return "", "", "unknown"
			}
			directAddress, directSeen = address, true
		case "dns-vless":
			if address == "" || (vpnAddress != "" && vpnAddress != address) {
				return "", "", "unknown"
			}
			vpnAddress, vpnSeen = address, true
		}
	}
	if !directSeen || !vpnSeen {
		return "", "", "unknown"
	}
	direct = settingsDNSProviderFromEndpoint(directAddress)
	vpn = settingsDNSProviderFromEndpoint(vpnAddress)
	if direct != "" && vpn != "" {
		return direct, vpn, "accepted"
	}
	if directAddress == "77.88.8.8" && vpnAddress == "https://8.8.8.8/dns-query" {
		return "", "", "legacy"
	}
	return "", "", "unknown"
}

func settingsDNSControlSnapshot(configPath string) settingsDNSControlResponse {
	_, activeMode := readNetworkProfileConfig(configPath)
	direct := settingsDNSDesiredProvider(configPath, "SPLIT_DIRECT_DNS_PROVIDER", settingsDNSDirectProviderYandex)
	vpn := settingsDNSDesiredProvider(configPath, "SPLIT_VPN_DNS_PROVIDER", settingsDNSVPNProviderGoogle)
	activeDirect, activeVPN, runtimeState := settingsDNSRuntimeState()
	splitSupported := splitDNSSelectionError("xkeen") == nil
	response := settingsDNSControlResponse{
		Success: true,
		Mode: activeMode,
		ActiveMode: activeMode,
		DirectProvider: direct,
		VPNProvider: vpn,
		ActiveDirect: activeDirect,
		ActiveVPN: activeVPN,
		DirectOptions: settingsDNSProviderOptions(),
		VPNOptions: settingsDNSProviderOptions(),
		RuntimeState: runtimeState,
		SplitSupported: splitSupported,
		ApplySupported: activeMode != "xkeen" || runtimeState != "unknown",
	}
	if activeMode == "xkeen" {
		switch runtimeState {
		case "legacy":
			response.Warning = "Используется прежняя resolver-схема. Явное сохранение безопасно переведёт её на выбранные DoH resolver-ы."
		case "unknown":
			response.Warning = "Активную Split DNS resolver-схему нельзя однозначно классифицировать. Изменение DNS заблокировано без догадок."
		}
	} else if !splitSupported {
		response.Warning = "Раздельный DNS недоступен на этом устройстве. DNS через роутер продолжает работать штатно."
	}
	return response
}

func (a *app) handleSettingsDNSControlGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, settingsDNSControlSnapshot(a.cfg.ConfigPath))
}

func (a *app) handleSettingsDNSControlPost(w http.ResponseWriter, r *http.Request) {
	if a.mutationBlockedBySelfUpdate(w) {
		return
	}
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, settingsDNSControlResponse{Success: false, Error: "cross-origin request rejected"})
		return
	}
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, settingsDNSControlResponse{Success: false, Error: "application/json required"})
		return
	}
	body := http.MaxBytesReader(w, r.Body, 1024)
	defer body.Close()
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	var req settingsDNSControlRequest
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, settingsDNSControlResponse{Success: false, Error: "invalid request"})
		return
	}
	if !req.Confirm {
		writeJSON(w, http.StatusBadRequest, settingsDNSControlResponse{Success: false, Error: "explicit confirmation required"})
		return
	}
	if req.Mode != "firmware" && req.Mode != "xkeen" {
		writeJSON(w, http.StatusBadRequest, settingsDNSControlResponse{Success: false, Error: "unsupported DNS mode"})
		return
	}
	if !validSettingsDNSProvider(req.DirectProvider) || !validSettingsDNSProvider(req.VPNProvider) {
		writeJSON(w, http.StatusBadRequest, settingsDNSControlResponse{Success: false, Error: "unsupported resolver provider"})
		return
	}
	if req.Mode == "xkeen" {
		if capabilityErr := splitDNSSelectionError("xkeen"); capabilityErr != nil {
			writeJSON(w, http.StatusConflict, settingsDNSControlResponse{Success: false, PrimaryError: capabilityErr.Error(), RollbackState: "NOT_APPLIED", Error: "Раздельный DNS недоступен на этом устройстве"})
			return
		}
	}

	acquire := time.NewTimer(12 * time.Second)
	defer acquire.Stop()
	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	case <-r.Context().Done():
		writeJSON(w, http.StatusRequestTimeout, settingsDNSControlResponse{Success: false, RollbackState: "NOT_APPLIED", Error: "запрос отменён до начала DNS operation"})
		return
	case <-acquire.C:
		writeJSON(w, http.StatusLocked, settingsDNSControlResponse{Success: false, RollbackState: "NOT_APPLIED", Error: "FreeNet выполняет другую подтверждённую operation"})
		return
	}

	status, result := a.executeSettingsDNSControl(req)
	writeJSON(w, status, result)
}

func settingsDNSApplyPath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_SETTINGS_DNS_APPLY_HELPER")); value != "" {
		return value
	}
	return settingsDNSApplyPathDefault
}

func settingsDNSRestorePath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_SETTINGS_DNS_RESTORE_HELPER")); value != "" {
		return value
	}
	return settingsDNSRestorePathDefault
}

func ensureSettingsDNSScript(path string, data []byte, configured bool) (string, error) {
	if configured {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return "", errors.New("configured Settings DNS helper is unavailable")
		}
		return path, nil
	}
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		_ = os.Chmod(path, 0755)
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0755); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp, 0755); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}

func runSettingsDNSApplyHelper(ctx context.Context, mode, direct, vpn string) ([]byte, error) {
	path, err := ensureSettingsDNSScript(settingsDNSApplyPath(), settingsDNSApplyScript, strings.TrimSpace(os.Getenv("FREENET_SETTINGS_DNS_APPLY_HELPER")) != "")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, mode, direct, vpn)
	cmd.Env = append(os.Environ(), "PATH=/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin", "FREENET_XRAY_CONFIG_DIR="+settingsDNSConfigDir())
	cmd.WaitDelay = 2 * time.Second
	output, runErr := cmd.CombinedOutput()
	if errors.Is(runErr, exec.ErrWaitDelay) && ctx.Err() == nil {
		runErr = nil
	}
	return output, runErr
}

func runSettingsDNSRestoreHelper(ctx context.Context, backup string) ([]byte, error) {
	path, err := ensureSettingsDNSScript(settingsDNSRestorePath(), settingsDNSRestoreScript, strings.TrimSpace(os.Getenv("FREENET_SETTINGS_DNS_RESTORE_HELPER")) != "")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, backup)
	cmd.Env = append(os.Environ(), "PATH=/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin", "FREENET_XRAY_CONFIG_DIR="+settingsDNSConfigDir())
	cmd.WaitDelay = 2 * time.Second
	return cmd.CombinedOutput()
}

func writeSettingsDNSProviderKeys(path, direct, vpn string) error {
	if !validSettingsDNSProvider(direct) || !validSettingsDNSProvider(vpn) {
		return errors.New("unsupported resolver provider")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	values := map[string]string{"SPLIT_DIRECT_DNS_PROVIDER": direct, "SPLIT_VPN_DNS_PROVIDER": vpn}
	seen := map[string]bool{}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n")
	for index, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		for key, value := range values {
			if strings.HasPrefix(trimmed, key+"=") {
				lines[index] = key + "=" + value
				seen[key] = true
			}
		}
	}
	for _, key := range []string{"SPLIT_DIRECT_DNS_PROVIDER", "SPLIT_VPN_DNS_PROVIDER"} {
		if !seen[key] {
			lines = append(lines, key+"="+values[key])
		}
	}
	return os.WriteFile(path, []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n")+"\n"), 0600)
}

func (a *app) prepareSettingsDNSConfig(isp, mode, nativeProvider, direct, vpn string) (string, error) {
	draft, err := a.createNetworkDraftConfig(isp, mode, nativeProvider)
	if err != nil {
		return "", err
	}
	defer os.Remove(draft)
	if err := writeSettingsDNSProviderKeys(draft, direct, vpn); err != nil {
		return "", err
	}
	data, err := os.ReadFile(draft)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(a.cfg.ConfigPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	file, err := os.CreateTemp(dir, ".freenet-dns-target-*.conf")
	if err != nil {
		return "", err
	}
	name := file.Name()
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := file.Chmod(0600); err != nil {
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	ok = true
	return name, nil
}

func settingsDNSResolverSnapshot() (string, error) {
	data, err := os.ReadFile(filepath.Join(settingsDNSConfigDir(), "02_dns.json"))
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp("", "freenet-dns-before-*.json")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Chmod(0600); err != nil {
		_ = file.Close(); _ = os.Remove(name); return "", err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close(); _ = os.Remove(name); return "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(name); return "", err
	}
	return name, nil
}

func restoreSettingsDNSResolver(backup string) string {
	if strings.TrimSpace(backup) == "" {
		return "NOT_NEEDED"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 95*time.Second)
	output, err := runSettingsDNSRestoreHelper(ctx, backup)
	cancel()
	if err != nil || !strings.Contains(string(output), "RESULT=RESTORED") {
		return "FAILED/UNKNOWN"
	}
	return "SUCCESS"
}

func settingsDNSRollbackNetwork(a *app, isp, mode, nativeProvider string) string {
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Timeout)
	output, err := a.runNetworkApplyFor(ctx, isp, mode, nativeProvider)
	cancel()
	if err != nil {
		_, rollback := classifyApplyFailure(sanitizeOutput(string(output)))
		if rollback == "" || rollback == "UNKNOWN" {
			return "FAILED/UNKNOWN"
		}
		return rollback
	}
	post, err := a.runNetworkPlanFor(isp, mode, nativeProvider)
	if err != nil || !post.Active {
		return "FAILED/UNKNOWN"
	}
	return "SUCCESS"
}

func (a *app) executeSettingsDNSControl(req settingsDNSControlRequest) (int, settingsDNSControlResponse) {
	activeISP, activeMode := readNetworkProfileConfig(a.cfg.ConfigPath)
	activeNativeProvider := readNativeDNSProvider(a.cfg.ConfigPath)
	_, _, currentResolverState := settingsDNSRuntimeState()
	if activeMode == "xkeen" && currentResolverState == "unknown" {
		result := settingsDNSControlSnapshot(a.cfg.ConfigPath)
		result.Success = false
		result.PrimaryError = "active Split DNS resolver state is ambiguous"
		result.RollbackState = "NOT_APPLIED"
		result.Error = "Текущий Split DNS нельзя безопасно изменить без точного runtime-факта."
		return http.StatusConflict, result
	}

	stagedConfig, err := a.prepareSettingsDNSConfig(activeISP, req.Mode, activeNativeProvider, req.DirectProvider, req.VPNProvider)
	if err != nil {
		return http.StatusInternalServerError, settingsDNSControlResponse{Success: false, RollbackState: "NOT_APPLIED", Error: "не удалось подготовить целевую DNS-конфигурацию"}
	}
	defer os.Remove(stagedConfig)

	plan, planErr := a.runNetworkPlanFor(activeISP, req.Mode, activeNativeProvider)
	if planErr != nil || !plan.Supported || plan.Mutation != "NONE" {
		primary := "network DNS plan не прошёл read-only validation"
		if planErr != nil { primary = planErr.Error() } else if plan.Reason != "" { primary = plan.Reason }
		return http.StatusConflict, settingsDNSControlResponse{Success: false, PrimaryError: primary, RollbackState: "NOT_APPLIED", Error: "DNS apply остановлен до mutation"}
	}
	if activeMode == req.Mode && !plan.Active {
		return http.StatusConflict, settingsDNSControlResponse{Success: false, PrimaryError: "current DNS topology is not canonical", RollbackState: "NOT_APPLIED", Error: "Текущая DNS topology требует отдельного восстановления; resolver mutation не выполнялась."}
	}

	topologyChanged := activeMode != req.Mode
	if topologyChanged {
		if req.Mode == "firmware" {
			if err := prepareCanonicalNativeApplyState(); err != nil {
				return http.StatusServiceUnavailable, settingsDNSControlResponse{Success: false, PrimaryError: err.Error(), RollbackState: "NOT_APPLIED", Error: "не удалось подготовить Native DNS state"}
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Timeout)
		output, applyErr := runNetworkHelperWithConfig(ctx, stagedConfig, "apply")
		timedOut := ctx.Err() == context.DeadlineExceeded
		cancel()
		if timedOut { applyErr = errors.New("network DNS topology apply timed out") }
		if applyErr != nil {
			primary, rollback := classifyApplyFailure(sanitizeOutput(string(output)))
			if primary == "" { primary = applyErr.Error() }
			return http.StatusBadGateway, settingsDNSControlResponse{Success: false, PrimaryError: primary, RollbackState: rollback, Error: "DNS topology apply failed"}
		}
	}

	resolverBackup := ""
	if req.Mode == "xkeen" {
		resolverBackup, err = settingsDNSResolverSnapshot()
		if err != nil {
			rollback := "NOT_APPLIED"
			if topologyChanged { rollback = settingsDNSRollbackNetwork(a, activeISP, activeMode, activeNativeProvider) }
			return http.StatusBadGateway, settingsDNSControlResponse{Success: false, PrimaryError: "cannot snapshot active resolver config", RollbackState: rollback, Error: "resolver mutation не началась"}
		}
		defer os.Remove(resolverBackup)

		planCtx, cancelPlan := context.WithTimeout(context.Background(), 35*time.Second)
		planOutput, helperPlanErr := runSettingsDNSApplyHelper(planCtx, "plan", req.DirectProvider, req.VPNProvider)
		planTimedOut := planCtx.Err() == context.DeadlineExceeded
		cancelPlan()
		if helperPlanErr != nil || planTimedOut || !strings.Contains(string(planOutput), "MUTATION=NONE") {
			rollback := "NOT_APPLIED"
			if topologyChanged { rollback = settingsDNSRollbackNetwork(a, activeISP, activeMode, activeNativeProvider) }
			primary := "resolver plan failed"
			if helperPlanErr != nil { primary = helperPlanErr.Error() }
			return http.StatusBadGateway, settingsDNSControlResponse{Success: false, PrimaryError: primary, RollbackState: rollback, Error: "resolver candidate validation failed"}
		}

		applyCtx, cancelApply := context.WithTimeout(context.Background(), 110*time.Second)
		resolverOutput, resolverErr := runSettingsDNSApplyHelper(applyCtx, "apply", req.DirectProvider, req.VPNProvider)
		resolverTimedOut := applyCtx.Err() == context.DeadlineExceeded
		cancelApply()
		if resolverTimedOut { resolverErr = errors.New("resolver apply timed out") }
		if resolverErr != nil {
			primary, helperRollback := classifyApplyFailure(sanitizeOutput(string(resolverOutput)))
			if primary == "" { primary = resolverErr.Error() }
			if helperRollback == "FAILED/UNKNOWN" || helperRollback == "UNKNOWN" {
				return http.StatusBadGateway, settingsDNSControlResponse{Success: false, PrimaryError: primary, RollbackState: "FAILED/UNKNOWN", Error: "resolver rollback не подтверждён; дальнейшая mutation остановлена"}
			}
			rollback := helperRollback
			if topologyChanged { rollback = settingsDNSRollbackNetwork(a, activeISP, activeMode, activeNativeProvider) }
			return http.StatusBadGateway, settingsDNSControlResponse{Success: false, PrimaryError: primary, RollbackState: rollback, Error: "resolver apply failed"}
		}
	}

	post, postErr := a.runNetworkPlanFor(activeISP, req.Mode, activeNativeProvider)
	if postErr != nil || !post.Active {
		rollback := "NOT_NEEDED"
		if topologyChanged { rollback = settingsDNSRollbackNetwork(a, activeISP, activeMode, activeNativeProvider) } else if req.Mode == "xkeen" { rollback = restoreSettingsDNSResolver(resolverBackup) }
		return http.StatusBadGateway, settingsDNSControlResponse{Success: false, PrimaryError: "post-apply DNS topology acceptance failed", RollbackState: rollback, Error: "DNS runtime не подтверждён"}
	}
	if req.Mode == "xkeen" {
		postDirect, postVPN, postState := settingsDNSRuntimeState()
		if postState != "accepted" || postDirect != req.DirectProvider || postVPN != req.VPNProvider {
			rollback := "NOT_NEEDED"
			if topologyChanged { rollback = settingsDNSRollbackNetwork(a, activeISP, activeMode, activeNativeProvider) } else { rollback = restoreSettingsDNSResolver(resolverBackup) }
			return http.StatusBadGateway, settingsDNSControlResponse{Success: false, PrimaryError: "resolver post-check mismatch", RollbackState: rollback, Error: "Активные resolver-ы не совпали с выбранными"}
		}
	}

	if err := os.Rename(stagedConfig, a.cfg.ConfigPath); err != nil {
		rollback := "NOT_NEEDED"
		if topologyChanged { rollback = settingsDNSRollbackNetwork(a, activeISP, activeMode, activeNativeProvider) } else if req.Mode == "xkeen" { rollback = restoreSettingsDNSResolver(resolverBackup) }
		return http.StatusBadGateway, settingsDNSControlResponse{Success: false, PrimaryError: "cannot commit accepted DNS product state", RollbackState: rollback, Error: "runtime изменён, но целевая конфигурация не сохранена"}
	}

	result := settingsDNSControlSnapshot(a.cfg.ConfigPath)
	result.Applied = topologyChanged || req.Mode == "xkeen"
	result.Message = "DNS-настройки применены, проверены и сохранены."
	result.RollbackState = "NOT_NEEDED"
	return http.StatusOK, result
}
