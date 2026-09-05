package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCanonicalRecoveryManagedSplitDNS(t *testing.T, configDir string) {
	t.Helper()
	writeLegacyMigrationConfig(t, configDir, "02_dns.json", `{
  "dns": {
    "tag": "dns-vless",
    "servers": [
      {"address":"https://8.8.8.8/dns-query","tag":"dns-vless","finalQuery":true}
    ],
    "queryStrategy": "UseIPv4"
  }
}`)
}

func removeCanonicalRecoveryNativeBackup(t *testing.T, backupRoot string) {
	t.Helper()
	if err := os.RemoveAll(filepath.Join(backupRoot, "freenet-network-standard-20260901-120000")); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyNativeDNSCanonicalRecoveryAfterEngineAlreadyConfirmed(t *testing.T) {
	stateDir, backupRoot, configDir, _ := setupLegacyNativeRecoveryFixture(t, "exit 0")
	writeCanonicalRecoveryManagedSplitDNS(t, configDir)
	removeCanonicalRecoveryNativeBackup(t, backupRoot)

	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "filter-engine.native"), []byte("public\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plan := legacySplitPlanForTest()
	enrichNativeFilterEngineMigration(&plan)
	if plan.NativeFilterEngineConfirmRequired {
		t.Fatal("already-confirmed engine must not be requested again")
	}
	if err := persistConfirmedLegacyNativeFilterEngine(plan, ""); err != nil {
		t.Fatalf("canonical post-confirmation recovery failed: %v", err)
	}

	recovered, err := os.ReadFile(filepath.Join(stateDir, "02_dns.native"))
	if err != nil {
		t.Fatal(err)
	}
	if string(recovered) != "{}\n" {
		t.Fatalf("unexpected canonical native DNS baseline: %q", recovered)
	}
	valid, missing, err := legacyNativeDNSSnapshotStatus()
	if err != nil || !valid || missing {
		t.Fatalf("canonical native snapshot invalid: valid=%v missing=%v err=%v", valid, missing, err)
	}
	engine, err := os.ReadFile(filepath.Join(stateDir, "filter-engine.native"))
	if err != nil || string(engine) != "public\n" {
		t.Fatalf("confirmed engine changed during canonical recovery: %q err=%v", engine, err)
	}
}

func TestLegacyNativeDNSCanonicalRecoveryCommitsEngineOnlyAfterValidation(t *testing.T) {
	stateDir, backupRoot, configDir, _ := setupLegacyNativeRecoveryFixture(t, "exit 0")
	writeCanonicalRecoveryManagedSplitDNS(t, configDir)
	removeCanonicalRecoveryNativeBackup(t, backupRoot)

	plan := legacySplitPlanForTest()
	enrichNativeFilterEngineMigration(&plan)
	if !plan.NativeFilterEngineConfirmRequired {
		t.Fatal("expected explicit engine confirmation")
	}
	if err := persistConfirmedLegacyNativeFilterEngine(plan, "public"); err != nil {
		t.Fatalf("canonical recovery with confirmation failed: %v", err)
	}

	recovered, err := os.ReadFile(filepath.Join(stateDir, "02_dns.native"))
	if err != nil || string(recovered) != "{}\n" {
		t.Fatalf("canonical native DNS not persisted: %q err=%v", recovered, err)
	}
	engine, err := os.ReadFile(filepath.Join(stateDir, "filter-engine.native"))
	if err != nil || string(engine) != "public\n" {
		t.Fatalf("engine baseline not committed after canonical validation: %q err=%v", engine, err)
	}
}

func TestLegacyNativeDNSCanonicalRecoveryRejectsUnknownCurrentDNS(t *testing.T) {
	stateDir, backupRoot, configDir, _ := setupLegacyNativeRecoveryFixture(t, "exit 0")
	removeCanonicalRecoveryNativeBackup(t, backupRoot)
	writeLegacyMigrationConfig(t, configDir, "02_dns.json", `{"dns":{"servers":["1.1.1.1"]}}`)

	plan := legacySplitPlanForTest()
	enrichNativeFilterEngineMigration(&plan)
	err := persistConfirmedLegacyNativeFilterEngine(plan, "public")
	if err == nil || !strings.Contains(err.Error(), "не совпадает с детерминированным FreeNet-managed Split") {
		t.Fatalf("unknown current DNS must STOP, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(stateDir, "02_dns.native")); !os.IsNotExist(statErr) {
		t.Fatalf("unknown current DNS created native snapshot: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(stateDir, "filter-engine.native")); !os.IsNotExist(statErr) {
		t.Fatalf("unknown current DNS persisted engine: %v", statErr)
	}
}

func TestLegacyNativeDNSCanonicalRecoveryRequiresXrayValidation(t *testing.T) {
	stateDir, backupRoot, configDir, _ := setupLegacyNativeRecoveryFixture(t, "exit 1")
	writeCanonicalRecoveryManagedSplitDNS(t, configDir)
	removeCanonicalRecoveryNativeBackup(t, backupRoot)

	plan := legacySplitPlanForTest()
	enrichNativeFilterEngineMigration(&plan)
	err := persistConfirmedLegacyNativeFilterEngine(plan, "public")
	if err == nil || !strings.Contains(err.Error(), "canonical neutral native 02_dns не прошёл Xray candidate validation") {
		t.Fatalf("canonical recovery bypassed Xray validation: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(stateDir, "02_dns.native")); !os.IsNotExist(statErr) {
		t.Fatalf("failed canonical validation persisted native snapshot: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(stateDir, "filter-engine.native")); !os.IsNotExist(statErr) {
		t.Fatalf("failed canonical validation persisted engine: %v", statErr)
	}
}
