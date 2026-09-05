package main

import (
	"errors"
	"os"
	"strings"
)

var networkBridgeYandexBasicResolvers = []string{"77.88.8.8", "77.88.8.1"}

func networkBridgeNameServerAddress(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "ip name-server ") {
		return ""
	}
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(fields[2]), "\"")
}

func networkBridgeAddressMatchesLAN(address, lanIP string) bool {
	return address == lanIP || address == lanIP+":53"
}

// networkBridgeHasNativeResolverSelection answers whether Keenetic has an
// explicit independent system resolver selection that can still feed native
// ndnproxy after the Split-owned LAN_IP:53 pointer is removed.
//
// Passive resolver definitions and interface/WAN hints are deliberately NOT
// sufficient evidence. dns-proxy tls/https/dns53 upstream lines are profile
// definitions, while interface-scoped name-servers / ip dhcp client dns-routes
// describe interface capabilities or learned-DNS behaviour without proving that
// the system resolver will actually select a usable upstream after the Split
// pointer disappears. HOME v0.2.63/v0.2.64 demonstrated both false-positive
// classes: read-only plans reported readiness even though the prior native
// transition on the same baseline reached DNS-query failure.
//
// Only explicit global non-local name-server selections are accepted as proven
// readiness. Everything else gets either an exact previously snapshotted native
// selection or the transactional Yandex Basic safety fallback. Existing profile
// definitions, assignments and WAN DNS flags remain preserved.
func networkBridgeHasNativeResolverSelection(config, lanIP string) bool {
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
			address := networkBridgeNameServerAddress(line)
			if address != "" && !networkBridgeAddressMatchesLAN(address, lanIP) {
				return true
			}
		}
		if strings.HasPrefix(line, "ipv6 name-server ") {
			return true
		}
	}
	return false
}

func networkBridgeNativeResolverStatus(lanIP string) (string, error) {
	config, err := networkBridgeRunningConfig()
	if err != nil {
		return "", err
	}
	if networkBridgeHasNativeResolverSelection(config, lanIP) {
		return "existing-native-resolver-ready", nil
	}
	if snapshot, snapshotErr := networkBridgeLoadNativeResolverSelection(); snapshotErr == nil && len(snapshot) > 0 {
		return "native-resolver-snapshot-restore-needed", nil
	} else if snapshotErr != nil && !errors.Is(snapshotErr, os.ErrNotExist) {
		return "", snapshotErr
	}
	return "yandex-basic-fallback-needed", nil
}

func networkBridgeNameServerLinesForAddress(config, address string) []string {
	var lines []string
	for _, raw := range strings.Split(strings.ReplaceAll(config, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if networkBridgeNameServerAddress(line) == address {
			lines = append(lines, line)
		}
	}
	return lines
}

// Stage a deterministic native resolver before removing the Split-owned local
// pointer. Prefer the exact resolver selection snapshotted when entering Split;
// only use Yandex Basic when no verified native snapshot exists. Forward staging
// remains running-config only: persistence is owned by the full transaction after
// runtime acceptance.
func networkBridgeEnsureNativeResolverReady(lanIP string) ([]string, string, error) {
	config, err := networkBridgeRunningConfig()
	if err != nil {
		return nil, "", err
	}
	if networkBridgeHasNativeResolverSelection(config, lanIP) {
		return nil, "preserved-existing", nil
	}

	if snapshot, snapshotErr := networkBridgeLoadNativeResolverSelection(); snapshotErr == nil && len(snapshot) > 0 {
		added, addErr := networkBridgeAddResolverSelectionLines(snapshot)
		if addErr != nil {
			return nil, "", addErr
		}
		return added, "restored-native-snapshot", nil
	} else if snapshotErr != nil && !errors.Is(snapshotErr, os.ErrNotExist) {
		return nil, "", snapshotErr
	}

	fallbackLines := make([]string, 0, len(networkBridgeYandexBasicResolvers))
	for _, address := range networkBridgeYandexBasicResolvers {
		fallbackLines = append(fallbackLines, "ip name-server "+address)
	}
	added, addErr := networkBridgeAddResolverSelectionLines(fallbackLines)
	if addErr != nil {
		return nil, "", addErr
	}
	return added, "yandex-basic-fallback", nil
}

func networkBridgeRemoveAddedNativeResolvers(lines []string) error {
	return networkBridgeRemoveResolverSelectionLines(lines)
}

func networkBridgeRollbackNativeResolverStage(lanIP string, restoreLocal bool, added []string) error {
	if restoreLocal {
		if err := networkBridgeAddLocalPointer(lanIP); err != nil {
			return err
		}
	}
	if err := networkBridgeRemoveAddedNativeResolvers(added); err != nil {
		return err
	}
	return networkBridgeSave()
}

func augmentNetworkBridgeNativeResolverPlan(output, status string) string {
	values := parseNetworkBridgeValues(output)
	delta := values["EXPECTED_DELTA"]
	var extra string
	switch status {
	case "native-resolver-snapshot-restore-needed":
		extra = "restore exact snapshotted native Keenetic resolver selection before DNS acceptance"
	case "yandex-basic-fallback-needed":
		extra = "ensure native Yandex Basic resolver 77.88.8.8/77.88.8.1 before DNS acceptance"
	}
	if extra != "" {
		if delta == "" {
			delta = extra
		} else if !strings.Contains(delta, extra) {
			delta += "; " + extra
		}
	}
	out := strings.TrimRight(output, "\r\n")
	if delta != "" {
		out += "\nEXPECTED_DELTA=" + delta
	}
	out += "\nNATIVE_RESOLVER_SELECTION=" + status + "\n"
	return out
}
