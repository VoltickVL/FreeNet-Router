package main

import (
	"strings"
	"testing"
)

func TestNetworkApplyConflictReconcilesByReadOnlyState(t *testing.T) {
	data, err := webFS.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)

	for _, required := range []string{
		"reconcileNetworkTargetAfterConflict",
		"another FreeNet operation is already running",
		"await loadStatus()",
		"await loadNetworkPlan()",
		"Повторный Apply автоматически не запускался",
		"removeEventListener('click', legacyApplyNetworkProfile)",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("network apply reconciliation contract missing %q", required)
		}
	}

	start := strings.Index(ui, "async function reconcileNetworkTargetAfterConflict")
	end := strings.Index(ui, "async function reconciledNetworkApply")
	if start < 0 || end <= start {
		t.Fatal("cannot isolate network reconciliation helper")
	}
	if strings.Contains(ui[start:end], "/api/network-profile/apply") {
		t.Fatal("read-only reconciliation must never retry Network Apply mutation")
	}
}
