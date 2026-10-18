package main

import (
	"os/exec"
	"testing"
)

func TestXrayTopbarAuthenticatedLifecycle(t *testing.T) {
	out, err := exec.Command("node", "../tests/test_xray_topbar_lifecycle.cjs").CombinedOutput()
	if err != nil {
		t.Fatalf("Xray read-only hydration: %v\n%s", err, out)
	}
}
