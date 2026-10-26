package main

import (
    "context"
    "encoding/json"
    "net/http"
    "strings"
    "time"
)

// Recovering from a previous failed core rollback must not require SSH or
// editing FreeNet's private state file. This opt-in operation changes ONLY
// the mutation latch. It does not touch Xray, XKeen, DNS, VPN or routing.
// The historical failed-rollback receipt is retained separately.
type xrayCoreRecoveryRequest struct {
    ExpectedVersion string `json:"expected_version"`
}
type xrayCoreRecoveryResponse struct {
    Success bool `json:"success"`
    Version string `json:"version,omitempty"`
    Mutation string `json:"mutation"`
    Reason string `json:"reason,omitempty"`
}

func (a *app) handleXrayCoreRecovery(w http.ResponseWriter, r *http.Request) {
    blocked := func(status int, reason string) {
        writeJSON(w, status, xrayCoreRecoveryResponse{Success: false, Mutation:"NONE", Reason:reason})
    }
    if a.mutationBlockedBySelfUpdate(w) { return }
    if !sameOrigin(r) {
        blocked(http.StatusForbidden, "reason=xray-recovery-cross-origin")
        return
    }
    if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))), "application/json") {
        blocked(http.StatusUnsupportedMediaType, "reason=xray-recovery-json-required")
        return
    }
    var request xrayCoreRecoveryRequest
    dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
    dec.DisallowUnknownFields()
    if err := dec.Decode(&request); err != nil ||
        !validXrayCoreTag(request.ExpectedVersion) || normalizeXrayCoreTag(request.ExpectedVersion) != request.ExpectedVersion {
        blocked(http.StatusBadRequest, "reason=xray-recovery-expected-version-invalid")
        return
    }
    if xrayCoreOperationSnapshot().State == "RUNNING" || vpnTransactionPending() {
        blocked(http.StatusConflict, "reason=xray-recovery-operation-pending")
        return
    }
    select {
    case a.sem <- struct{}{}:
        defer func(){<-a.sem}()
    default:
        blocked(http.StatusConflict, "reason=xray-recovery-operation-busy")
        return
    }
    ctx,cancel:=context.WithTimeout(r.Context(),8*time.Second)
    defer cancel()
    binary,_,err:=a.resolvedXrayCoreBinary()
    if err!=nil {
        blocked(http.StatusConflict,"reason=xray-recovery-binary-unavailable")
        return
    }
    liveVersion, err:=xrayCoreVersionAt(ctx,binary)
    if err!=nil || liveVersion!=request.ExpectedVersion || a.manualRecoveryCoreState()!="running" {
        blocked(http.StatusConflict, "reason=xray-recovery-runtime-unconfirmed")
        return
    }
    if err:=xrayCoreNativePreflight(ctx,binary);err!=nil {
        blocked(http.StatusConflict, "reason=xray-recovery-native-process-unconfirmed")
        return
    }
    // Explicit user request plus live GID11111/executable-inode proof is
    // sufficient to adopt THIS running version as the new safe baseline.
    // This is not a claim that the previous rollback originally succeeded.
    if automationMutationBlockedState() {
        setAutomationMutationBlocked(false)
        if automationMutationBlockedState() {
            blocked(http.StatusInternalServerError,"reason=xray-recovery-latch-persist-failed")
            return
        }
    }
    old:=xrayCoreOperationSnapshot()
    if old.State=="STOP" {
        setXrayCoreOperationStatus(xrayCoreOperationStatus{
            State:"RECOVERED",TargetVersion:old.TargetVersion,Result:old.Result,
        })
    }
    v3AppendEvent("xray","success","Фактический Xray подтверждён: нативный запуск и GID11111. Разблокирована новая операция; прежний rollback FAILED сохранён в журнале.")
    writeJSON(w,http.StatusOK,xrayCoreRecoveryResponse{Success:true,Mutation:"LATCH_ONLY",Version:liveVersion})
}
