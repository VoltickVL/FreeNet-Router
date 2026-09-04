package main

import "testing"

func TestClassifyNetworkFailureBeforeMutationAsNotApplied(t *testing.T) {
	raw := "[FreeNet Network] ERROR: PRIMARY ERROR: native Keenetic DNS query failed\n" +
		"[FreeNet Network] ERROR: ROLLBACK ERROR/STATE: no live apply\n"
	primary, rollback := classifyApplyFailure(sanitizeOutput(raw))
	if primary != "native Keenetic DNS query failed" {
		t.Fatalf("primary=%q", primary)
	}
	if rollback != "NOT_APPLIED" {
		t.Fatalf("rollback=%q, want NOT_APPLIED", rollback)
	}
}
