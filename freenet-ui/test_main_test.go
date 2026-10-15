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
    dir, err := os.MkdirTemp("", "freenet-unit-state-")
    if err != nil { os.Exit(2) }
    // Default writes must be impossible; tests which need writable state use
    // their own fresh, test-scoped FREENET_AUTOMATION_STATE path.
    if err := os.Setenv("FREENET_AUTOMATION_STATE", "/dev/null"); err != nil { os.Exit(2) }
    if err := os.Setenv("FREENET_VPN_TRANSACTION_DIR", filepath.Join(dir, "transaction.pending")); err != nil { os.Exit(2) }
    code := m.Run()
    _ = os.RemoveAll(dir)
    os.Exit(code)
}
