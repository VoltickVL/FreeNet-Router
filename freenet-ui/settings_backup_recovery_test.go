package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV3BackupSnapshotRestoresExactPresenceAndBytes(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "snapshot")
	if err := os.Mkdir(src, 0700); err != nil {
		t.Fatal(err)
	}
	existingDst := filepath.Join(dir, "dst", "existing.conf")
	absentDst := filepath.Join(dir, "dst", "absent.conf")
	if err := os.MkdirAll(filepath.Dir(existingDst), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existingDst, []byte("before\n"), 0600); err != nil {
		t.Fatal(err)
	}
	files := []v3BackupTrackedFile{
		{Name: "existing.conf", Dst: existingDst},
		{Name: "absent.conf", Dst: absentDst},
	}
	if err := v3CaptureBackupSnapshot(src, files); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(existingDst, []byte("mutated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absentDst, []byte("created-after-snapshot\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := v3ApplyBackupSnapshot(src, files, true); err != nil {
		t.Fatal(err)
	}
	if err := v3VerifyBackupSnapshot(src, files, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(existingDst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "before\n" {
		t.Fatalf("existing file bytes not restored: %q", got)
	}
	if _, err := os.Stat(absentDst); !os.IsNotExist(err) {
		t.Fatalf("file that was absent before snapshot must be absent after restore: %v", err)
	}
}

func TestV3LegacySnapshotDoesNotDeleteDestinationMissingFromBackup(t *testing.T) {
	dir := t.TempDir()
	snapshot := filepath.Join(dir, "legacy")
	if err := os.Mkdir(snapshot, 0700); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst", "kept.conf")
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("keep-me\n"), 0600); err != nil {
		t.Fatal(err)
	}
	files := []v3BackupTrackedFile{{Name: "missing.conf", Dst: dst}}

	if err := v3ApplyBackupSnapshot(snapshot, files, false); err != nil {
		t.Fatal(err)
	}
	if err := v3VerifyBackupSnapshot(snapshot, files, false); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep-me\n" {
		t.Fatalf("legacy missing entry changed destination: %q", got)
	}
}

func TestRestoreV3BackupReportsUnknownWhenRollbackFails(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FREENET_SETTINGS_BACKUP_ROOT", root)
	sourceName := "backup-20260920-000000.000000000"
	if err := os.Mkdir(filepath.Join(root, sourceName), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "latest"), []byte(sourceName+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	a := &app{cfg: config{
		ConfigPath: filepath.Join(dir, "freenet.conf"),
		SubPath: filepath.Join(dir, "subscription.url"),
		FilterPath: filepath.Join(dir, "profile_filter.regex"),
		OutPath: filepath.Join(dir, "04_outbounds.json"),
	}}

	origApply := v3ApplyBackupSnapshotForRestore
	origVerify := v3VerifyBackupSnapshotForRestore
	defer func() {
		v3ApplyBackupSnapshotForRestore = origApply
		v3VerifyBackupSnapshotForRestore = origVerify
	}()
	v3ApplyBackupSnapshotForRestore = func(string, []v3BackupTrackedFile, bool) error {
		return errors.New("simulated restore failure")
	}
	v3VerifyBackupSnapshotForRestore = v3VerifyBackupSnapshot

	err := a.restoreV3Backup()
	if err == nil {
		t.Fatal("restore must fail when forward restore and rollback fail")
	}
	if !strings.Contains(err.Error(), "rollback failed or is unknown") {
		t.Fatalf("rollback failure classification missing: %v", err)
	}
	if strings.Contains(err.Error(), "previous files restored") {
		t.Fatalf("rollback failure must not claim previous files were restored: %v", err)
	}
}

func TestCreateV3BackupFailsWhenLatestPointerCannotBeCommitted(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FREENET_SETTINGS_BACKUP_ROOT", root)
	if err := os.Mkdir(filepath.Join(root, "latest"), 0700); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	a := &app{cfg: config{
		ConfigPath: filepath.Join(dir, "freenet.conf"),
		SubPath: filepath.Join(dir, "subscription.url"),
		FilterPath: filepath.Join(dir, "profile_filter.regex"),
		OutPath: filepath.Join(dir, "04_outbounds.json"),
	}}

	_, err := a.createV3Backup(true)
	if err == nil {
		t.Fatal("backup must fail when latest restore reference cannot be committed")
	}
	if !strings.Contains(err.Error(), "cannot commit backup restore reference") {
		t.Fatalf("unexpected error: %v", err)
	}
}
