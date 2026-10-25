package main

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// A core binary update must outlive the browser connection that initiated it.
// Only one writer is admitted by app.sem, which the worker releases after the
// complete forward/rollback transaction. No credentials or raw config are
// included in this read-only process-local receipt.
type xrayCoreOperationStatus struct {
	State         string                 `json:"state"`
	TargetVersion string                 `json:"target_version,omitempty"`
	UpdatedAt     string                 `json:"updated_at,omitempty"`
	Result        *xrayCoreApplyResponse `json:"result,omitempty"`
}

var xrayCoreOperationRecord = struct {
	sync.RWMutex
	current xrayCoreOperationStatus
}{current: xrayCoreOperationStatus{State: "IDLE"}}

func xrayCoreOperationSnapshot() xrayCoreOperationStatus {
	xrayCoreOperationRecord.RLock()
	defer xrayCoreOperationRecord.RUnlock()
	return xrayCoreOperationRecord.current
}

func setXrayCoreOperationStatus(status xrayCoreOperationStatus) {
	status.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	xrayCoreOperationRecord.Lock()
	xrayCoreOperationRecord.current = status
	xrayCoreOperationRecord.Unlock()
}

func (a *app) handleXrayCoreOperationGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, xrayCoreOperationSnapshot())
}

func (a *app) runXrayCoreUpdateDetached(target string, release func()) <-chan xrayCoreApplyResponse {
	done := make(chan xrayCoreApplyResponse, 1)
	setXrayCoreOperationStatus(xrayCoreOperationStatus{State: "RUNNING", TargetVersion: target})
	go func() {
		defer release()
		// No HTTP context here. The Xray binary and the rollback snapshot
		// remain owned until either side is proven terminal.
		result := a.applyXrayCore(context.Background(), target)
		state := "FAILED"
		switch {
		case result.Success:
			state = "SUCCESS"
		case result.Rollback == "SUCCESS":
			state = "ROLLED_BACK"
		case result.Rollback == "FAILED":
			state = "STOP"
			// Persist the system-wide mutation latch as well: a failed Xray
			// rollback must fence AUTO, Xray control and further VPN changes,
			// including after a FreeNet daemon restart.
			setAutomationMutationBlocked(true)
		}
		setXrayCoreOperationStatus(xrayCoreOperationStatus{
			State: state, TargetVersion: target, Result: &result,
		})
		done <- result
	}()
	return done
}
