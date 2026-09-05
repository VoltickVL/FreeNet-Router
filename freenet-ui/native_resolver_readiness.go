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

// networkBridgeHasNativeResolverDefinition answers whether the current Keenetic
// configuration contains at least one resolver path that remains meaningful after
// the Split-owned LAN_IP:53 pointer is removed. Definitions are preserved; this
// check never rewrites DoT/DoH, filter profiles, client assignments or WAN flags.
func networkBridgeHasNativeResolverDefinition(config, lanIP string) bool {
	dnsProxyScope := false
	interfaceScope := false
	for _, raw := range strings.Split(strings.ReplaceAll(config, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		indented := len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t')
		if !indented {
			dnsProxyScope = false
			interfaceScope = false
			if line == "!" {
				continue
			}
			if line == "dns-proxy" {
				dnsProxyScope = true
				continue
			}
			if strings.HasPrefix(line, "interface ") {
				interfaceScope = true
				continue
			}
			if strings.HasPrefix(line, "dns-proxy ") {
				sub := strings.TrimSpace(strings.TrimPrefix(line, "dns-proxy "))
				if networkBridgeIsSecureResolverDefinition(sub) {
					return true
				}
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
		if dnsProxyScope && networkBridgeIsSecureResolverDefinition(line) {
			return true
		}
		if interfaceScope && (strings.HasPrefix(line, "name-servers ") || strings.HasPrefix(line, "ip dhcp client dns-routes")) {
			return true
		}
	}
	return false
}

func networkBridgeIsSecureResolverDefinition(line string) bool {
	return strings.HasPrefix(line, "tls upstream ") ||
		strings.HasPrefix(line, "https upstream ") ||
		strings.HasPrefix(line, "dns53 upstream ")
}

func networkBridgeNativeResolverStatus(lanIP string) (string, error) {
	config, err := networkBridgeRunningConfig()
	if err != nil {
		return "", err
	}
	if networkBridgeHasNativeResolverDefinition(config, lanIP) {
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
// pointer would otherwise leave no explicit native resolver definition at all.
// Yandex Basic uses numeric upstreams so native DNS readiness has no DNS bootstrap
// dependency. The forward change is intentionally not persisted here: the shell
// core owns the single save after full post-apply acceptance.
func networkBridgeEnsureNativeResolverReady(lanIP string) ([]string, error) {
	config, err := networkBridgeRunningConfig()
	if err != nil {
		return nil, err
	}
	if networkBridgeHasNativeResolverDefinition(config, lanIP) {
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
