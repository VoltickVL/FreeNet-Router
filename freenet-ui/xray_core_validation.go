package main

import (
    "context"
    "net/http"
    "time"
)

// This GET only checks whether a guarded transaction can be requested.
// A full semantic Xray run -test cannot share a deadline with admission to
// the busy Best Server/provider probe pool. Both installed and candidate
// configs are tested *inside the exclusive fence* before binary mutation.
type xrayCoreValidationResponse struct {
    Success    bool   `json:"success"`
    Ready      bool   `json:"ready"`
    Valid      bool   `json:"valid"` // false until mandatory guarded semantic tests
    Validation string `json:"validation"`
    Mutation   string `json:"mutation"`
    Reason     string `json:"reason,omitempty"`
}

func (a *app) handleXrayCoreValidationGet(w http.ResponseWriter, r *http.Request) {
    result := xrayCoreValidationResponse{Success: true, Mutation: "NONE", Validation: "DEFERRED"}
    if xrayCoreOperationSnapshot().State == "RUNNING" {
        result.Reason = "reason=xray-core-update-running"
        writeJSON(w, http.StatusOK, result)
        return
    }
    if xrayCoreOperationSnapshot().State == "STOP" || automationMutationBlockedState() {
        result.Reason = "reason=xray-recovery-stop"
        writeJSON(w, http.StatusOK, result)
        return
    }
    ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
    defer cancel()
    binary, _, err := a.resolvedXrayCoreBinary()
    if err != nil {
        result.Reason = "reason=xray-test-executable-unavailable"
        writeJSON(w, http.StatusOK, result)
        return
    }
    if _, err := xrayCoreVersionAt(ctx, binary); err != nil {
        result.Reason = "reason=xray-current-version-unavailable"
        writeJSON(w, http.StatusOK, result)
        return
    }
    if _, err := a.xrayCoreLiveConfigDir(); err != nil {
        result.Reason = err.Error()
        writeJSON(w, http.StatusOK, result)
        return
    }
    // No semantic PASS has been claimed. The detached apply worker must
    // drain diagnostic probes and validate both binaries before any swap.
    result.Ready = true
    writeJSON(w, http.StatusOK, result)
}
