package main

// Verified, non-disruptive XKeen 1.1.3.9 -> 2.1 init.d migration.
// No upstream installation code is executed: the official, SHA-256 verified
// init template is treated as data and only known legacy settings are copied.
import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const xkeenLegacyPrevious = "1.1.3.9"
const xkeenLegacyTarget = "2.1"

var xkeenLegacyCopiedSettings = []string{
	"name_client", "name_policy", "name_policy_full", "table_id", "table_mark",
	"custom_mark", "dscp_enable", "dscp_force_proxy", "dscp_force_proxy_tag",
	"dscp_exclude", "dscp_proxy", "ipv4_proxy", "ipv4_exclude",
	"ipv6_proxy", "ipv6_exclude", "proxy_dns", "proxy_router",
	"nfqws_mark", "pbr_strict", "start_verbose", "start_attempts",
	"start_auto", "start_delay", "init_delay", "check_fd", "arm64_fd",
	"other_fd", "delay_fd", "ipv6_support", "extended_msg", "backup",
	"aghfix",
}

func xkeenLegacyMigrationReadyAt(initDir, oldVersion, target string) bool {
	if oldVersion != xkeenLegacyPrevious || target != xkeenLegacyTarget {
		return false
	}
	reason, need := xkeenInitRepairNeed(initDir)
	if !need || reason != "legacy-init-references-removed-module" {
		return false
	}
	// Do not migrate a partial/contradictory installation.
	for _, name := range []string{"S05xkeen", "S99xkeenstart", "S24xray"} {
		if _, err := os.Lstat(filepath.Join(initDir, name)); !errors.Is(err, os.ErrNotExist) {
			return false
		}
	}
	return xkeenInitSafeFile(filepath.Join(initDir, "S99xkeen"), false)
}

func xkeenLegacyMigrationReady(binary, oldVersion, target string) bool {
	return filepath.Clean(binary) == defaultXKeenPath &&
		xkeenLegacyMigrationReadyAt(xkeenInitDir(), oldVersion, target)
}

func xkeenLegacyReadRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 512<<10 {
		return nil, errors.New("legacy-init-file-unverified")
	}
	return os.ReadFile(path)
}

// Copies exactly the official upstream 1.x register_xkeen_initd assignment
// allowlist. Duplicates, missing target assignments and shell syntax errors
// STOP before any live file is changed. Values never leave the router.
func xkeenLegacyBuildInit(old, template []byte) ([]byte, error) {
	allowed := make(map[string]bool)
	for _, name := range xkeenLegacyCopiedSettings {
		allowed[name] = true
	}
	original := make(map[string]string)
	for _, line := range strings.Split(string(old), "\n") {
		// Runtime assignments inside functions are not persistent settings.
		// Official 1.x init defines all configurable defaults before its
		// first function; ignore subsequent dynamic reassignment.
		if strings.Contains(line, "() {") {
			break
		}
		i := strings.IndexByte(line, '=')
		if i <= 0 || !allowed[line[:i]] {
			continue
		}
		name := line[:i]
		if _, duplicate := original[name]; duplicate {
			return nil, errors.New("legacy-init-duplicate-assignment")
		}
		original[name] = line
	}
	if !strings.Contains(string(old), "03_info_cpu.sh") || len(original) == 0 {
		return nil, errors.New("legacy-init-unrecognized")
	}
	seen := make(map[string]bool)
	lines := strings.Split(string(template), "\n")
	for i, line := range lines {
		if strings.Contains(line, "() {") {
			break
		}
		index := strings.IndexByte(line, '=')
		if index <= 0 || !allowed[line[:index]] {
			continue
		}
		name := line[:index]
		if seen[name] {
			return nil, errors.New("modern-init-duplicate-assignment")
		}
		seen[name] = true
		if saved, ok := original[name]; ok {
			lines[i] = saved
		}
	}
	for key := range original {
		if !seen[key] {
			return nil, errors.New("modern-init-missing-legacy-setting")
		}
	}
	data := []byte(strings.Join(lines, "\n"))
	if len(data) == 0 || len(data) > 512<<10 ||
		bytes.Contains(data, []byte("03_info_cpu.sh")) ||
		bytes.Contains(data, []byte("S99xkeen")) {
		return nil, errors.New("modern-init-invalid")
	}
	return data, nil
}

func xkeenLegacyWriteDurable(path string, data []byte, mode os.FileMode) error {
	out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return errors.New("legacy-init-stage-unavailable")
	}
	n, writeErr := out.Write(data)
	syncErr := out.Sync()
	closeErr := out.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || n != len(data) {
		return errors.New("legacy-init-stage-unavailable")
	}
	return nil
}

func xkeenLegacyPrepareInit(ctx context.Context, stageDir, initDir string) (string, error) {
	if !xkeenLegacyMigrationReadyAt(initDir, xkeenLegacyPrevious, xkeenLegacyTarget) {
		return "", errors.New("legacy-init-changed")
	}
	old, err := xkeenLegacyReadRegular(filepath.Join(initDir, "S99xkeen"))
	if err != nil {
		return "", err
	}
	template, err := xkeenLegacyReadRegular(filepath.Join(stageDir, "_xkeen", "02_install", "07_install_register", "04_register_init.sh"))
	if err != nil {
		return "", errors.New("modern-init-template-unavailable")
	}
	data, err := xkeenLegacyBuildInit(old, template)
	if err != nil {
		return "", err
	}
	staged := filepath.Join(stageDir, "legacy-S05xkeen")
	if err := xkeenLegacyWriteDurable(staged, data, 0700); err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, "sh", "-n", staged)
	command.Env = append(os.Environ(), "PATH=/opt/bin:/opt/sbin:/bin:/sbin:/usr/bin:/usr/sbin")
	if command.Run() != nil {
		return "", errors.New("modern-init-syntax-invalid")
	}
	return staged, nil
}

func xkeenLegacyBackupInit(initDir, snapshot string) error {
	old, err := xkeenLegacyReadRegular(filepath.Join(initDir, "S99xkeen"))
	if err != nil {
		return err
	}
	path := filepath.Join(snapshot, "legacy-S99xkeen")
	if err := xkeenLegacyWriteDurable(path, old, 0600); err != nil {
		return err
	}
	return nil
}

func xkeenLegacyApplyInit(initDir, staged string) error {
	if !xkeenLegacyMigrationReadyAt(initDir, xkeenLegacyPrevious, xkeenLegacyTarget) {
		return errors.New("legacy-init-changed-before-apply")
	}
	path := filepath.Join(initDir, "S05xkeen")
	if err := os.Rename(staged, path); err != nil {
		return errors.New("modern-init-commit-failed")
	}
	if err := os.Remove(filepath.Join(initDir, "S99xkeen")); err != nil {
		return errors.New("legacy-init-retirement-failed")
	}
	reason, needs := xkeenInitRepairNeed(initDir)
	if needs || reason != "compatible" {
		return errors.New("modern-init-postcheck-failed")
	}
	return nil
}

// The private snapshot survives every non-terminal transaction. Recovery
// restores the original init before restoring the original script tree.
func xkeenLegacyRestoreInit(initDir, snapshot string) error {
	before, err := xkeenLegacyReadRegular(filepath.Join(snapshot, "legacy-S99xkeen"))
	if err != nil {
		return errors.New("legacy-init-snapshot-unavailable")
	}
	oldPath := filepath.Join(initDir, "S99xkeen")
	current, readErr := os.ReadFile(oldPath)
	if readErr == nil && !bytes.Equal(current, before) {
		return errors.New("legacy-init-live-mismatch")
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return errors.New("legacy-init-live-unavailable")
	}
	if errors.Is(readErr, os.ErrNotExist) {
		tmp := filepath.Join(initDir, ".freenet-xkeen-legacy-restore")
		if err := xkeenLegacyWriteDurable(tmp, before, 0700); err != nil {
			return err
		}
		if err := os.Rename(tmp, oldPath); err != nil {
			return errors.New("legacy-init-restore-failed")
		}
	}
	modern := filepath.Join(initDir, "S05xkeen")
	if info, err := os.Lstat(modern); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("modern-init-untrusted")
		}
		if err := os.Rename(modern, filepath.Join(snapshot, "failed-S05xkeen")); err != nil {
			return errors.New("modern-init-quarantine-failed")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("modern-init-state-unavailable")
	}
	if !xkeenLegacyMigrationReadyAt(initDir, xkeenLegacyPrevious, xkeenLegacyTarget) {
		return errors.New("legacy-init-restore-postcheck-failed")
	}
	return nil
}
