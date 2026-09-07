package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	nativeDNSProviderRouterCurrent = "router-current"
	nativeDNSProviderYandexBasic   = "yandex-basic"
)

type nativeDNSProviderOption struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Addresses []string `json:"addresses,omitempty"`
}

var nativeDNSProviderCatalog = []nativeDNSProviderOption{
	{ID: nativeDNSProviderRouterCurrent, Label: "Текущие DNS роутера"},
	{ID: nativeDNSProviderYandexBasic, Label: "Яндекс Basic", Addresses: []string{"77.88.8.8", "77.88.8.1"}},
}

func validNativeDNSProvider(provider string) bool {
	switch strings.TrimSpace(provider) {
	case nativeDNSProviderRouterCurrent, nativeDNSProviderYandexBasic:
		return true
	default:
		return false
	}
}

// Existing installations predate the provider selector and already converge
// Native DNS to Yandex Basic. Keep that behavior until the user explicitly
// chooses another provider so an upgrade never changes runtime by itself.
func readNativeDNSProvider(path string) string {
	provider := nativeDNSProviderYandexBasic
	b, err := os.ReadFile(path)
	if err != nil {
		return provider
	}
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "NATIVE_DNS_PROVIDER" {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "'\"")
		if validNativeDNSProvider(value) {
			return value
		}
	}
	return provider
}

func nativeDNSProviderOptions() []nativeDNSProviderOption {
	out := make([]nativeDNSProviderOption, 0, len(nativeDNSProviderCatalog))
	for _, option := range nativeDNSProviderCatalog {
		copyOption := option
		copyOption.Addresses = append([]string(nil), option.Addresses...)
		out = append(out, copyOption)
	}
	return out
}

// Commit ISP, DNS mode and the Native DNS provider in one atomic config write.
// Unknown configuration keys are preserved verbatim.
func writeNetworkProfileConfigWithNativeProvider(path, isp, dnsMode, provider string) error {
	if _, ok := ispProfiles[isp]; !ok {
		return errors.New("unsupported ISP")
	}
	if _, ok := dnsModes[dnsMode]; !ok {
		return errors.New("unsupported DNS mode")
	}
	provider = strings.TrimSpace(provider)
	if !validNativeDNSProvider(provider) {
		return errors.New("unsupported Native DNS provider")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	} else if !os.IsNotExist(err) {
		return err
	}

	seenISP := false
	seenDNS := false
	seenProvider := false
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "ISP_ID="):
			lines[i] = "ISP_ID=" + isp
			seenISP = true
		case strings.HasPrefix(line, "DNS_MODE="):
			lines[i] = "DNS_MODE=" + dnsMode
			seenDNS = true
		case strings.HasPrefix(line, "NATIVE_DNS_PROVIDER="):
			lines[i] = "NATIVE_DNS_PROVIDER=" + provider
			seenProvider = true
		}
	}
	if !seenISP {
		lines = append(lines, "ISP_ID="+isp)
	}
	if !seenDNS {
		lines = append(lines, "DNS_MODE="+dnsMode)
	}
	if !seenProvider {
		lines = append(lines, "NATIVE_DNS_PROVIDER="+provider)
	}
	return atomicWrite(path, []byte(strings.Join(lines, "\n")+"\n"), 0600)
}
