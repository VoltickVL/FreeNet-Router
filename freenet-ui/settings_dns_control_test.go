package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestSettingsDNSDirectEgressStateClassifiesAcceptedRepairableAndUnknown(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_XRAY_CONFIG_DIR", dir)
	path := filepath.Join(dir, "04_outbounds.json")

	accepted := `{"outbounds":[{"tag":"direct","protocol":"freedom","streamSettings":{"sockopt":{"mark":255,"tcpFastOpen":true}}}]}`
	if err := os.WriteFile(path, []byte(accepted), 0600); err != nil { t.Fatal(err) }
	if got := settingsDNSDirectEgressState(); got != "accepted" {
		t.Fatalf("marked DIRECT classified as %q", got)
	}

	repairable := `{"outbounds":[{"tag":"direct","protocol":"freedom","streamSettings":{"sockopt":{"tcpFastOpen":true}}}]}`
	if err := os.WriteFile(path, []byte(repairable), 0600); err != nil { t.Fatal(err) }
	if got := settingsDNSDirectEgressState(); got != "repairable" {
		t.Fatalf("unmarked DIRECT classified as %q", got)
	}

	unknown := `{"outbounds":[{"tag":"direct","protocol":"freedom"},{"tag":"direct","protocol":"freedom"}]}`
	if err := os.WriteFile(path, []byte(unknown), 0600); err != nil { t.Fatal(err) }
	if got := settingsDNSDirectEgressState(); got != "unknown" {
		t.Fatalf("ambiguous DIRECT classified as %q", got)
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

func TestSettingsDNSOperationFenceExcludesAutoHealthAndReleases(t *testing.T) {
	t.Setenv("FREENET_AUTO_HEALTH_LOCK", filepath.Join(t.TempDir(), "auto-health.lock"))
	a := &app{sem: make(chan struct{}, 1)}
	release, reason := acquireSettingsDNSOperation(context.Background(), a, time.Second)
	if release == nil || reason != "" {
		t.Fatalf("DNS guard acquisition failed: release=%v reason=%q", release != nil, reason)
	}
	if len(a.sem) != 1 {
		t.Fatalf("web operation semaphore=%d want=1", len(a.sem))
	}
	if autoRelease, err := acquireAutomationHealthLock(); err == nil {
		autoRelease()
		t.Fatal("DNS operation must hold the cross-process AUTO health fence")
	}
	other := &app{sem: make(chan struct{}, 1)}
	if secondRelease, secondReason := acquireSettingsDNSOperation(context.Background(), other, 10*time.Millisecond); secondRelease != nil || secondReason != "AUTO VPN health/recovery operation did not finish before DNS deadline" {
		if secondRelease != nil { secondRelease() }
		t.Fatalf("parallel DNS/AUTO guard result release=%v reason=%q", secondRelease != nil, secondReason)
	}
	release()
	if len(a.sem) != 0 {
		t.Fatal("web operation semaphore leaked after DNS guard release")
	}
	autoRelease, err := acquireAutomationHealthLock()
	if err != nil {
		t.Fatalf("AUTO health fence leaked after DNS guard release: %v", err)
	}
	autoRelease()
}

func TestSettingsDNSOperationWaitsForActiveAutoHealthFence(t *testing.T) {
	t.Setenv("FREENET_AUTO_HEALTH_LOCK", filepath.Join(t.TempDir(), "auto-health.lock"))
	autoRelease, err := acquireAutomationHealthLock()
	if err != nil {
		t.Fatalf("AUTO health fixture acquire failed: %v", err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(30 * time.Millisecond)
		autoRelease()
		close(released)
	}()

	a := &app{sem: make(chan struct{}, 1)}
	started := time.Now()
	release, reason := acquireSettingsDNSOperation(context.Background(), a, 500*time.Millisecond)
	<-released
	if release == nil || reason != "" {
		if release != nil {
			release()
		}
		t.Fatalf("DNS guard did not wait for transient AUTO health fence: release=%v reason=%q", release != nil, reason)
	}
	if time.Since(started) < 25*time.Millisecond {
		release()
		t.Fatal("DNS guard bypassed an active AUTO health fence")
	}
	release()
}

func TestSettingsDNSOperationTimeoutReleasesCrossProcessFence(t *testing.T) {
	t.Setenv("FREENET_AUTO_HEALTH_LOCK", filepath.Join(t.TempDir(), "auto-health.lock"))
	a := &app{sem: make(chan struct{}, 1)}
	a.sem <- struct{}{}
	release, reason := acquireSettingsDNSOperation(context.Background(), a, 15*time.Millisecond)
	if release != nil || reason != "another confirmed FreeNet operation is active" {
		if release != nil { release() }
		t.Fatalf("blocked DNS guard release=%v reason=%q", release != nil, reason)
	}
	<-a.sem
	autoRelease, err := acquireAutomationHealthLock()
	if err != nil {
		t.Fatalf("filesystem fence leaked after web-sem timeout: %v", err)
	}
	autoRelease()
}

func TestSettingsDNSResolverApplyAndRestoreTestMode(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "etc", "xray", "configs")
	assetDir := filepath.Join(root, "etc", "xray", "dat")
	backupDir := filepath.Join(root, "backups")
	binDir := filepath.Join(root, "sbin")
	for _, dir := range []string{configDir, assetDir, backupDir, binDir} {
		if err := os.MkdirAll(dir, 0755); err != nil { t.Fatal(err) }
	}
	xray := filepath.Join(binDir, "xray")
	if err := os.WriteFile(xray, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil { t.Fatal(err) }
	dnsFile := filepath.Join(configDir, "02_dns.json")
	outFile := filepath.Join(configDir, "04_outbounds.json")
	legacy := []byte(`{"dns":{"servers":[{"address":"77.88.8.8","port":53,"domains":["domain:direct.example"],"tag":"dns-direct"},{"address":"https://8.8.8.8/dns-query","domains":["domain:vpn.example"],"tag":"dns-vless"},{"address":"https://bootstrap.example/dns-query","tag":"bootstrap"}],"queryStrategy":"UseIPv4"}}`)
	unmarkedOut := []byte(`{"outbounds":[{"tag":"vless-reality","protocol":"vless"},{"tag":"direct","protocol":"freedom","streamSettings":{"sockopt":{"tcpFastOpen":true}}},{"tag":"block","protocol":"blackhole"}]}`)
	if err := os.WriteFile(dnsFile, legacy, 0600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(outFile, unmarkedOut, 0600); err != nil { t.Fatal(err) }
	restoreDir := filepath.Join(root, "before")
	if err := os.MkdirAll(restoreDir, 0700); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(restoreDir, "02_dns.json"), legacy, 0600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(restoreDir, "04_outbounds.json"), unmarkedOut, 0600); err != nil { t.Fatal(err) }

	env := append(os.Environ(),
		"FREENET_ROOT="+root,
		"FREENET_XRAY_CONFIG_DIR="+configDir,
		"FREENET_XRAY_BIN="+xray,
		"FREENET_XRAY_ASSET_DIR="+assetDir,
		"FREENET_SETTINGS_DNS_BACKUP_ROOT="+backupDir,
		"FREENET_SETTINGS_DNS_TEST_MODE=yes",
	)
	run := func(script string, args ...string) ([]byte, error) {
		cmd := exec.Command("sh", append([]string{script}, args...)...)
		cmd.Env = env
		return cmd.CombinedOutput()
	}

	plan, err := run("settings_dns_apply.sh", "plan", "yandex-doh", "google-doh")
	if err != nil { t.Fatalf("resolver plan failed: %v\n%s", err, plan) }
	if !bytes.Contains(plan, []byte("MUTATION=NONE")) { t.Fatalf("plan is not read-only: %s", plan) }
	if after, err := os.ReadFile(dnsFile); err != nil || !bytes.Equal(after, legacy) { t.Fatal("resolver plan mutated 02_dns.json") }
	if after, err := os.ReadFile(outFile); err != nil || !bytes.Equal(after, unmarkedOut) { t.Fatal("resolver plan mutated 04_outbounds.json") }

	apply, err := run("settings_dns_apply.sh", "apply", "yandex-doh", "google-doh")
	if err != nil { t.Fatalf("resolver apply failed: %v\n%s", err, apply) }
	if !bytes.Contains(apply, []byte("RESULT=SUCCESS")) { t.Fatalf("resolver apply missing success: %s", apply) }
	data, err := os.ReadFile(dnsFile); if err != nil { t.Fatal(err) }
	text := string(data)
	for _, want := range []string{"https://dns.yandex.ru/dns-query", "https://dns.google/dns-query", "https://bootstrap.example/dns-query", "domain:direct.example", "domain:vpn.example"} {
		if !strings.Contains(text, want) { t.Fatalf("applied DNS config lost %q: %s", want, text) }
	}
	if strings.Contains(text, `"port": 53`) || strings.Contains(text, `"port":53`) { t.Fatalf("legacy direct UDP port survived DoH migration: %s", text) }

	outData, err := os.ReadFile(outFile); if err != nil { t.Fatal(err) }
	var outCfg struct { Outbounds []map[string]any `json:"outbounds"` }
	if err := json.Unmarshal(outData, &outCfg); err != nil { t.Fatal(err) }
	foundDirect := false
	for _, outbound := range outCfg.Outbounds {
		if outbound["tag"] != "direct" { continue }
		foundDirect = true
		stream, _ := outbound["streamSettings"].(map[string]any)
		sockopt, _ := stream["sockopt"].(map[string]any)
		if sockopt["mark"] != float64(255) { t.Fatalf("DIRECT self-mark not repaired: %s", outData) }
		if sockopt["tcpFastOpen"] != true { t.Fatalf("existing DIRECT sockopt lost: %s", outData) }
	}
	if !foundDirect { t.Fatal("DIRECT outbound disappeared") }

	restored, err := run("settings_dns_restore.sh", restoreDir)
	if err != nil { t.Fatalf("resolver restore failed: %v\n%s", err, restored) }
	if !bytes.Contains(restored, []byte("RESULT=RESTORED")) { t.Fatalf("restore result missing: %s", restored) }
	if after, err := os.ReadFile(dnsFile); err != nil || !bytes.Equal(after, legacy) { t.Fatal("resolver restore was not byte-exact") }
	if after, err := os.ReadFile(outFile); err != nil || !bytes.Equal(after, unmarkedOut) { t.Fatal("DIRECT egress restore was not byte-exact") }

	unknown := []byte(`{"dns":{"servers":[{"address":"1.1.1.1","tag":"dns-direct"},{"address":"https://dns.google/dns-query","tag":"dns-vless"}]}}`)
	if err := os.WriteFile(dnsFile, unknown, 0600); err != nil { t.Fatal(err) }
	failed, err := run("settings_dns_apply.sh", "apply", "yandex-doh", "google-doh")
	if err == nil { t.Fatalf("unknown resolver state unexpectedly applied: %s", failed) }
	if !bytes.Contains(failed, []byte("ROLLBACK ERROR/STATE: no live apply")) { t.Fatalf("unknown state not classified NOT_APPLIED: %s", failed) }
	if after, err := os.ReadFile(dnsFile); err != nil || !bytes.Equal(after, unknown) { t.Fatal("unknown resolver state was mutated") }
	if after, err := os.ReadFile(outFile); err != nil || !bytes.Equal(after, unmarkedOut) { t.Fatal("unknown resolver state mutated DIRECT egress") }
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
		"DIRECT egress",
		"mark:255",
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
		"Прямой",
		"Раздельный",
		"DIRECT DNS",
		"VPN DNS",
		"Яндекс DoH",
		"Google DoH",
		"/api/settings-v3/dns/control",
		"Восстановить DIRECT",
		"repair_required",
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
