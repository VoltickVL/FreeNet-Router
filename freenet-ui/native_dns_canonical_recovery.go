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

	expected, err := expectedFreeNetManagedSplitDNS(routing)
	if err != nil {
		return nil, err
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

func expectedFreeNetManagedSplitDNS(routing map[string]any) (map[string]any, error) {
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
			servers = append(servers, map[string]any{
				"address":      "77.88.8.8",
				"port":         float64(53),
				"domains":      domains,
				"skipFallback": true,
				"finalQuery":    true,
				"tag":           "dns-direct",
			})
			continue
		}
		servers = append(servers, map[string]any{
			"address":      "https://8.8.8.8/dns-query",
			"domains":      domains,
			"skipFallback": true,
			"finalQuery":    true,
			"tag":           "dns-vless",
		})
	}
	servers = append(servers, map[string]any{
		"address":    "https://8.8.8.8/dns-query",
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
