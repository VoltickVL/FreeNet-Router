package main

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func networkBridgeRoot() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_ROOT")); value != "" {
		return value
	}
	return "/opt"
}

func networkBridgeBackupRoot() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_BACKUP_ROOT")); value != "" {
		return value
	}
	return filepath.Join(networkBridgeRoot(), "backups")
}

func networkBridgeNativeStateDir() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_NATIVE_DNS_STATE_DIR")); value != "" {
		return value
	}
	return filepath.Join(networkBridgeRoot(), "etc", "freenet", "native-dns")
}

func networkBridgeStrictRuntimeSplit(values map[string]string) bool {
	return networkBridgeRuntimeSplit(values) &&
		values["PROXY_DNS"] == "off" &&
		values["NDM_DNS_INTERCEPT"] == "off" &&
		values["NDM_DNS_ASSIGNMENTS"] == "none" &&
		values["XRAY_DNS_INBOUND_COUNT"] == "1" &&
		values["XRAY_RUNNING"] == "yes" &&
		values["XRAY_GID"] == "11111" &&
		values["DNS_OUT"] == "yes" &&
		values["VLESS_PROFILE"] == "yes"
}

func networkBridgeCanonicalLine(line string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
}

func networkBridgeCanonicalLineSet(text string) string {
	var lines []string
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		line := networkBridgeCanonicalLine(raw)
		if line != "" {
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func networkBridgeProtectedDNSLine(line string) bool {
	fields := strings.Fields(line)
	if len(fields) >= 2 && (fields[0] == "tls" || fields[0] == "https" || fields[0] == "dns53") && fields[1] == "upstream" {
		return true
	}
	return len(fields) >= 2 && fields[0] == "filter" && fields[1] == "profile"
}

func networkBridgeContainsField(line, want string) bool {
	for _, field := range strings.Fields(line) {
		if field == want {
			return true
		}
	}
	return false
}

// networkBridgeProtectedStateText mirrors the shell controller's protected NDM
// projection. Engine/intercept/active assignments are intentionally excluded;
// native DNS upstream/profile definitions and WAN/client DNS flags are retained.
func networkBridgeProtectedStateText(config string) string {
	var protected []string
	scope := ""
	emit := func(scopeName, line string) {
		line = strings.TrimSpace(line)
		if line != "" {
			protected = append(protected, scopeName+"|"+line)
		}
	}

	for _, raw := range strings.Split(strings.ReplaceAll(config, "\r", ""), "\n") {
		if raw == "" {
			continue
		}
		topLevel := raw[0] != ' ' && raw[0] != '\t'
		line := strings.TrimSpace(raw)
		if topLevel {
			switch {
			case line == "!":
				scope = ""
			case line == "dns-proxy":
				scope = "dns-proxy"
			case strings.HasPrefix(line, "dns-proxy "):
				child := strings.TrimSpace(strings.TrimPrefix(line, "dns-proxy "))
				if networkBridgeProtectedDNSLine(child) {
					emit("dns-proxy", child)
				}
				scope = ""
			case strings.HasPrefix(line, "interface "):
				scope = line
			case strings.HasPrefix(line, "ip host "), strings.HasPrefix(line, "ip name-server "), strings.HasPrefix(line, "ipv6 name-server "):
				emit("global", line)
				scope = ""
			default:
				scope = ""
			}
			continue
		}

		switch {
		case scope == "dns-proxy" && networkBridgeProtectedDNSLine(line):
			emit("dns-proxy", line)
		case strings.HasPrefix(scope, "interface ") && (networkBridgeContainsField(line, "name-servers") || strings.HasPrefix(line, "ip dhcp client dns-routes")):
			emit(scope, line)
		}
	}

	return networkBridgeCanonicalLineSet(strings.Join(protected, "\n"))
}

func networkBridgeCanonicalAssignment(line string) (string, error) {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 6 || fields[0] != "filter" || fields[1] != "assign" {
		return "", errors.New("invalid DNS filter assignment")
	}
	if fields[2] != "host" && fields[2] != "interface" {
		return "", errors.New("unsupported DNS filter assignment scope")
	}
	if fields[3] != "profile" && fields[3] != "preset" {
		return "", errors.New("unsupported DNS filter assignment kind")
	}
	return strings.Join(fields, " "), nil
}

func networkBridgeAssignmentsFromRunningConfig(config string) (string, error) {
	var assignments []string
	scope := ""
	appendAssignment := func(line string) error {
		canonical, err := networkBridgeCanonicalAssignment(line)
		if err != nil {
			return err
		}
		assignments = append(assignments, canonical)
		return nil
	}

	for _, raw := range strings.Split(strings.ReplaceAll(config, "\r", ""), "\n") {
		if raw == "" {
			continue
		}
		topLevel := raw[0] != ' ' && raw[0] != '\t'
		line := strings.TrimSpace(raw)
		if topLevel {
			switch {
			case line == "!":
				scope = ""
			case line == "dns-proxy":
				scope = "dns-proxy"
			case strings.HasPrefix(line, "dns-proxy filter assign "):
				if err := appendAssignment(strings.TrimSpace(strings.TrimPrefix(line, "dns-proxy "))); err != nil {
					return "", err
				}
				scope = ""
			default:
				scope = ""
			}
			continue
		}
		if scope == "dns-proxy" && strings.HasPrefix(line, "filter assign ") {
			if err := appendAssignment(line); err != nil {
				return "", err
			}
		}
	}

	sort.Strings(assignments)
	if len(assignments) == 0 {
		return "", nil
	}
	return strings.Join(assignments, "\n") + "\n", nil
}

func networkBridgeAssignmentSnapshotValid(data []byte) bool {
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if _, err := networkBridgeCanonicalAssignment(raw); err != nil {
			return false
		}
	}
	return true
}

func networkBridgeReadTrimmed(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.ReplaceAll(string(data), "\r", "")), nil
}

func networkBridgeConfigWithoutLocalPointer(config, lanIP string) string {
	var kept []string
	for _, raw := range strings.Split(strings.ReplaceAll(config, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "ip name-server ") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				address := strings.Trim(fields[2], "\"")
				if address == lanIP || address == lanIP+":53" {
					continue
				}
			}
		}
		kept = append(kept, raw)
	}
	return strings.Join(kept, "\n")
}

func networkBridgeExistingAssignmentsSnapshot(nativeDir string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(nativeDir, "assignments.native"))
	if err == nil {
		if !networkBridgeAssignmentSnapshotValid(data) {
			return false, errors.New("existing native assignments snapshot is invalid")
		}
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// networkBridgeFindNativeAssignmentsCandidate recovers only the assignments
// control-plane. DNS resolver profiles/upstreams/WAN flags are intentionally not
// used as a matching key because they are preserved independently and may change
// legitimately after the old native snapshot. A candidate is accepted only when
// every local native backup with the saved engine/intercept baseline agrees on the
// exact assignment set; disagreement remains ambiguous and fails closed.
func networkBridgeFindNativeAssignmentsCandidate(nativeDir, backupRoot, currentConfig string) (string, error) {
	_ = currentConfig
	nativeEngine, err := networkBridgeReadTrimmed(filepath.Join(nativeDir, "filter-engine.native"))
	if err != nil || nativeEngine == "" || nativeEngine == "opkg" {
		return "", errors.New("native filter engine baseline is unavailable")
	}
	nativeIntercept, err := networkBridgeReadTrimmed(filepath.Join(nativeDir, "intercept.native"))
	if err != nil || (nativeIntercept != "on" && nativeIntercept != "off") {
		return "", errors.New("native intercept baseline is unavailable")
	}

	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		return "", errors.New("historical network backups are unavailable")
	}
	candidate := ""
	found := false
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "freenet-network-") {
			continue
		}
		dir := filepath.Join(backupRoot, entry.Name())
		override, err := networkBridgeReadTrimmed(filepath.Join(dir, "ndm-override.before"))
		if err != nil || override != "off" {
			continue
		}
		engine, err := networkBridgeReadTrimmed(filepath.Join(dir, "ndm-filter-engine.before"))
		if err != nil || engine != nativeEngine {
			continue
		}
		intercept, err := networkBridgeReadTrimmed(filepath.Join(dir, "ndm-intercept.before"))
		if err != nil || intercept != nativeIntercept {
			continue
		}
		runningData, err := os.ReadFile(filepath.Join(dir, "ndm-running.before"))
		if err != nil {
			continue
		}
		assignments, err := networkBridgeAssignmentsFromRunningConfig(string(runningData))
		if err != nil {
			continue
		}
		if !found {
			candidate = assignments
			found = true
			continue
		}
		if candidate != assignments {
			return "", errors.New("matching historical native backups contain different DNS filter assignments")
		}
	}
	if !found {
		return "", errors.New("no unambiguous historical native assignments backup matches native engine/intercept baseline")
	}
	return candidate, nil
}

func networkBridgeNativeAssignmentsRecoveryStatus(lanIP string) (string, error) {
	nativeDir := networkBridgeNativeStateDir()
	exists, err := networkBridgeExistingAssignmentsSnapshot(nativeDir)
	if err != nil {
		return "", err
	}
	if exists {
		return "snapshot-present", nil
	}
	config, err := networkBridgeRunningConfig()
	if err != nil {
		return "", errors.New("cannot read current Keenetic running-config")
	}
	config = networkBridgeConfigWithoutLocalPointer(config, lanIP)
	if _, err := networkBridgeFindNativeAssignmentsCandidate(nativeDir, networkBridgeBackupRoot(), config); err != nil {
		return "", err
	}
	return "historical-native-backup-ready", nil
}

// networkBridgeRecoverNativeAssignmentsSnapshot repairs the one legacy gap left
// by pre-v0.2.53 Split: those releases saved full ndm-running.before snapshots but
// did not persist assignments.native. Assignment recovery is deliberately
// independent from resolver profile/upstream state: it requires the saved native
// engine/intercept baseline and unanimous exact assignments across matching local
// native backups. Different assignment sets remain ambiguous and fail closed.
func networkBridgeRecoverNativeAssignmentsSnapshot(nativeDir, backupRoot, currentConfig string) (bool, error) {
	exists, err := networkBridgeExistingAssignmentsSnapshot(nativeDir)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}

	candidate, err := networkBridgeFindNativeAssignmentsCandidate(nativeDir, backupRoot, currentConfig)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(nativeDir, 0o700); err != nil {
		return false, err
	}
	f, err := os.CreateTemp(nativeDir, ".assignments.native.*")
	if err != nil {
		return false, err
	}
	tmp := f.Name()
	keep := false
	defer func() {
		_ = f.Close()
		if !keep {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return false, err
	}
	if _, err := f.WriteString(candidate); err != nil {
		return false, err
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmp, filepath.Join(nativeDir, "assignments.native")); err != nil {
		return false, err
	}
	keep = true
	return true, nil
}

func networkBridgeEnsureNativeAssignmentsSnapshot() (bool, error) {
	config, err := networkBridgeRunningConfig()
	if err != nil {
		return false, errors.New("cannot read current Keenetic running-config")
	}
	return networkBridgeRecoverNativeAssignmentsSnapshot(networkBridgeNativeStateDir(), networkBridgeBackupRoot(), config)
}
