package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAutomationDefaultStateRejectsWrites(t *testing.T) {
	path := automationStatePath()
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	// On the old /dev/null default this fails before any write is attempted.
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("default automation state must have a regular-file parent: %v", err)
	}
	root, err := os.Stat(filepath.Dir(parent))
	if err != nil || !root.IsDir() || root.Mode().Perm()&0077 != 0 {
		t.Fatalf("default automation state must use a private temporary root: %v", err)
	}
	before, err := os.ReadFile(parent)
	if err != nil {
		t.Fatal(err)
	}

	// Exercise the production atomic writer which previously could rename over
	// /dev/null. The safe default must never retain its global UNKNOWN marker.
	writeAutomationStateV2("blocked", "test default-state isolation", "FAILED/UNKNOWN", false)
	for _, candidate := range []string{path, path + ".v2.new"} {
		if _, err := os.Lstat(candidate); !errors.Is(err, syscall.ENOTDIR) {
			t.Fatalf("default state path must reject writes with ENOTDIR: %v", err)
		}
	}
	if state := parseAutomationState(path); len(state) != 0 {
		t.Fatalf("default state retained a marker: %+v", state)
	}
	after, err := os.ReadFile(parent)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("default-state blocker changed: %v", err)
	}
}

func TestAutomationStateOverrideKeepsUnknownGuard(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "automation.state")
	t.Setenv("FREENET_AUTOMATION_STATE", state)
	t.Setenv("FREENET_VPN_TRANSACTION_DIR", filepath.Join(dir, "transaction.pending"))

	writeAutomationStateV2("blocked", "test explicit-state override", "FAILED/UNKNOWN", false)
	values := parseAutomationState(state)
	if values["MUTATION_BLOCKED"] != "yes" || values["ROLLBACK_READY"] != "FAILED/UNKNOWN" {
		t.Fatalf("explicit test state did not retain the UNKNOWN marker: %+v", values)
	}
	if !automationMutationBlockedState() {
		t.Fatal("explicit test override bypassed the production UNKNOWN guard")
	}
}
