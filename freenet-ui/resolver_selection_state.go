package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultNetworkBridgeNativeDNSStateDir = "/opt/etc/freenet/native-dns"

func networkBridgeNativeDNSStateDir() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_NATIVE_DNS_STATE_DIR")); value != "" {
		return value
	}
	return defaultNetworkBridgeNativeDNSStateDir
}

func networkBridgeNativeResolverSnapshotPaths() (string, string) {
	base := filepath.Join(networkBridgeNativeDNSStateDir(), "resolver-selection.native")
	return base, base + ".sha256"
}

func networkBridgeResolverInterfaceTokenSupported(token string) bool {
	if token == "" {
		return false
	}
	for _, r := range token {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("_.:/-", r) {
			continue
		}
		return false
	}
	return true
}

// Keenetic can serialize an explicit System DNS selection in either the compact
// CLI form:
//
//   ip name-server 77.88.8.8
//
// or an interface-qualified running-config form:
//
//   ip name-server 77.88.8.8 "" on GigabitEthernet0/Vlan5
//
// The latter is still the same active resolver selection. Accept only this exact
// known qualifier shape; arbitrary extra CLI syntax remains fail-closed.
func networkBridgeResolverSelectionLineSupported(line string) bool {
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) != 3 && len(fields) != 6 {
		return false
	}
	if (fields[0] != "ip" && fields[0] != "ipv6") || fields[1] != "name-server" || strings.Trim(fields[2], "\"") == "" {
		return false
	}
	if len(fields) == 3 {
		return true
	}
	return fields[3] == `""` && fields[4] == "on" && networkBridgeResolverInterfaceTokenSupported(fields[5])
}

func networkBridgeResolverSelectionKey(line string) string {
	line = strings.TrimSpace(line)
	if !networkBridgeResolverSelectionLineSupported(line) {
		return ""
	}
	fields := strings.Fields(line)
	return fields[0] + " name-server " + strings.Trim(fields[2], "\"")
}

// Return only top-level explicit resolver selections. The Split-owned LAN pointer
// is excluded. Interface/WAN hints and dns-proxy profile definitions are not active
// system selections and are intentionally ignored here.
func networkBridgeNativeResolverSelectionLines(config, lanIP string) ([]string, error) {
	var result []string
	seen := map[string]bool{}
	for _, raw := range strings.Split(strings.ReplaceAll(config, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		indented := len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t')
		if indented {
			continue
		}
		if strings.HasPrefix(line, "ip name-server ") {
			fields := strings.Fields(line)
			if len(fields) < 3 {
				return nil, errors.New("invalid IPv4 resolver selection syntax")
			}
			address := strings.Trim(fields[2], "\"")
			if networkBridgeAddressMatchesLAN(address, lanIP) {
				continue
			}
			if !networkBridgeResolverSelectionLineSupported(line) {
				return nil, fmt.Errorf("unsupported IPv4 resolver selection syntax: %s", line)
			}
			key := networkBridgeResolverSelectionKey(line)
			if !seen[key] {
				result = append(result, line)
				seen[key] = true
			}
			continue
		}
		if strings.HasPrefix(line, "ipv6 name-server ") {
			if !networkBridgeResolverSelectionLineSupported(line) {
				return nil, fmt.Errorf("unsupported IPv6 resolver selection syntax: %s", line)
			}
			key := networkBridgeResolverSelectionKey(line)
			if !seen[key] {
				result = append(result, line)
				seen[key] = true
			}
		}
	}
	return result, nil
}

func networkBridgeCurrentNativeResolverSelection(lanIP string) ([]string, error) {
	config, err := networkBridgeRunningConfig()
	if err != nil {
		return nil, err
	}
	return networkBridgeNativeResolverSelectionLines(config, lanIP)
}

func networkBridgeResolverSelectionLinePresent(config, want string) bool {
	want = strings.TrimSpace(want)
	for _, raw := range strings.Split(strings.ReplaceAll(config, "\r", ""), "\n") {
		if len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t') {
			continue
		}
		if strings.TrimSpace(raw) == want {
			return true
		}
	}
	return false
}

func networkBridgeResolverSelectionPresent(config, want string) bool {
	wantKey := networkBridgeResolverSelectionKey(want)
	if wantKey == "" {
		return false
	}
	for _, raw := range strings.Split(strings.ReplaceAll(config, "\r", ""), "\n") {
		if len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t') {
			continue
		}
		if networkBridgeResolverSelectionKey(strings.TrimSpace(raw)) == wantKey {
			return true
		}
	}
	return false
}

func networkBridgeAddResolverSelectionLines(lines []string) ([]string, error) {
	added := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !networkBridgeResolverSelectionLineSupported(line) {
			_ = networkBridgeRemoveResolverSelectionLines(added)
			return nil, fmt.Errorf("unsupported resolver selection snapshot line: %s", line)
		}
		config, err := networkBridgeRunningConfig()
		if err != nil {
			_ = networkBridgeRemoveResolverSelectionLines(added)
			return nil, err
		}
		if networkBridgeResolverSelectionPresent(config, line) {
			continue
		}
		if err := networkBridgeNDMC(line); err != nil {
			_ = networkBridgeRemoveResolverSelectionLines(added)
			return nil, err
		}
		updated, err := networkBridgeRunningConfig()
		if err != nil || !networkBridgeResolverSelectionPresent(updated, line) {
			_ = networkBridgeRemoveResolverSelectionLines(append(added, line))
			return nil, errors.New("resolver selection was not accepted by Keenetic")
		}
		added = append(added, line)
	}
	return added, nil
}

func networkBridgeRemoveResolverSelectionLines(lines []string) error {
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !networkBridgeResolverSelectionLineSupported(line) {
			return fmt.Errorf("unsupported resolver selection removal line: %s", line)
		}
		config, err := networkBridgeRunningConfig()
		if err != nil {
			return err
		}
		if !networkBridgeResolverSelectionPresent(config, line) {
			continue
		}
		if err := networkBridgeNDMC("no " + line); err != nil {
			return err
		}
		updated, err := networkBridgeRunningConfig()
		if err != nil {
			return err
		}
		if networkBridgeResolverSelectionPresent(updated, line) {
			return errors.New("resolver selection remained active")
		}
	}
	return nil
}

func networkBridgeWriteNativeResolverSelection(lines []string) error {
	if len(lines) == 0 {
		return errors.New("refusing to overwrite native resolver snapshot with empty selection")
	}
	for _, line := range lines {
		if !networkBridgeResolverSelectionLineSupported(line) {
			return fmt.Errorf("unsupported resolver selection snapshot line: %s", line)
		}
	}
	dir := networkBridgeNativeDNSStateDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path, hashPath := networkBridgeNativeResolverSnapshotPaths()
	data := []byte(strings.Join(lines, "\n") + "\n")
	sum := fmt.Sprintf("%x", sha256.Sum256(data)) + "\n"
	tmpPath := path + ".tmp"
	tmpHash := hashPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(tmpHash, []byte(sum), 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		_ = os.Remove(tmpHash)
		return err
	}
	if err := os.Rename(tmpHash, hashPath); err != nil {
		return err
	}
	return nil
}

func networkBridgeLoadNativeResolverSelection() ([]string, error) {
	path, hashPath := networkBridgeNativeResolverSnapshotPaths()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	hashData, err := os.ReadFile(hashPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("native resolver selection snapshot checksum is missing")
		}
		return nil, err
	}
	actual := fmt.Sprintf("%x", sha256.Sum256(data))
	expected := strings.TrimSpace(string(hashData))
	if expected == "" || actual != expected {
		return nil, errors.New("native resolver selection snapshot checksum mismatch")
	}
	var lines []string
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if !networkBridgeResolverSelectionLineSupported(line) {
			return nil, fmt.Errorf("unsupported native resolver selection snapshot line: %s", line)
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil, errors.New("native resolver selection snapshot is empty")
	}
	return lines, nil
}

func augmentNetworkBridgeSplitResolverPlan(output string, count int) string {
	if count == 0 {
		return output
	}
	values := parseNetworkBridgeValues(output)
	delta := values["EXPECTED_DELTA"]
	extra := fmt.Sprintf("snapshot and temporarily detach %d active native resolver selection(s) so Split uses only local Xray DNS", count)
	if delta == "" {
		delta = extra
	} else if !strings.Contains(delta, extra) {
		delta += "; " + extra
	}
	out := strings.TrimRight(output, "\r\n") + "\nEXPECTED_DELTA=" + delta + "\n"
	out += fmt.Sprintf("NATIVE_RESOLVER_SELECTION=detach-for-split:%d\n", count)
	if networkBridgeRuntimeSplit(values) {
		out += "DNS_ROUTING_MODE=split-native-resolver-selection-present\n"
	}
	return out
}

func networkBridgeRollbackSplitResolverRepair(lanIP string, pointerWasPresent bool, selection []string) error {
	if _, err := networkBridgeAddResolverSelectionLines(selection); err != nil {
		return err
	}
	if !pointerWasPresent {
		if err := networkBridgeRemoveLocalPointer(lanIP); err != nil {
			return err
		}
	}
	return networkBridgeSave()
}
