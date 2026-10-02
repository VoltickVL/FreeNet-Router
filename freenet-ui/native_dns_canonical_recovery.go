package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
)

var canonicalLegacyNativeDNS = []byte("{}\n")

func recoverCanonicalLegacyNativeDNSFromManagedSplit(backupErr error) error {
	if backupErr == nil {
		return nil
	}
	if !legacyNativeBackupUnavailable(backupErr) {
		return backupErr
	}

	dnsData, err := canonicalNativeDNSFromCurrentManagedSplit()
	if err != nil {
		return fmt.Errorf("%v; canonical native fallback недоступен: %w", backupErr, err)
	}
	if err := validateRecoveredNativeDNSCandidate(dnsData); err != nil {
		return fmt.Errorf("%v; canonical neutral native 02_dns не прошёл Xray candidate validation", backupErr)
	}
	if err := persistRecoveredNativeDNS(dnsData); err != nil {
		return err
	}
	return nil
}

func legacyNativeBackupUnavailable(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "не найден historical network backup для восстановления native 02_dns") ||
		strings.Contains(message, "не найден однозначный native network backup для восстановления 02_dns")
}

func canonicalNativeDNSFromCurrentManagedSplit() ([]byte, error) {
	configDir := legacyNativeConfigDir()
	currentDNS, err := legacyNativeReadJSONObject(filepath.Join(configDir, "02_dns.json"))
	if err != nil {
		return nil, errors.New("не удалось прочитать current Split 02_dns")
	}
	routing, err := legacyNativeReadJSONObject(filepath.Join(configDir, "05_routing.json"))
	if err != nil {
		return nil, errors.New("не удалось прочитать current Split routing")
	}

	expected, err := expectedFreeNetManagedSplitDNSForCurrent(routing, currentDNS)
	if err != nil {
		return nil, fmt.Errorf("current 02_dns не совпадает с детерминированным FreeNet-managed Split; STOP без догадки: %w", err)
	}
	if !reflect.DeepEqual(currentDNS, expected) {
		return nil, errors.New("current 02_dns не совпадает с детерминированным FreeNet-managed Split; STOP без догадки")
	}

	// In native mode Keenetic/ndnproxy owns :53. The transactional shell removes
	// the Xray :53 inbound, dns-out and DNS-only routing before enabling native
	// DNS. Therefore an empty Xray DNS fragment is the canonical neutral baseline,
	// not a guessed historical resolver configuration. The complete stripped
	// candidate is validated by the router's own Xray before this baseline is
	// persisted, and the live apply still has its normal backup/acceptance/rollback.
	return append([]byte(nil), canonicalLegacyNativeDNS...), nil
}

func managedSplitResolverPair(currentDNS map[string]any) (direct, vpn string, err error) {
	dnsObj, ok := currentDNS["dns"].(map[string]any)
	if !ok {
		return "", "", errors.New("current Split DNS object отсутствует")
	}
	servers, ok := dnsObj["servers"].([]any)
	if !ok || len(servers) == 0 {
		return "", "", errors.New("current Split DNS servers отсутствуют")
	}
	for _, raw := range servers {
		server, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		tag := legacyNativeString(server["tag"])
		address := legacyNativeString(server["address"])
		if address == "" {
			continue
		}
		switch tag {
		case "dns-direct":
			if direct != "" && direct != address {
				return "", "", errors.New("current Split dns-direct содержит несколько resolver endpoints")
			}
			direct = address
		case "dns-vless":
			if vpn != "" && vpn != address {
				return "", "", errors.New("current Split dns-vless содержит несколько resolver endpoints")
			}
			vpn = address
		}
	}
	if vpn == "" {
		return "", "", errors.New("current Split dns-vless resolver отсутствует")
	}
	if direct == "" {
		if vpn == "https://8.8.8.8/dns-query" {
			return "77.88.8.8", vpn, nil
		}
		if settingsDNSProviderFromEndpoint(vpn) != "" {
			return settingsDNSYandexDoH, vpn, nil
		}
		return "", "", errors.New("current Split vpn resolver не принадлежит поддерживаемому FreeNet catalog")
	}
	if direct == "77.88.8.8" && vpn == "https://8.8.8.8/dns-query" {
		return direct, vpn, nil
	}
	if settingsDNSProviderFromEndpoint(direct) == "" || settingsDNSProviderFromEndpoint(vpn) == "" {
		return "", "", errors.New("current Split resolver pair не принадлежит поддерживаемому FreeNet catalog")
	}
	return direct, vpn, nil
}

func expectedFreeNetManagedSplitDNSForCurrent(routing, currentDNS map[string]any) (map[string]any, error) {
	direct, vpn, err := managedSplitResolverPair(currentDNS)
	if err != nil {
		return nil, err
	}
	return expectedFreeNetManagedSplitDNSWithResolvers(routing, direct, vpn)
}

func expectedFreeNetManagedSplitDNS(routing map[string]any) (map[string]any, error) {
	return expectedFreeNetManagedSplitDNSWithResolvers(routing, "77.88.8.8", "https://8.8.8.8/dns-query")
}

func expectedFreeNetManagedSplitDNSWithResolvers(routing map[string]any, directAddress, vpnAddress string) (map[string]any, error) {
	routingObj, ok := routing["routing"].(map[string]any)
	if !ok {
		return nil, errors.New("current Split routing не содержит routing object")
	}
	rules, err := legacyNativeObjectSlice(routingObj, "rules")
	if err != nil {
		return nil, err
	}

	servers := make([]any, 0, len(rules)+1)
	for _, rule := range rules {
		domains, ok := rule["domain"].([]any)
		if !ok || len(domains) == 0 {
			continue
		}
		if legacyNativeString(rule["outboundTag"]) == "direct" {
			server := map[string]any{
				"address":      directAddress,
				"domains":      domains,
				"skipFallback": true,
				"finalQuery":    true,
				"tag":           "dns-direct",
			}
			if directAddress == "77.88.8.8" {
				server["port"] = float64(53)
			}
			servers = append(servers, server)
			continue
		}
		servers = append(servers, map[string]any{
			"address":      vpnAddress,
			"domains":      domains,
			"skipFallback": true,
			"finalQuery":    true,
			"tag":           "dns-vless",
		})
	}
	servers = append(servers, map[string]any{
		"address":    vpnAddress,
		"tag":        "dns-vless",
		"finalQuery": true,
	})

	return map[string]any{
		"dns": map[string]any{
			"tag":           "dns-vless",
			"servers":       servers,
			"queryStrategy": "UseIPv4",
		},
	}, nil
}
