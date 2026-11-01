package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func xkeenLegacyFixture(t *testing.T) (string, string, string, []byte) {
	t.Helper()
	root := t.TempDir()
	initDir := filepath.Join(root, "etc", "init.d")
	stage := filepath.Join(root, "sbin", "staging")
	if err := os.MkdirAll(initDir, 0700); err != nil {
		t.Fatal(err)
	}
	templateDir := filepath.Join(stage, "_xkeen", "02_install", "07_install_register")
	if err := os.MkdirAll(templateDir, 0700); err != nil {
		t.Fatal(err)
	}
	old := []byte("#!/bin/sh\nname_client=\"xray\"\nstart_auto=\"on\"\nstart_delay=\"3\"\n# old module: 03_info_cpu.sh\n")
	tpl := []byte("#!/bin/sh\nname_client=\"mihomo\"\nstart_auto=\"off\"\nstart_delay=\"0\"\n# current module: 03_info_xkeen.sh\n")
	if err := os.WriteFile(filepath.Join(initDir, "S99xkeen"), old, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templateDir, "04_register_init.sh"), tpl, 0700); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(root, "sbin", ".freenet-xkeen-snapshot-test")
	if err := os.MkdirAll(snapshot, 0700); err != nil {
		t.Fatal(err)
	}
	return initDir, stage, snapshot, old
}

func TestXKeenLegacyMigrationExactVersionAndCoherentInit(t *testing.T) {
	root, _, _, _ := xkeenLegacyFixture(t)
	if !xkeenLegacyMigrationReadyAt(root, "1.1.3.9", "2.1") {
		t.Fatal("coherent old stable init must be eligible")
	}
	for _, pair := range [][2]string{{"1.1.3.8", "2.1"}, {"1.1.3.9", "2.2"}, {"2.1", "2.1"}} {
		if xkeenLegacyMigrationReadyAt(root, pair[0], pair[1]) {
			t.Fatalf("unsupported migration admitted: %v", pair)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "S05xkeen"), []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	if xkeenLegacyMigrationReadyAt(root, "1.1.3.9", "2.1") {
		t.Fatal("duplicate init generations accepted")
	}
}

func TestXKeenLegacyInitMigrationPreservesSettingsAndRestoresOnRollback(t *testing.T) {
	dir, stage, snapshot, old := xkeenLegacyFixture(t)
	if err := xkeenLegacyBackupInit(dir, snapshot); err != nil {
		t.Fatal(err)
	}
	staged, err := xkeenLegacyPrepareInit(context.Background(), stage, dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(staged)
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range []string{"name_client=\"xray\"", "start_auto=\"on\"", "start_delay=\"3\""} {
		if !bytes.Contains(data, []byte(setting)) {
			t.Fatalf("legacy setting not retained: %s", setting)
		}
	}
	if bytes.Contains(data, []byte("03_info_cpu.sh")) {
		t.Fatal("new init still references deleted 1.x module")
	}
	if err := xkeenLegacyApplyInit(dir, staged, snapshot); err != nil {
		t.Fatal(err)
	}
	if reason, needs := xkeenInitRepairNeed(dir); needs || reason != "compatible" {
		t.Fatalf("modern init not accepted: %s %t", reason, needs)
	}
	if err := xkeenLegacyRestoreInit(dir, snapshot); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(dir, "S99xkeen"))
	if err != nil || !bytes.Equal(restored, old) {
		t.Fatal("rollback did not restore exact original")
	}
	if _, err := os.Stat(filepath.Join(dir, "S05xkeen")); !os.IsNotExist(err) {
		t.Fatal("modern init not quarantined during rollback")
	}
	saved, err := os.ReadFile(filepath.Join(snapshot, "legacy-S99xkeen"))
	if err != nil || !bytes.Equal(saved, old) {
		t.Fatal("rollback damaged original durable snapshot")
	}
}

func TestXKeenLegacyMigrationRejectsInvalidCandidatesBeforeMutation(t *testing.T) {
	t.Run("missing-required-template-assignment", func(t *testing.T) {
		dir, stage, _, old := xkeenLegacyFixture(t)
		_ = dir
		tpl := filepath.Join(stage, "_xkeen", "02_install", "07_install_register", "04_register_init.sh")
		if err := os.WriteFile(tpl, []byte("#!/bin/sh\nname_client=\"xray\"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		_, err := xkeenLegacyBuildInit(old, []byte("#!/bin/sh\nname_client=xray\n"))
		if err == nil {
			t.Fatal("candidate silently lost legacy start_auto/start_delay")
		}
		if _, err := xkeenLegacyPrepareInit(context.Background(), stage, dir); err == nil {
			t.Fatal("broken init candidate staged")
		}
	})
	t.Run("duplicate-legacy-assignment", func(t *testing.T) {
		_, _, _, old := xkeenLegacyFixture(t)
		tpl := []byte("#!/bin/sh\nname_client=xray\nstart_auto=on\nstart_delay=1\n")
		if _, err := xkeenLegacyBuildInit(append(old, []byte("name_client=\"mihomo\"\n")...), tpl); err == nil {
			t.Fatal("conflicting legacy assignment accepted")
		}
	})
	t.Run("new-template-mentions-legacy-module", func(t *testing.T) {
		_, _, _, old := xkeenLegacyFixture(t)
		tpl := []byte("#!/bin/sh\nname_client=xray\nstart_auto=on\nstart_delay=1\n# 03_info_cpu.sh\n")
		if _, err := xkeenLegacyBuildInit(old, tpl); err == nil {
			t.Fatal("2.x init may not reference removed module")
		}
	})
	t.Run("symlinked-legacy-init", func(t *testing.T) {
		dir, _, _, _ := xkeenLegacyFixture(t)
		_ = os.Remove(filepath.Join(dir, "S99xkeen"))
		if err := os.Symlink("anything", filepath.Join(dir, "S99xkeen")); err != nil {
			t.Fatal(err)
		}
		if xkeenLegacyMigrationReadyAt(dir, "1.1.3.9", "2.1") {
			t.Fatal("symlink init accepted")
		}
	})
}

func TestXKeenLegacyRuntimeAssignmentsNotTreatedAsHeaderDuplicates(t *testing.T) {
	_, _, _, header := xkeenLegacyFixture(t)
	old := append(append([]byte{}, header...), []byte("do_work() {\n    name_client=\"transient\"\n    table_id=\"temporary\"\n}\n")...)
	template := []byte("#!/bin/sh\nname_client=\"xray\"\nstart_auto=\"off\"\nstart_delay=0\n# 03_info_xkeen.sh\nservice() {\n    name_client=\"other\"\n}\n")
	data, err := xkeenLegacyBuildInit(old, template)
	if err != nil {
		t.Fatalf("runtime-only assignments must not be considered persistent settings: %v", err)
	}
	if !bytes.Contains(data, []byte("name_client=\"xray\"")) ||
		!bytes.Contains(data, []byte("start_auto=\"on\"")) {
		t.Fatal("persistent header values not retained")
	}
}

func TestXKeenLegacyApplyRefusesModifiedInitAfterSnapshot(t *testing.T) {
	root, stage, snapshot, _ := xkeenLegacyFixture(t)
	if err := xkeenLegacyBackupInit(root, snapshot); err != nil {
		t.Fatal(err)
	}
	staged, err := xkeenLegacyPrepareInit(context.Background(), stage, root)
	if err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(root, "S99xkeen")
	if err := os.WriteFile(oldPath, []byte("#!/bin/sh\nname_client=xray\nstart_auto=off\n# 03_info_cpu.sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := xkeenLegacyApplyInit(root, staged, snapshot); err == nil ||
		!strings.Contains(err.Error(), "drift") {
		t.Fatalf("TOCTOU init change was not rejected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "S05xkeen")); !os.IsNotExist(err) {
		t.Fatal("migration changed live init despite drift")
	}
}

func TestXKeenLegacyRestoreFailsClosedWithoutBackup(t *testing.T) {
	dir, stage, snapshot, _ := xkeenLegacyFixture(t)
	// Normal apply requires the durable snapshot; simulate its loss
	// only afterwards, before the explicit rollback attempt.
	if err := xkeenLegacyBackupInit(dir, snapshot); err != nil {
		t.Fatal(err)
	}
	staged, err := xkeenLegacyPrepareInit(context.Background(), stage, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := xkeenLegacyApplyInit(dir, staged, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(snapshot, "legacy-S99xkeen")); err != nil {
		t.Fatal(err)
	}
	if err := xkeenLegacyRestoreInit(dir, snapshot); err == nil ||
		!strings.Contains(err.Error(), "snapshot") {
		t.Fatal("missing original snapshot was treated as a successful rollback")
	}
	if reason, needs := xkeenInitRepairNeed(dir); needs || reason != "compatible" {
		t.Fatalf("unsafe rollback touched the working new init: %s", reason)
	}
}
