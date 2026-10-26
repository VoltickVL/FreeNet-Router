package main

import (
    "context"
    "net/http"
    "time"
)

// This GET only checks whether a guarded transaction can be requested.
// The chooser is read-only. The core update uses verified official SHA-256,
// a matching binary version, a safe snapshot, then native XKeen UI softRestart.
// No repeated 30-second "run -test" blocks UI or the native rollback.
type xrayCoreValidationResponse struct {
    Success    bool   `json:"success"`
    Ready      bool   `json:"ready"`
    Valid      bool   `json:"valid"` // no claim about an unperformed live semantic test
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
    // No runtime PASS is claimed here; native transactional acceptance is
    // verified after a controlled XKeen UI restart.
    result.Ready = true
    writeJSON(w, http.StatusOK, result)
}
