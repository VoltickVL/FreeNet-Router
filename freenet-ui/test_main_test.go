package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The production UNKNOWN gate is global. Unrelated unit tests must not read
// or inherit a persistent automation rollback marker from the host/test suite.
// Tests explicitly exercising the marker override these locations with t.Setenv.
func TestMain(m *testing.M) {
	os.Exit(runTestsWithIsolatedState(m))
}

func runTestsWithIsolatedState(m *testing.M) int {
	dir, err := os.MkdirTemp("", "freenet-unit-state-")
	if err != nil {
		return 2
	}
	defer os.RemoveAll(dir)

	// A regular-file parent rejects both mkdir and atomic state writes with
	// ENOTDIR, including under root. Tests needing writable state use t.Setenv.
	blocker := filepath.Join(dir, "automation-state-blocker")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		return 2
	}
	if err := os.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(blocker, "automation.state")); err != nil {
		return 2
	}
	if err := os.Setenv("FREENET_VPN_TRANSACTION_DIR", filepath.Join(dir, "transaction.pending")); err != nil {
		return 2
	}
	// Tests and their explicit child processes must never join the host's
	// production probe fence. Child fixtures inherit this isolated path.
	if err := os.Setenv("FREENET_XRAY_PROBE_LOCK_FILE", filepath.Join(dir, "xray-probes.lock")); err != nil {
		return 2
	}
	return m.Run()
}
