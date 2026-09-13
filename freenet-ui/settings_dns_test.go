package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsDNSProviderCatalogUsesAcceptedDoHEndpoints(t *testing.T) {
	options := settingsDNSProviderOptions()
	if len(options) != 2 {
		t.Fatalf("expected exactly two accepted DNS providers, got %d", len(options))
	}
	if got := settingsDNSProviderEndpoint(settingsDNSProviderYandex); got != "https://dns.yandex.ru/dns-query" {
		t.Fatalf("unexpected Yandex DoH endpoint %q", got)
	}
	if got := settingsDNSProviderEndpoint(settingsDNSProviderGoogle); got != "https://dns.google/dns-query" {
		t.Fatalf("unexpected Google DoH endpoint %q", got)
	}
}

func TestSettingsDNSDefaultsAreYandexDirectGoogleVPN(t *testing.T) {
	if settingsDNSDirectProviderYandex != settingsDNSProviderYandex {
		t.Fatalf("DIRECT DNS default must be Yandex DoH")
	}
	if settingsDNSVPNProviderGoogle != settingsDNSProviderGoogle {
		t.Fatalf("VPN DNS default must be Google DoH")
	}
}

func TestReadSettingsSplitDNSRuntimeRecognizesAcceptedPair(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_XRAY_CONFIG_DIR", dir)
	content := `{"dns":{"servers":[{"address":"https://dns.yandex.ru/dns-query","tag":"dns-direct"},{"address":"https://dns.google/dns-query","tag":"dns-vless"}]}}`
	if err := os.WriteFile(filepath.Join(dir, "02_dns.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	direct, vpn, known := readSettingsSplitDNSRuntime()
	if !known || direct != settingsDNSProviderYandex || vpn != settingsDNSProviderGoogle {
		t.Fatalf("unexpected runtime direct=%q vpn=%q known=%v", direct, vpn, known)
	}
}

func TestReadSettingsSplitDNSRuntimeFailsClosedForLegacyResolvers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_XRAY_CONFIG_DIR", dir)
	content := `{"dns":{"servers":[{"address":"77.88.8.8","tag":"dns-direct"},{"address":"https://8.8.8.8/dns-query","tag":"dns-vless"}]}}`
	if err := os.WriteFile(filepath.Join(dir, "02_dns.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, known := readSettingsSplitDNSRuntime(); known {
		t.Fatal("legacy resolver pair must not be presented as accepted DoH state")
	}
}
