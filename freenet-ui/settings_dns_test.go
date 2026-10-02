package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsDNSProviderCatalogIsLegAware(t *testing.T) {
	direct := settingsDNSDirectProviderOptions()
	vpn := settingsDNSVPNProviderOptions()
	if len(direct) != 2 || len(vpn) != 2 {
		t.Fatalf("expected two providers per DNS leg, direct=%d vpn=%d", len(direct), len(vpn))
	}
	if got := settingsDNSDirectProviderEndpoint(settingsDNSProviderYandex); got != settingsDNSYandexDirect {
		t.Fatalf("unexpected Yandex DIRECT endpoint %q", got)
	}
	if got := settingsDNSDirectProviderEndpoint(settingsDNSProviderGoogle); got != settingsDNSGoogleDirect {
		t.Fatalf("unexpected Google DIRECT endpoint %q", got)
	}
	if got := settingsDNSVPNProviderEndpoint(settingsDNSProviderYandex); got != settingsDNSYandexDoH {
		t.Fatalf("unexpected Yandex VPN DoH endpoint %q", got)
	}
	if got := settingsDNSVPNProviderEndpoint(settingsDNSProviderGoogle); got != settingsDNSGoogleDoH {
		t.Fatalf("unexpected Google VPN DoH endpoint %q", got)
	}
}

func TestSettingsDNSDefaultsAreYandexDirectGoogleVPN(t *testing.T) {
	if settingsDNSDirectProviderYandex != settingsDNSProviderYandex {
		t.Fatalf("DIRECT DNS default must be Yandex provider")
	}
	if settingsDNSVPNProviderGoogle != settingsDNSProviderGoogle {
		t.Fatalf("VPN DNS default must be Google DoH")
	}
}

func TestReadSettingsSplitDNSRuntimeRecognizesBootstrapSafePair(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_XRAY_CONFIG_DIR", dir)
	content := `{"dns":{"servers":[{"address":"77.88.8.8","port":53,"tag":"dns-direct"},{"address":"https://dns.google/dns-query","tag":"dns-vless"}]}}`
	if err := os.WriteFile(filepath.Join(dir, "02_dns.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	direct, vpn, known := readSettingsSplitDNSRuntime()
	if !known || direct != settingsDNSProviderYandex || vpn != settingsDNSProviderGoogle {
		t.Fatalf("unexpected runtime direct=%q vpn=%q known=%v", direct, vpn, known)
	}
}

func TestReadSettingsSplitDNSRuntimeRejectsHostnameDoHOnDirectLeg(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_XRAY_CONFIG_DIR", dir)
	content := `{"dns":{"servers":[{"address":"https://dns.yandex.ru/dns-query","tag":"dns-direct"},{"address":"https://dns.google/dns-query","tag":"dns-vless"}]}}`
	if err := os.WriteFile(filepath.Join(dir, "02_dns.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, known := readSettingsSplitDNSRuntime(); known {
		t.Fatal("hostname DoH on DIRECT leg must not be accepted as bootstrap-safe runtime")
	}
}

func TestReadSettingsSplitDNSRuntimeRecognizesHistoricalIPLiteralDirectPair(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_XRAY_CONFIG_DIR", dir)
	content := `{"dns":{"servers":[{"address":"77.88.8.8","port":53,"tag":"dns-direct"},{"address":"https://8.8.8.8/dns-query","tag":"dns-vless"}]}}`
	if err := os.WriteFile(filepath.Join(dir, "02_dns.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	direct, vpn, known := readSettingsSplitDNSRuntime()
	if !known || direct != settingsDNSProviderYandex || vpn != settingsDNSProviderGoogle {
		t.Fatalf("historical bootstrap-safe pair direct=%q vpn=%q known=%v", direct, vpn, known)
	}
}
