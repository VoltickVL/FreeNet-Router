package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var legacyHandoffAssetDestinations = map[string]string{
	"freenet-ui-arm64-v8a": "sbin/freenet-ui",
	"freenet": "bin/freenet",
	"vpn": "bin/vpn",
	"blanc_xkeen_update_outbounds.sh": "bin/blanc_xkeen_update_outbounds.sh",
	"migrate_split_dns.sh": "lib/freenet/migrate_split_dns.sh",
	"apply_network_profile.sh": "lib/freenet/apply_network_profile.sh",
	"apply_provider_profile.sh": "lib/freenet/apply_provider_profile.sh",
	"finalize_setup.sh": "lib/freenet/finalize_setup.sh",
	"bootstrap_entware.sh": "lib/freenet/bootstrap_entware.sh",
	"upstream-pins.env": "etc/freenet/upstream-pins.env",
	"self_update.sh": "lib/freenet/self_update.sh",
}

func legacyHandoffWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil { t.Fatal(err) }
	if err := os.WriteFile(path, data, 0700); err != nil { t.Fatal(err) }
}

func legacyHandoffFixture(t *testing.T, root, release, tag, updater string) map[string][]byte {
	t.Helper()
	before := map[string][]byte{}
	var manifest strings.Builder
	for name, dest := range legacyHandoffAssetDestinations {
		old := []byte("old application asset " + name + "\n")
		legacyHandoffWrite(t, filepath.Join(root, dest), old)
		before[dest] = old
		content := []byte("#!/bin/sh\nexit 0\n")
		if name == "self_update.sh" {
			var err error
			content, err = os.ReadFile(updater)
			if err != nil { t.Fatal(err) }
		}
		if name == "upstream-pins.env" { content = []byte("PIN_POLICY_VERSION=fixture\n") }
		legacyHandoffWrite(t, filepath.Join(release, name), content)
		fmt.Fprintf(&manifest, "%x  %s\n", sha256.Sum256(content), name)
	}
	meta := []byte(fmt.Sprintf(`{"tag_name":%q,"prerelease":%t,"draft":false,"body":"Recovery fixture"}`, tag, strings.Contains(tag, "-beta.")))
	legacyHandoffWrite(t, filepath.Join(release, "release.json"), meta)
	legacyHandoffWrite(t, filepath.Join(release, "release-metadata.json"), meta)
	fmt.Fprintf(&manifest, "%x  release-metadata.json\n", sha256.Sum256(meta))
	legacyHandoffWrite(t, filepath.Join(release, "SHA256SUMS"), []byte(manifest.String()))
	for path, value := range map[string]string{
		"etc/freenet/freenet.conf": "UI_PORT=1001\nAUTO_VPN_V1=yes\n",
		"etc/xray/configs/04_outbounds.json": "{\"outbounds\":[]}\n",
		"etc/xray/configs/05_routing.json": "{\"routing\":{}}\n",
		"etc/xray/blanc_subscription.url": "PRIVATE_SUBSCRIPTION_SENTINEL\n",
		"var/lib/freenet/vpn-transaction.pending/state": "PENDING\n",
		"var/lib/freenet/vpn-transaction.pending/out.before": "PRIVATE_BEFORE_SENTINEL\n",
		"var/run/freenet-settings-v3.state": "MUTATION_BLOCKED=yes\n",
	} {
		legacyHandoffWrite(t, filepath.Join(root, path), []byte(value))
		before[path] = []byte(value)
	}
	return before
}

func legacyHandoffRun(t *testing.T, updater, root, release, current, target, mode, failure string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("sh", updater, mode, target)
	cmd.Env = append(os.Environ(), "FREENET_ROOT="+root, "FREENET_CURRENT_VERSION="+current,
		"FREENET_ARCH=arm64-v8a", "FREENET_LATEST_TAG="+target, "FREENET_TEST_RELEASE_DIR="+release,
		"FREENET_UPDATE_STATE_FILE="+filepath.Join(root, "var/run/update.state"),
		"FREENET_UPDATE_LOCK_DIR="+filepath.Join(root, "var/run/update.lock"),
		"FREENET_SELF_UPDATE_TEST_MODE=yes", "FREENET_UPDATE_BETA_OPT_IN=yes", "FREENET_TEST_FAIL_STAGE="+failure)
	return cmd.CombinedOutput()
}

func TestLegacyHandoffDistributionTwoHopAndRollback(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil { t.Fatal("python3 required for deterministic distribution test") }
	if _, err := exec.LookPath("jq"); err != nil { t.Skip("shell fixture requires jq") }
	dir := t.TempDir()
	updater := filepath.Join(dir, "self_update.sh")
	generator := filepath.Join("..", "scripts", "build_legacy_recovery_updater.py")
	engine := filepath.Join("..", "scripts", "self_update.sh")
	if out, err := exec.Command("python3", generator, engine, updater).CombinedOutput(); err != nil { t.Fatalf("generator: %v %s", err, out) }
	if out, err := exec.Command("sh", "-n", updater).CombinedOutput(); err != nil { t.Fatalf("syntax: %v %s", err, out) }
	second := filepath.Join(dir, "repeat.sh")
	if out, err := exec.Command("python3", generator, engine, second).CombinedOutput(); err != nil { t.Fatalf("repeat: %v %s", err, out) }
	firstBytes, _ := os.ReadFile(updater)
	secondBytes, _ := os.ReadFile(second)
	if !bytes.Equal(firstBytes, secondBytes) { t.Fatal("distribution is not reproducible") }
	for _, scenario := range []struct{ name, current, target, failure string; narrow, fail bool }{
		{"beta-first-hop", "v0.8.0-beta.1", "v0.7.18", "", true, false},
		{"beta-first-hop-rollback", "v0.8.0-beta.1", "v0.7.18", "accept", true, true},
		{"normal-stable-maintenance", "v0.7.17", "v0.7.18", "", false, false},
		{"explicit-beta-continuation", "v0.7.18", "v0.8.0-beta.5", "", false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			base := t.TempDir()
			root, release := filepath.Join(base, "opt"), filepath.Join(base, "release")
			before := legacyHandoffFixture(t, root, release, scenario.target, updater)
			out, err := legacyHandoffRun(t, updater, root, release, scenario.current, scenario.target, "plan", "")
			if err != nil || !strings.Contains(string(out), "READY=yes") { t.Fatalf("plan: %v %s", err, out) }
			if strings.Contains(string(out), "RECOVERY_BINARY_ONLY=yes") != scenario.narrow { t.Fatalf("wrong plan scope: %s", out) }
			for path, want := range before {
				got, _ := os.ReadFile(filepath.Join(root, path))
				if !bytes.Equal(got, want) { t.Fatal("plan mutated", path) }
			}
			out, err = legacyHandoffRun(t, updater, root, release, scenario.current, scenario.target, "apply", scenario.failure)
			if (err != nil) != scenario.fail { t.Fatalf("apply: %v %s", err, out) }
			for path, want := range before {
				isAsset := false
				for _, assetPath := range legacyHandoffAssetDestinations { if assetPath == path { isAsset = true } }
				changed := isAsset && !scenario.fail && (!scenario.narrow || path == "sbin/freenet-ui" || path == "lib/freenet/self_update.sh")
				got, _ := os.ReadFile(filepath.Join(root, path))
				if bytes.Equal(got, want) == changed { t.Fatalf("unexpected delta for %s", path) }
			}
			state, _ := os.ReadFile(filepath.Join(root, "var/run/update.state"))
			if scenario.fail && !strings.Contains(string(state), "ROLLBACK_STATE=SUCCESS") { t.Fatal("rollback not confirmed", string(state)) }
		})
	}
}
