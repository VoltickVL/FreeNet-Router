package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsDNSRuntimeStateClassifiesAcceptedAndLegacy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_XRAY_CONFIG_DIR", dir)
	path := filepath.Join(dir, "02_dns.json")

	accepted := `{"dns":{"servers":[{"address":"https://dns.yandex.ru/dns-query","tag":"dns-direct"},{"address":"https://dns.google/dns-query","tag":"dns-vless"}]}}`
	if err := os.WriteFile(path, []byte(accepted), 0600); err != nil { t.Fatal(err) }
	direct, vpn, state := settingsDNSRuntimeState()
	if state != "accepted" || direct != "yandex-doh" || vpn != "google-doh" {
		t.Fatalf("accepted pair direct=%q vpn=%q state=%q", direct, vpn, state)
	}

	legacy := `{"dns":{"servers":[{"address":"77.88.8.8","port":53,"tag":"dns-direct"},{"address":"https://8.8.8.8/dns-query","tag":"dns-vless"}]}}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil { t.Fatal(err) }
	if _, _, state := settingsDNSRuntimeState(); state != "legacy" {
		t.Fatalf("legacy pair classified as %q", state)
	}

	unknown := `{"dns":{"servers":[{"address":"1.1.1.1","tag":"dns-direct"},{"address":"https://dns.google/dns-query","tag":"dns-vless"}]}}`
	if err := os.WriteFile(path, []byte(unknown), 0600); err != nil { t.Fatal(err) }
	if _, _, state := settingsDNSRuntimeState(); state != "unknown" {
		t.Fatalf("unknown pair classified as %q", state)
	}
}

func TestWriteSettingsDNSProviderKeysPreservesOtherConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "freenet.conf")
	before := "ISP_ID=rostelecom\nDNS_MODE=xkeen\nKEEP_ME=preserved\nSPLIT_DIRECT_DNS_PROVIDER=google-doh\n"
	if err := os.WriteFile(path, []byte(before), 0600); err != nil { t.Fatal(err) }
	if err := writeSettingsDNSProviderKeys(path, "yandex-doh", "google-doh"); err != nil { t.Fatal(err) }
	data, err := os.ReadFile(path); if err != nil { t.Fatal(err) }
	text := string(data)
	for _, want := range []string{"ISP_ID=rostelecom", "DNS_MODE=xkeen", "KEEP_ME=preserved", "SPLIT_DIRECT_DNS_PROVIDER=yandex-doh", "SPLIT_VPN_DNS_PROVIDER=google-doh"} {
		if !strings.Contains(text, want) { t.Fatalf("config missing %q: %s", want, text) }
	}
}

func TestSettingsDNSRuntimeHelpersKeepTransactionalContract(t *testing.T) {
	apply, err := os.ReadFile("settings_dns_apply.sh"); if err != nil { t.Fatal(err) }
	restore, err := os.ReadFile("settings_dns_restore.sh"); if err != nil { t.Fatal(err) }
	applyText, restoreText := string(apply), string(restore)
	for _, want := range []string{
		"https://dns.yandex.ru/dns-query",
		"https://dns.google/dns-query",
		"активная Split DNS resolver-схема неизвестна; STOP",
		"xray-test.log",
		"snapshot",
		"ROLLBACK ERROR/STATE: FAILED/UNKNOWN",
		"post-apply resolver acceptance failed",
	} {
		if !strings.Contains(applyText, want) { t.Fatalf("apply helper missing %q", want) }
	}
	for _, want := range []string{"RESULT=RESTORED", "ROLLBACK=SUCCESS", "resolver restore acceptance failed"} {
		if !strings.Contains(restoreText, want) { t.Fatalf("restore helper missing %q", want) }
	}
	for _, text := range []string{applyText, restoreText} {
		if strings.Contains(text, "vless://") || strings.Contains(text, "shortId") || strings.Contains(text, "publicKey") {
			t.Fatal("DNS helper must not contain VPN credentials")
		}
	}
}

func TestSettingsDNSUIContractMatchesAcceptedRender(t *testing.T) {
	data, err := os.ReadFile("web/settings-dns-ui.js"); if err != nil { t.Fatal(err) }
	js := string(data)
	for _, want := range []string{
		"Режим DNS",
		"DNS через роутер",
		"Раздельный DNS",
		"DIRECT DNS",
		"VPN DNS",
		"Яндекс DoH",
		"Google DoH",
		"/api/settings-v3/dns/control",
		"flag-icon flag-${code}",
		"backgroundImage",
	} {
		if !strings.Contains(js, want) { t.Fatalf("DNS UI missing %q", want) }
	}
	if strings.Contains(js, "Кастомный") || strings.Contains(js, "Custom DNS") {
		t.Fatal("accepted DNS UI must not expose a Custom mode")
	}
}

func TestCanonicalIndexDeliversSettingsDNSUI(t *testing.T) {
	raw, err := webFS.ReadFile("web/index.html"); if err != nil { t.Fatal(err) }
	html, err := canonicalizeControlCenterIndex(string(raw)); if err != nil { t.Fatal(err) }
	if !strings.Contains(html, `/api/settings-v3/assets/dns-ui.js`) {
		t.Fatal("canonical Settings DNS UI asset is not delivered")
	}
}
