package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSetupStateUsesOnlySafeLocalFlags(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "freenet.conf")
	content := "INSTALL_SCENARIO=existing_stack\nSETUP_COMPLETE=yes\nSECRET_URL=https://example.invalid/secret\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	scenario, complete := readSetupState(path)
	if scenario != "existing_stack" || !complete {
		t.Fatalf("неожиданное состояние: scenario=%q complete=%v", scenario, complete)
	}

	b, err := json.Marshal(statusResponse{InstallScenario: scenario, SetupComplete: complete})
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "example.invalid") || strings.Contains(text, "SECRET_URL") {
		t.Fatalf("status API раскрыл посторонние данные config: %s", text)
	}
}

func TestReadSetupStateRejectsManualUnknownScenario(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "freenet.conf")
	if err := os.WriteFile(path, []byte("INSTALL_SCENARIO=force_reinstall\nSETUP_COMPLETE=no\n"), 0600); err != nil {
		t.Fatal(err)
	}

	scenario, complete := readSetupState(path)
	if scenario != "unknown" || complete {
		t.Fatalf("неизвестный сценарий не нормализован: scenario=%q complete=%v", scenario, complete)
	}
}
