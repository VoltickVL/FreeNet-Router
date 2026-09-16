package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func settingsDNSReadinessTestEnv(t *testing.T) (string, string, []byte, []string) {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "etc", "xray", "configs")
	assetDir := filepath.Join(root, "etc", "xray", "dat")
	backupDir := filepath.Join(root, "backups")
	binDir := filepath.Join(root, "sbin")
	for _, dir := range []string{configDir, assetDir, backupDir, binDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	xray := filepath.Join(binDir, "xray")
	if err := os.WriteFile(xray, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"dns":{"servers":[{"address":"77.88.8.8","port":53,"tag":"dns-direct"},{"address":"https://8.8.8.8/dns-query","tag":"dns-vless"}]}}`)
	dnsFile := filepath.Join(configDir, "02_dns.json")
	if err := os.WriteFile(dnsFile, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(),
		"FREENET_ROOT="+root,
		"FREENET_XRAY_CONFIG_DIR="+configDir,
		"FREENET_XRAY_BIN="+xray,
		"FREENET_XRAY_ASSET_DIR="+assetDir,
		"FREENET_SETTINGS_DNS_BACKUP_ROOT="+backupDir,
		"FREENET_SETTINGS_DNS_TEST_MODE=yes",
		"FREENET_SETTINGS_DNS_READY_TIMEOUT=4",
		"FREENET_SETTINGS_DNS_READY_INTERVAL=1",
	)
	return root, dnsFile, legacy, env
}

func runSettingsDNSScript(t *testing.T, env []string, script string, args ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Env = env
	return cmd.CombinedOutput()
}

func TestSettingsDNSApplyWaitsForDelayedResolverReadiness(t *testing.T) {
	root, _, _, env := settingsDNSReadinessTestEnv(t)
	state := filepath.Join(root, "apply-query-count")
	env = append(env,
		"FREENET_SETTINGS_DNS_TEST_QUERY=delayed",
		"FREENET_SETTINGS_DNS_TEST_QUERY_STATE="+state,
		"FREENET_SETTINGS_DNS_TEST_QUERY_SUCCEED_AFTER=3",
	)
	output, err := runSettingsDNSScript(t, env, "settings_dns_apply.sh", "apply", "yandex-doh", "google-doh")
	if err != nil {
		t.Fatalf("delayed resolver readiness must converge: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte("RESULT=SUCCESS")) || !bytes.Contains(output, []byte("ROLLBACK=NOT_NEEDED")) {
		t.Fatalf("unexpected apply result: %s", output)
	}
	data, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	attempts, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || attempts < 3 {
		t.Fatalf("readiness was not retried: %q", data)
	}
}

func TestSettingsDNSRestoreWaitsForDelayedResolverReadiness(t *testing.T) {
	root, dnsFile, legacy, env := settingsDNSReadinessTestEnv(t)
	backup := filepath.Join(root, "resolver-before.json")
	if err := os.WriteFile(backup, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	accepted := []byte(`{"dns":{"servers":[{"address":"https://dns.google/dns-query","tag":"dns-direct"},{"address":"https://dns.google/dns-query","tag":"dns-vless"}]}}`)
	if err := os.WriteFile(dnsFile, accepted, 0600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "restore-query-count")
	env = append(env,
		"FREENET_SETTINGS_DNS_TEST_QUERY=delayed",
		"FREENET_SETTINGS_DNS_TEST_QUERY_STATE="+state,
		"FREENET_SETTINGS_DNS_TEST_QUERY_SUCCEED_AFTER=3",
	)
	output, err := runSettingsDNSScript(t, env, "settings_dns_restore.sh", backup)
	if err != nil {
		t.Fatalf("delayed rollback readiness must converge: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte("RESULT=RESTORED")) || !bytes.Contains(output, []byte("ROLLBACK=SUCCESS")) {
		t.Fatalf("unexpected restore result: %s", output)
	}
	if after, err := os.ReadFile(dnsFile); err != nil || !bytes.Equal(after, legacy) {
		t.Fatal("resolver rollback was not byte-exact")
	}
}

func TestSettingsDNSRollbackRemainsUnknownWhenDNSNeverRecovers(t *testing.T) {
	_, _, _, env := settingsDNSReadinessTestEnv(t)
	env = append(env,
		"FREENET_SETTINGS_DNS_TEST_QUERY=fail",
		"FREENET_SETTINGS_DNS_READY_TIMEOUT=1",
		"FREENET_SETTINGS_DNS_READY_INTERVAL=1",
	)
	output, err := runSettingsDNSScript(t, env, "settings_dns_apply.sh", "apply", "google-doh", "google-doh")
	if err == nil {
		t.Fatalf("permanently unavailable DNS unexpectedly accepted: %s", output)
	}
	text := string(output)
	if !strings.Contains(text, "PRIMARY ERROR: post-apply resolver acceptance failed") || !strings.Contains(text, "ROLLBACK ERROR/STATE: FAILED/UNKNOWN") {
		t.Fatalf("strict rollback classification lost: %s", output)
	}
}
