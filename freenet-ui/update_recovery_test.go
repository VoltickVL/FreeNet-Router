package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeRecoveryRelease(t *testing.T, dir, updater string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	assets := []string{
		"freenet-ui-arm64-v8a", "freenet", "vpn", "blanc_xkeen_update_outbounds.sh",
		"migrate_split_dns.sh", "apply_network_profile.sh", "apply_provider_profile.sh",
		"finalize_setup.sh", "bootstrap_entware.sh", "upstream-pins.env", "self_update.sh",
	}
	var manifest strings.Builder
	for _, name := range assets {
		data := []byte("#!/bin/sh\nexit 0\n")
		if name == "upstream-pins.env" {
			data = []byte("PIN_POLICY_VERSION=TEST\n")
		}
		if name == "self_update.sh" {
			b, err := os.ReadFile(updater)
			if err != nil {
				t.Fatal(err)
			}
			data = b
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o755); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(&manifest, "%x  %s\n", sum, name)
	}
	if err := os.WriteFile(filepath.Join(dir, "SHA256SUMS"), []byte(manifest.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func recoveryRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{
		"sbin", "bin", "lib/freenet", "etc/freenet", "etc/xray/configs", "var/run", "backups",
	} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"sbin/freenet-ui":                         "OLD_UI\n",
		"bin/freenet":                             "OLD_MANAGER\n",
		"bin/vpn":                                 "OLD_VPN\n",
		"bin/blanc_xkeen_update_outbounds.sh":     "#!/bin/sh\nexit 0\n",
		"lib/freenet/migrate_split_dns.sh":        "#!/bin/sh\nexit 0\n",
		"lib/freenet/apply_network_profile.sh":    "#!/bin/sh\nexit 0\n",
		"lib/freenet/apply_provider_profile.sh":   "#!/bin/sh\nexit 0\n",
		"lib/freenet/finalize_setup.sh":           "#!/bin/sh\nexit 0\n",
		"lib/freenet/bootstrap_entware.sh":        "#!/bin/sh\nexit 0\n",
		"lib/freenet/self_update.sh":              "#!/bin/sh\nexit 0\n",
		"etc/freenet/upstream-pins.env":           "OLD_PINS\n",
		"etc/freenet/freenet.conf":                "UI_PORT=1001\n",
		"etc/xray/configs/04_outbounds.json":      "{\"outbounds\":[]}\n",
		"etc/freenet/version":                     "v0.3.9\n",
	}
	for rel, body := range files {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(rel, ".sh") || rel == "sbin/freenet-ui" || rel == "bin/freenet" || rel == "bin/vpn" {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestSelfUpdateRecoveryAutodetectsInstalledVersion(t *testing.T) {
	root := recoveryRoot(t)
	updater := filepath.Join("..", "scripts", "self_update.sh")
	release := filepath.Join(t.TempDir(), "release")
	writeRecoveryRelease(t, release, updater)

	cmd := exec.Command("sh", updater, "apply", "v0.3.10")
	cmd.Env = append(os.Environ(),
		"FREENET_ROOT="+root,
		"FREENET_ARCH=arm64-v8a",
		"FREENET_LATEST_TAG=v0.3.10",
		"FREENET_TEST_RELEASE_DIR="+release,
		"FREENET_UPDATE_STATE_FILE="+filepath.Join(root, "var/run/update.state"),
		"FREENET_UPDATE_LOCK_DIR="+filepath.Join(root, "var/run/update.lock"),
		"FREENET_SELF_UPDATE_TEST_MODE=yes",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("direct recovery update failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "RESULT=SUCCESS") {
		t.Fatalf("success marker missing: %s", out)
	}
	version, err := os.ReadFile(filepath.Join(root, "etc/freenet/version"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(version)) != "v0.3.10" {
		t.Fatalf("persistent version marker = %q, want v0.3.10", version)
	}
}

func TestSelfUpdateFailedStagingDoesNotAdvanceVersionMarker(t *testing.T) {
	root := recoveryRoot(t)
	updater := filepath.Join("..", "scripts", "self_update.sh")
	release := filepath.Join(t.TempDir(), "release")
	writeRecoveryRelease(t, release, updater)

	cmd := exec.Command("sh", updater, "apply", "v0.3.10")
	cmd.Env = append(os.Environ(),
		"FREENET_ROOT="+root,
		"FREENET_ARCH=arm64-v8a",
		"FREENET_TEST_RELEASE_DIR="+release,
		"FREENET_UPDATE_STATE_FILE="+filepath.Join(root, "var/run/update.state"),
		"FREENET_UPDATE_LOCK_DIR="+filepath.Join(root, "var/run/update.lock"),
		"FREENET_SELF_UPDATE_TEST_MODE=yes",
		"FREENET_TEST_FAIL_STAGE=staging",
	)
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("staging failure unexpectedly succeeded: %s", out)
	}
	version, err := os.ReadFile(filepath.Join(root, "etc/freenet/version"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(version)) != "v0.3.9" {
		t.Fatalf("failed update advanced version marker to %q", version)
	}
}
