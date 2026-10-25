package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestXrayCoreOperationReadOnlyReceipt(t *testing.T) {
	previous := xrayCoreOperationSnapshot()
	t.Cleanup(func() { setXrayCoreOperationStatus(previous) })

	setXrayCoreOperationStatus(xrayCoreOperationStatus{State: "RUNNING", TargetVersion: "v26.10.21"})
	a := &app{}
	w := httptest.NewRecorder()
	a.handleXrayCoreOperationGet(w, httptest.NewRequest(http.MethodGet, "/api/xray/core/operation", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("read-only status: HTTP %d", w.Code)
	}
	var status xrayCoreOperationStatus
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.State != "RUNNING" || status.TargetVersion != "v26.10.21" || status.UpdatedAt == "" || status.Result != nil {
		t.Fatalf("running status must not claim terminal success: %+v", status)
	}
	failed := xrayCoreApplyResponse{Success:false, TargetVersion:"v26.10.21", Rollback:"FAILED", Error:"restart/rollback not confirmed"}
	setXrayCoreOperationStatus(xrayCoreOperationStatus{State: "STOP", TargetVersion:"v26.10.21", Result:&failed})
	w = httptest.NewRecorder()
	a.handleXrayCoreOperationGet(w, httptest.NewRequest(http.MethodGet, "/api/xray/core/operation", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.State != "STOP" || status.Result == nil || status.Result.Rollback != "FAILED" || status.Result.Success {
		t.Fatalf("failed rollback must never turn into a successful receipt: %+v", status)
	}
}
