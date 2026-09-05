package main

import (
	"errors"
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
// independent resolver selection that can still feed native ndnproxy after the
// Split-owned LAN_IP:53 pointer is removed.
//
// dns-proxy tls/https/dns53 upstream lines are deliberately NOT sufficient:
// they are preserved profile definitions, but their mere existence does not prove
// that the system resolver is currently selected to use them. HOME v0.2.63
// demonstrated this exact distinction: preserved Yandex DoT/DoH definitions were
// present while the only active global resolver selection was Split-local.
func networkBridgeHasNativeResolverSelection(config, lanIP string) bool {
	interfaceScope := false
	for _, raw := range strings.Split(strings.ReplaceAll(config, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		indented := len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t')
		if !indented {
			interfaceScope = false
			if line == "!" {
				continue
			}
			if strings.HasPrefix(line, "interface ") {
				interfaceScope = true
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
			continue
		}
		if interfaceScope && (strings.HasPrefix(line, "name-servers ") || strings.HasPrefix(line, "ip dhcp client dns-routes")) {
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

// Stage a deterministic native resolver only when removing the Split-owned local
// pointer would otherwise leave no independent active resolver selection.
// Preserved DoT/DoH/profile definitions are intentionally not treated as active
// selection. Yandex Basic uses numeric upstreams so native DNS readiness has no
// DNS bootstrap dependency. The forward change is intentionally not persisted
// here: the shell core owns the single save after full post-apply acceptance.
func networkBridgeEnsureNativeResolverReady(lanIP string) ([]string, error) {
	config, err := networkBridgeRunningConfig()
	if err != nil {
		return nil, err
	}
	if networkBridgeHasNativeResolverSelection(config, lanIP) {
		return nil, nil
	}

	added := make([]string, 0, len(networkBridgeYandexBasicResolvers))
	for _, address := range networkBridgeYandexBasicResolvers {
		if err := networkBridgeNDMC("ip name-server " + address); err != nil {
			_ = networkBridgeRemoveAddedNativeResolvers(added)
			return nil, err
		}
		updated, readErr := networkBridgeRunningConfig()
		if readErr != nil || len(networkBridgeNameServerLinesForAddress(updated, address)) == 0 {
			_ = networkBridgeRemoveAddedNativeResolvers(append(added, address))
			return nil, errors.New("native DNS fallback was not accepted by Keenetic")
		}
		added = append(added, address)
	}
	return added, nil
}

func networkBridgeRemoveAddedNativeResolvers(addresses []string) error {
	for i := len(addresses) - 1; i >= 0; i-- {
		address := addresses[i]
		config, err := networkBridgeRunningConfig()
		if err != nil {
			return err
		}
		lines := networkBridgeNameServerLinesForAddress(config, address)
		for _, line := range lines {
			if err := networkBridgeNDMC("no " + line); err != nil {
				return err
			}
		}
	}
	return nil
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
	if status == "yandex-basic-fallback-needed" {
		extra := "ensure native Yandex Basic resolver 77.88.8.8/77.88.8.1 before DNS acceptance"
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
