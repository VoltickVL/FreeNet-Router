package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDetectCountry(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "filter")
	cases := map[string]string{
		"Frankfurt|Germany|Германия": "de",
		"Warsaw|Poland|Польша":      "pl",
		"Helsinki|Finland":          "fi",
		"Amsterdam|Netherlands":     "nl",
	}
	for input, want := range cases {
		if err := os.WriteFile(p, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		if got := detectCountry(p); got != want {
			t.Fatalf("detectCountry(%q)=%q want %q", input, got, want)
		}
	}
}

func TestReadOutbound(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "04_outbounds.json")
	data := `{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"1.2.3.4","port":443}]}},{"tag":"direct"},{"tag":"dns-out"}]}`
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	endpoint, dns := readOutbound(p)
	if endpoint != "1.2.3.4:443" {
		t.Fatalf("endpoint=%q", endpoint)
	}
	if !dns {
		t.Fatal("dns-out should be present")
	}
}

func TestNetworkProfileConfigRoundTripPreservesOtherSettings(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "freenet.conf")
	before := "UI_PORT=1001\nAUTO_ENDPOINT_UPDATE=yes\nISP_ID=auto\nDNS_MODE=auto\n"
	if err := os.WriteFile(p, []byte(before), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeNetworkProfileConfig(p, "rostelecom", "firmware"); err != nil {
		t.Fatal(err)
	}
	isp, dnsMode := readNetworkProfileConfig(p)
	if isp != "rostelecom" || dnsMode != "firmware" {
		t.Fatalf("got isp=%q dns=%q", isp, dnsMode)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, keep := range []string{"UI_PORT=1001", "AUTO_ENDPOINT_UPDATE=yes"} {
		if !strings.Contains(text, keep) {
			t.Fatalf("config lost unrelated setting %q: %s", keep, text)
		}
	}
}

func TestNetworkProfileDefaultsAndIndependentISPRecords(t *testing.T) {
	isp, dnsMode := readNetworkProfileConfig(filepath.Join(t.TempDir(), "missing.conf"))
	if isp != "auto" || dnsMode != "auto" {
		t.Fatalf("defaults isp=%q dns=%q", isp, dnsMode)
	}

	// Безопасная продуктовая рекомендация одинакова для любого ISP:
	// штатный DNS Keenetic. Split DNS остаётся только явным выбором пользователя.
	for _, id := range []string{"auto", "vladlink", "alliancetelecom", "rostelecom", "podryad", "custom"} {
		meta, ok := ispProfiles[id]
		if !ok {
			t.Fatalf("missing ISP record %q", id)
		}
		if meta.RecommendedDNSMode != "firmware" {
			t.Fatalf("ISP %q mode=%q want firmware", id, meta.RecommendedDNSMode)
		}
	}
	if ispProfiles["vladlink"].Label == ispProfiles["alliancetelecom"].Label {
		t.Fatal("Vladlink and AllianceTelecom must remain separate records")
	}
	if ispProfiles["rostelecom"].Label == ispProfiles["podryad"].Label {
		t.Fatal("Rostelecom and Podryad must remain separate records")
	}
	if dnsModes["auto"] != "Авто (штатный DNS)" {
		t.Fatalf("auto DNS label=%q", dnsModes["auto"])
	}
	if dnsModes["xkeen"] != "Split DNS через VPN (XKeen/Xray)" {
		t.Fatalf("xkeen DNS label=%q", dnsModes["xkeen"])
	}
}

func TestSubscriptionURLValidationAndStorage(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "xray", "blanc_subscription.url")
	secret := "https://example.com/sub?token=SECRET_TEST_VALUE"
	if err := writeSubscriptionURL(p, secret); err != nil {
		t.Fatal(err)
	}
	if !subscriptionConfigured(p) {
		t.Fatal("subscription should be configured")
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("subscription mode=%o want 600", got)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(b)) != secret {
		t.Fatal("stored subscription differs")
	}
	for _, invalid := range []string{"", "http://example.com/sub", "https://", "https://user:pass@example.com/sub", "https://example.com/a b"} {
		if validateSubscriptionURL(invalid) == nil {
			t.Fatalf("invalid URL accepted: %q", invalid)
		}
	}
}

func TestSubscriptionResponseNeverContainsURL(t *testing.T) {
	b, err := json.Marshal(subscriptionResponse{Success: true, Configured: true, Message: "Подписка настроена"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "http://") || strings.Contains(text, "https://") || strings.Contains(text, "SECRET_TEST_VALUE") {
		t.Fatalf("subscription response exposes URL material: %s", text)
	}
}

func TestSameOrigin(t *testing.T) {
	r := httptest.NewRequest("POST", "http://192.168.50.1:1001/api/action", nil)
	r.Host = "192.168.50.1:1001"
	r.Header.Set("Origin", "http://192.168.50.1:1001")
	if !sameOrigin(r) {
		t.Fatal("same origin rejected")
	}
	r.Header.Set("Origin", "https://evil.example")
	if sameOrigin(r) {
		t.Fatal("cross origin accepted")
	}
}

func TestLoopbackListenAddr(t *testing.T) {
	if got := loopbackListenAddr("192.168.50.1:1001"); got != "127.0.0.1:1001" {
		t.Fatalf("loopbackListenAddr()=%q", got)
	}
	for _, addr := range []string{"127.0.0.1:1001", "0.0.0.0:1001", "[::]:1001"} {
		if got := loopbackListenAddr(addr); got != "" {
			t.Fatalf("loopbackListenAddr(%q)=%q want empty", addr, got)
		}
	}
}

func TestRunCommandDoesNotHangOnInheritedOutputPipe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	started := time.Now()
	out, err := runCommand(ctx, "/bin/sh", "-c", "(sleep 5; echo late) & echo done")
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("runCommand error: %v", err)
	}
	if elapsed >= 4*time.Second {
		t.Fatalf("runCommand waited for descendant output pipe: %s", elapsed)
	}
	if len(out) == 0 {
		t.Fatal("expected direct command output")
	}
}

func TestStatusJSONDoesNotExposeSecrets(t *testing.T) {
	s := statusResponse{Version: "0.2.1", CountryCode: "de", Country: "Германия", City: "Frankfurt", Endpoint: "1.2.3.4:443", ISP: "vladlink", DNSMode: "xkeen", SubscriptionConfigured: true}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, forbidden := range []string{"uuid", "publicKey", "shortId", "subscription_url", "vless://"} {
		if containsInsensitive(text, forbidden) {
			t.Fatalf("status JSON contains forbidden key/material %q", forbidden)
		}
	}
}

func containsInsensitive(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || containsFold(s, sub))
}

func containsFold(s, sub string) bool {
	ls, lsub := []rune(s), []rune(sub)
	for i := 0; i+len(lsub) <= len(ls); i++ {
		ok := true
		for j := range lsub {
			a, b := ls[i+j], lsub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
