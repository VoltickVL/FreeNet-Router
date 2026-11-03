package main

import (
    "context"
    "encoding/json"
    "net/http"
    "strings"
    "time"
)

// This operation never changes production Xray, a DNS file, a route or VPN
// credentials. The existing proven-orphan reaper is the sole mutator.
// It is guarded by the same exclusive cross-process probe fence as VPN apply.
type probeReconcileRequest struct {
    Confirm bool `json:"confirm"`
}

type probeReconcileResponse struct {
    Success bool `json:"success"`
    Mutation string `json:"mutation"`
    Decision string `json:"decision"`
    Before string `json:"before,omitempty"`
    After string `json:"after,omitempty"`
    Error string `json:"error,omitempty"`
}

func probeReconcileAdmissible(p xrayProcessInventoryResponse) bool {
    return p.Success &&
        p.State == "isolated-probe-active" &&
        p.ProductionCount == 1 &&
        p.ProbeCount > 0 &&
        p.UnknownCount == 0 &&
        p.ZombieCount == 0
}

func (a *app) workXrayProbeInventory() xrayProcessInventoryResponse {
    binary, _, binErr := a.resolvedXrayCoreBinary()
    configDir, dirErr := a.xrayCoreLiveConfigDir()
    if binErr != nil || dirErr != nil {
        return xrayProcessInventoryResponse{State: "runtime-identity-unavailable"}
    }
    return scanXrayProcessInventory("/proc", configDir, binary)
}

func (a *app) handleXrayProbeReconcile(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Cache-Control", "private, no-store")
    reject := func(code int, decision, reason string, before, after string) {
        writeJSON(w, code, probeReconcileResponse{
            Mutation: "NONE", Decision: decision,
            Error: reason, Before: before, After: after,
        })
    }
    if !sameOrigin(r) {
        reject(http.StatusForbidden, "STOP_ORIGIN", "cross-origin request rejected", "", "")
        return
    }
    if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
        reject(http.StatusUnsupportedMediaType, "STOP_CONTENT_TYPE", "application/json required", "", "")
        return
    }
    var request probeReconcileRequest
    dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256))
    dec.DisallowUnknownFields()
    if err := dec.Decode(&request); err != nil || !request.Confirm {
        reject(http.StatusBadRequest, "STOP_CONFIRMATION", "explicit confirmation required", "", "")
        return
    }
    if a.updateLockHeld() || automationMutationBlockedState() {
        reject(http.StatusConflict, "STOP_OPERATION_LOCK", "FreeNet update or unknown VPN transaction blocks reconciliation", "", "")
        return
    }
    doctor := a.vpnTransactionDoctor()
    if doctor.Checkpoint != "absent" || doctor.Decision != "NO_CHECKPOINT" {
        reject(http.StatusConflict, "STOP_VPN_CHECKPOINT", "unresolved VPN checkpoint blocks reconciliation", "", "")
        return
    }
    unlock, acquired := tryAcquireFreeNetOperation(a)
    if !acquired {
        reject(http.StatusLocked, "STOP_BUSY", "another FreeNet operation is active", "", "")
        return
    }
    defer unlock()

    before := a.workXrayProbeInventory()
    if before.State == "ready" && before.RestartReady {
        writeJSON(w, http.StatusOK, probeReconcileResponse{
            Success: true, Mutation: "NONE", Decision: "ALREADY_READY",
            Before: before.State, After: before.State,
        })
        return
    }
    if !probeReconcileAdmissible(before) {
        reject(http.StatusConflict, "STOP_INVENTORY", "only one verified production Xray with isolated probes is eligible", before.State, "")
        return
    }

    // Drain active owners and stop new probes before inspecting any orphan.
    quiesceCtx, cancelQuiesce := context.WithTimeout(context.Background(), 12*time.Second)
    release, ok := acquireExclusiveIsolatedXrayProbe(quiesceCtx)
    cancelQuiesce()
    if !ok {
        reject(http.StatusConflict, "STOP_PROBE_BUSY", "isolated diagnostics did not quiesce", before.State, "")
        return
    }
    defer release()

    guarded := a.workXrayProbeInventory()
    if !probeReconcileAdmissible(guarded) {
        if guarded.State == "ready" && guarded.RestartReady {
            writeJSON(w, http.StatusOK, probeReconcileResponse{
                Success: true, Mutation: "NONE", Decision: "ALREADY_READY",
                Before: before.State, After: guarded.State,
            })
            return
        }
        reject(http.StatusConflict, "STOP_INVENTORY_CHANGED", "probe identity changed during quiescence", before.State, guarded.State)
        return
    }

    // SIGTERM is allowed ONLY by the existing reaper after exact owned,
    // parent=1, binary inode, private 0700 confdir and pinned PID/start proof.
    reapCtx, cancelReap := context.WithTimeout(context.Background(), 5*time.Second)
    reapErr := reapVerifiedOrphanedXrayProbes(reapCtx)
    cancelReap()
    after := a.workXrayProbeInventory()
    if reapErr != nil || !after.Success || !after.RestartReady ||
        after.ProductionCount != 1 || after.ProbeCount != 0 ||
        after.UnknownCount != 0 || after.ZombieCount != 0 {
        writeJSON(w, http.StatusConflict, probeReconcileResponse{
            Mutation: "VERIFIED_PROBE_RECONCILE", Decision: "STOP_NOT_READY",
            Before: guarded.State, After: after.State,
            Error: "verified orphan recovery incomplete; do not start DNS or VPN mutation",
        })
        return
    }
    writeJSON(w, http.StatusOK, probeReconcileResponse{
        Success: true, Mutation: "VERIFIED_PROBE_RECONCILE",
        Decision: "READY", Before: guarded.State, After: after.State,
    })
}
