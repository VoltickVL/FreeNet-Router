package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultNetworkBridgeExecutable = "/opt/sbin/freenet-ui"
	defaultNetworkHelperCore       = "/opt/lib/freenet/apply_network_profile.sh"
	defaultNetworkBridgeConfig     = "/opt/etc/freenet/freenet.conf"
)

func networkBridgeCanonicalExecutable() (string, bool) {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, exe)
	}
	if len(os.Args) > 0 {
		candidates = append(candidates, os.Args[0])
	}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(strings.TrimSuffix(candidate, " (deleted)"))
		if candidate == "" {
			continue
		}
		if filepath.Base(filepath.Clean(candidate)) == "freenet-ui" {
			return defaultNetworkBridgeExecutable, true
		}
	}
	return "", false
}

func networkBridgeShouldOwnHelper(current string) bool {
	current = strings.TrimSpace(current)
	return current == "" || filepath.Clean(current) == filepath.Clean(defaultNetworkHelperCore)
}

// FreeNet owns the product-level DNS switch, while the legacy shell helper owns
// the already hardened Xray/NDM transaction. A production freenet-ui process must
// route network plan/apply through itself so the resolver-selection and migration
// bridge cannot be bypassed by an executable-path representation or a legacy
// default helper environment. Explicit non-default helper overrides remain intact.
func init() {
	bridgeExe, production := networkBridgeCanonicalExecutable()
	if !production {
		return
	}
	if len(os.Args) > 1 && (os.Args[1] == "plan" || os.Args[1] == "apply") {
		os.Exit(runNetworkHelperBridge(os.Args[1]))
	}
	if networkBridgeShouldOwnHelper(os.Getenv("FREENET_NETWORK_HELPER")) {
		_ = os.Setenv("FREENET_NETWORK_HELPER", bridgeExe)
	}
}

func networkBridgeCorePath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_NETWORK_HELPER_CORE")); value != "" {
		return value
	}
	return defaultNetworkHelperCore
}

func networkBridgeConfigPath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_CONFIG_FILE")); value != "" {
		return value
	}
	return defaultNetworkBridgeConfig
}

func runNetworkHelperBridge(mode string) int {
	configPath := networkBridgeConfigPath()
	planOutput, planErr := runNetworkBridgeCore(configPath, "plan")
	if planErr != nil {
		_, _ = os.Stdout.Write(planOutput)
		return bridgeExitCode(planErr)
	}
	values := parseNetworkBridgeValues(string(planOutput))
	target := values["EFFECTIVE_DNS_MODE"]
	if target != "xkeen" && target != "firmware" {
		_, _ = os.Stdout.Write(planOutput)
		return 0
	}

	lanIP, err := networkBridgeLANIPv4()
	if err != nil {
		_, _ = os.Stdout.Write(planOutput)
		bridgeFailure("не удалось определить LAN IPv4 для локального Xray DNS upstream", "NOT_APPLIED")
		return 1
	}
	pointerPresent, err := networkBridgeLocalPointerPresent(lanIP)
	if err != nil {
		_, _ = os.Stdout.Write(planOutput)
		bridgeFailure("не удалось прочитать активный Keenetic DNS upstream", "NOT_APPLIED")
		return 1
	}

	if mode == "plan" {
		out := augmentNetworkBridgePlan(string(planOutput), target, lanIP, pointerPresent)
		if target == "firmware" && networkBridgeStrictRuntimeSplit(values) {
			status, recoveryErr := networkBridgeNativeAssignmentsRecoveryStatus(lanIP)
			if recoveryErr != nil {
				out = strings.TrimRight(out, "\r\n") + "\nSUPPORTED=no\nREASON=legacy native DNS filter assignments preflight: " + recoveryErr.Error() + "\n"
			} else {
				out = strings.TrimRight(out, "\r\n") + "\nNATIVE_ASSIGNMENTS_RECOVERY=" + status + "\n"
			}
		}
		_, _ = os.Stdout.WriteString(out)
		return 0
	}
	if mode != "apply" {
		bridgeFailure("unsupported network bridge mode", "NOT_APPLIED")
		return 2
	}

	if target == "xkeen" {
		return applyNetworkBridgeSplit(configPath, lanIP, pointerPresent, values)
	}
	return applyNetworkBridgeNative(configPath, lanIP, pointerPresent, values)
}

func runNetworkBridgeCore(configPath string, args ...string) ([]byte, error) {
	core := networkBridgeCorePath()
	if filepath.Clean(core) == filepath.Clean(defaultNetworkBridgeExecutable) {
		return nil, errors.New("network helper core points to bridge executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, core, args...)
	cmd.Env = append(os.Environ(), "FREENET_CONFIG_FILE="+configPath)
	output, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return output, errors.New("network helper core timed out")
	}
	return output, err
}

func bridgeExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() > 0 {
		return exitErr.ExitCode()
	}
	return 1
}

func parseNetworkBridgeValues(output string) map[string]string {
	values := map[string]string{}
	for _, raw := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		key, value, ok := strings.Cut(line, "=")
		if ok && key != "" {
			values[key] = strings.TrimSpace(value)
		}
	}
	return values
}

func networkBridgeRuntimeSplit(values map[string]string) bool {
	return values["NDM_DNS_OVERRIDE"] == "on" &&
		values["NDM_FILTER_ENGINE"] == "opkg" &&
		values["PORT53_OWNER"] == "xray" &&
		values["DNS_ROUTING_MODE"] == "split"
}

func networkBridgeRuntimeNative(values map[string]string) bool {
	return values["NDM_DNS_OVERRIDE"] == "off" &&
		values["PORT53_OWNER"] == "ndnproxy" &&
		values["DNS_ROUTING_MODE"] == "native"
}

func augmentNetworkBridgePlan(output, target, lanIP string, pointerPresent bool) string {
	values := parseNetworkBridgeValues(output)
	delta := values["EXPECTED_DELTA"]
	appendDelta := func(extra string) {
		if delta == "" {
			delta = extra
		} else if !strings.Contains(delta, extra) {
			delta += "; " + extra
		}
	}
	if target == "xkeen" {
		appendDelta("set active Keenetic DNS upstream to local Xray " + lanIP + ":53")
	} else {
		appendDelta("remove Split-owned local Xray DNS upstream and restore native Keenetic resolver selection")
	}
	out := strings.TrimRight(output, "\r\n") + "\nEXPECTED_DELTA=" + delta + "\n"
	if pointerPresent {
		out += "LOCAL_XRAY_DNS_UPSTREAM=present\n"
	} else {
		out += "LOCAL_XRAY_DNS_UPSTREAM=missing\n"
	}
	if target == "xkeen" && networkBridgeRuntimeSplit(values) && !pointerPresent {
		out += "DNS_ROUTING_MODE=split-local-upstream-missing\n"
	}
	if target == "firmware" && networkBridgeRuntimeNative(values) && pointerPresent {
		out += "DNS_ROUTING_MODE=native-local-upstream-present\n"
	}
	return out
}

func networkBridgeLANIPv4() (string, error) {
	if value := strings.TrimSpace(os.Getenv("FREENET_NETWORK_BRIDGE_LAN_IP")); value != "" {
		ip := net.ParseIP(value)
		if ip == nil || ip.To4() == nil || ip.IsLoopback() {
			return "", errors.New("invalid LAN IPv4 override")
		}
		return value, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ip", "-o", "-4", "addr", "show", "br0").Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		for i := 0; i+1 < len(fields); i++ {
			if fields[i] != "inet" {
				continue
			}
			address := strings.SplitN(fields[i+1], "/", 2)[0]
			ip := net.ParseIP(address)
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				return address, nil
			}
		}
	}
	return "", errors.New("br0 IPv4 not found")
}

func networkBridgeRunningConfig() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "ndmc", "-c", "show running-config").CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(output), "\r", ""), nil
}

func networkBridgeLocalPointerLines(config, lanIP string) []string {
	var result []string
	for _, raw := range strings.Split(strings.ReplaceAll(config, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "ip name-server ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		address := strings.Trim(fields[2], "\"")
		if address == lanIP || address == lanIP+":53" {
			result = append(result, line)
		}
	}
	return result
}

func networkBridgeLocalPointerPresent(lanIP string) (bool, error) {
	config, err := networkBridgeRunningConfig()
	if err != nil {
		return false, err
	}
	return len(networkBridgeLocalPointerLines(config, lanIP)) > 0, nil
}

func networkBridgeNDMC(command string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "ndmc", "-c", command).Run()
}

func networkBridgeSave() error {
	return networkBridgeNDMC("system configuration save")
}

func networkBridgeAddLocalPointer(lanIP string) error {
	present, err := networkBridgeLocalPointerPresent(lanIP)
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	if err := networkBridgeNDMC("ip name-server " + lanIP + ":53"); err != nil {
		if fallbackErr := networkBridgeNDMC("ip name-server " + lanIP); fallbackErr != nil {
			return err
		}
	}
	present, err = networkBridgeLocalPointerPresent(lanIP)
	if err != nil || !present {
		return errors.New("local DNS pointer was not accepted by Keenetic")
	}
	return nil
}

func networkBridgeRemoveLocalPointer(lanIP string) error {
	config, err := networkBridgeRunningConfig()
	if err != nil {
		return err
	}
	lines := networkBridgeLocalPointerLines(config, lanIP)
	for _, line := range lines {
		if err := networkBridgeNDMC("no " + line); err != nil {
			return err
		}
	}
	// Current KeeneticOS accepts the explicit :53 form even when the web UI
	// renders only the IP. It is safe to try these idempotent removals as a
	// compatibility fallback when an old/manual entry is not serialized exactly.
	if len(lines) == 0 {
		_ = networkBridgeNDMC("no ip name-server " + lanIP + ":53")
		_ = networkBridgeNDMC("no ip name-server " + lanIP)
	}
	present, err := networkBridgeLocalPointerPresent(lanIP)
	if err != nil {
		return err
	}
	if present {
		return errors.New("local DNS pointer remained active")
	}
	return nil
}

func networkBridgeDNSQueryOK(lanIP string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "nslookup", "example.com", lanIP).Run() == nil
}

func applyNetworkBridgeSplit(configPath, lanIP string, pointerPresent bool, prePlan map[string]string) int {
	wasSplit := networkBridgeRuntimeSplit(prePlan)
	addedBeforeCore := false
	if wasSplit && !pointerPresent {
		if err := networkBridgeAddLocalPointer(lanIP); err != nil || networkBridgeSave() != nil {
			bridgeFailure("не удалось направить активный Keenetic DNS на локальный Xray "+lanIP+":53", "FAILED/UNKNOWN")
			return 1
		}
		addedBeforeCore = true
	}

	output, coreErr := runNetworkBridgeCore(configPath, "apply")
	_, _ = os.Stdout.Write(output)
	if coreErr != nil {
		if addedBeforeCore {
			if err := networkBridgeRemoveLocalPointer(lanIP); err != nil || networkBridgeSave() != nil {
				bridgeFailure("Split apply failed and local DNS pointer rollback failed", "FAILED/UNKNOWN")
			}
		}
		return bridgeExitCode(coreErr)
	}

	if !wasSplit {
		if err := networkBridgeAddLocalPointer(lanIP); err != nil || networkBridgeSave() != nil || !networkBridgeDNSQueryOK(lanIP) {
			return rollbackNetworkBridgeToNative(configPath, lanIP, "не удалось активировать локальный Xray DNS upstream после Split apply")
		}
	} else if !networkBridgeDNSQueryOK(lanIP) {
		if addedBeforeCore {
			_ = networkBridgeRemoveLocalPointer(lanIP)
			_ = networkBridgeSave()
		}
		bridgeFailure("локальный Xray DNS upstream не прошёл post-apply DNS query", "FAILED/UNKNOWN")
		return 1
	}

	present, err := networkBridgeLocalPointerPresent(lanIP)
	if err != nil || !present {
		bridgeFailure("Split acceptance: локальный Keenetic DNS upstream на Xray отсутствует", "FAILED/UNKNOWN")
		return 1
	}
	fmt.Printf("[FreeNet Network] LOCAL_XRAY_DNS_UPSTREAM=%s:53\n", lanIP)
	return 0
}

func applyNetworkBridgeNative(configPath, lanIP string, pointerPresent bool, prePlan map[string]string) int {
	removedBeforeCore := false
	if pointerPresent {
		if err := networkBridgeRemoveLocalPointer(lanIP); err != nil || networkBridgeSave() != nil {
			bridgeFailure("не удалось убрать Split-owned local Xray DNS upstream перед native restore", "FAILED/UNKNOWN")
			return 1
		}
		removedBeforeCore = true
	}

	if networkBridgeStrictRuntimeSplit(prePlan) {
		recovered, recoverErr := networkBridgeEnsureNativeAssignmentsSnapshot()
		if recoverErr != nil {
			rollback := "NOT_APPLIED"
			if removedBeforeCore {
				if err := networkBridgeAddLocalPointer(lanIP); err == nil && networkBridgeSave() == nil {
					rollback = "SUCCESS"
				} else {
					rollback = "FAILED/UNKNOWN"
				}
			}
			bridgeFailure("не удалось восстановить native DNS filter assignments baseline из historical backup: "+recoverErr.Error(), rollback)
			return 1
		}
		if recovered {
			fmt.Println("[FreeNet Network] NATIVE_ASSIGNMENTS_RECOVERY=historical-native-backup")
		}
	}

	output, coreErr := runNetworkBridgeCore(configPath, "apply")
	_, _ = os.Stdout.Write(output)
	if coreErr != nil {
		if removedBeforeCore {
			if err := networkBridgeAddLocalPointer(lanIP); err != nil || networkBridgeSave() != nil {
				bridgeFailure("native apply failed and local Xray DNS pointer rollback failed", "FAILED/UNKNOWN")
			}
		}
		return bridgeExitCode(coreErr)
	}

	present, err := networkBridgeLocalPointerPresent(lanIP)
	if err != nil || present {
		bridgeFailure("native acceptance: Split-owned local Xray DNS upstream остался активен", "FAILED/UNKNOWN")
		return 1
	}
	fmt.Println("[FreeNet Network] LOCAL_XRAY_DNS_UPSTREAM=none")
	return 0
}

func rollbackNetworkBridgeToNative(configPath, lanIP, primary string) int {
	_ = networkBridgeRemoveLocalPointer(lanIP)
	_ = networkBridgeSave()
	rollbackConfig, err := networkBridgeFirmwareDraft(configPath)
	if err != nil {
		bridgeFailure(primary, "FAILED/UNKNOWN")
		return 1
	}
	defer os.Remove(rollbackConfig)
	output, rollbackErr := runNetworkBridgeCore(rollbackConfig, "apply")
	_, _ = os.Stdout.Write(output)
	if rollbackErr != nil {
		bridgeFailure(primary, "FAILED/UNKNOWN")
		return 1
	}
	bridgeFailure(primary, "SUCCESS")
	return 1
}

func networkBridgeFirmwareDraft(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	text := strings.ReplaceAll(string(data), "\r", "")
	lines := strings.Split(text, "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "DNS_MODE=") {
			lines[i] = "DNS_MODE=firmware"
			found = true
		}
	}
	if !found {
		lines = append(lines, "DNS_MODE=firmware")
	}
	file, err := os.CreateTemp("", "freenet-network-bridge-native-*.conf")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		os.Remove(name)
		return "", err
	}
	if _, err := file.WriteString(strings.Join(lines, "\n")); err != nil {
		file.Close()
		os.Remove(name)
		return "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func bridgeFailure(primary, rollback string) {
	fmt.Fprintf(os.Stderr, "[FreeNet Network] ERROR: PRIMARY ERROR: %s\n", primary)
	switch rollback {
	case "SUCCESS":
		fmt.Fprintln(os.Stderr, "[FreeNet Network] ERROR: ROLLBACK ERROR/STATE: rollback success")
	case "NOT_APPLIED":
		fmt.Fprintln(os.Stderr, "[FreeNet Network] ERROR: ROLLBACK ERROR/STATE: no live apply")
	default:
		fmt.Fprintln(os.Stderr, "[FreeNet Network] ERROR: ROLLBACK ERROR/STATE: FAILED/UNKNOWN")
	}
}
