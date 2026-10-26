package main

import (
    "context"
    "io"
    "net/http"
    "os"
    "os/exec"
    "sync"
    "time"
)

// The existing Xray core must pass the exact live config before any version
// change. This opt-in test observes that same child without downloading or
// replacing anything, and without exposing stdout/stderr, paths or secrets.
const xrayCoreReadOnlyAdmissionTimeout = 12 * time.Second

var xrayCoreReadOnlyDiagnosticMu sync.Mutex

type xrayCoreReadOnlyDiagnosticResponse struct {
    Success bool `json:"success"`
    Valid bool `json:"valid"`
    Mutation string `json:"mutation"`
    Phase string `json:"phase"`
    Reason string `json:"reason,omitempty"`
    AdmissionMS int64 `json:"admission_ms,omitempty"`
    PreparationMS int64 `json:"preparation_ms,omitempty"`
    StartMS int64 `json:"start_ms,omitempty"`
    ChildStarted bool `json:"child_started"`
    ChildMS int64 `json:"child_ms,omitempty"`
    UserCPUMS int64 `json:"user_cpu_ms,omitempty"`
    SystemCPUMS int64 `json:"system_cpu_ms,omitempty"`
    ExitCode int `json:"exit_code,omitempty"`
}

func registerXrayCoreReadOnlyDiagnostic(mux *http.ServeMux, a *app) {
    mux.HandleFunc("GET /api/xray/core/read-only-test", a.requireAuth(a.handleXrayCoreReadOnlyDiagnostic))
}

// No stdout/stderr, source config, PID or real filesystem paths leave this
// function. The child runs under FreeNet's already established isolated-probe
// environment and never starts production listeners.
func runXrayCoreReadOnlyDiagnostic(ctx context.Context, binary, configDir, assetDir string) xrayCoreReadOnlyDiagnosticResponse {
    result := xrayCoreReadOnlyDiagnosticResponse{Success: true, Mutation: "NONE", Phase: "workspace"}
    started := time.Now()
    tmp, err := os.MkdirTemp("", "freenet-core-diagnostic-")
    if err != nil {
        result.Reason = "reason=xray-diagnostic-workspace-unavailable"
        return result
    }
    defer os.RemoveAll(tmp)
    result.Phase = "probe-alias"
    probePath, err := resolvedIsolatedXrayProbePath(tmp, binary)
    if err != nil {
        result.Reason = "reason=xray-diagnostic-probe-unavailable"
        return result
    }
    result.PreparationMS = time.Since(started).Milliseconds()
    result.Phase = "child-start"
    cmd := exec.CommandContext(ctx, probePath, "run", "-test", "-confdir", configDir)
    cmd.Env = isolatedXrayProbeEnv(append(os.Environ(), "XRAY_LOCATION_ASSET="+assetDir))
    cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
    cmd.WaitDelay = 2 * time.Second
    startAt := time.Now()
    if err := cmd.Start(); err != nil {
        result.StartMS = time.Since(startAt).Milliseconds()
        result.Reason = "reason=xray-diagnostic-child-start-failed"
        return result
    }
    result.ChildStarted = true
    result.StartMS = time.Since(startAt).Milliseconds()
    result.Phase = "child-wait"
    err = cmd.Wait()
    result.ChildMS = time.Since(startAt).Milliseconds()
    if cmd.ProcessState != nil {
        result.UserCPUMS = cmd.ProcessState.UserTime().Milliseconds()
        result.SystemCPUMS = cmd.ProcessState.SystemTime().Milliseconds()
        result.ExitCode = cmd.ProcessState.ExitCode()
    }
    if ctx.Err() != nil {
        result.Reason = "reason=xray-live-test-timeout"
        if ctx.Err() == context.Canceled {
            result.Reason = "reason=xray-diagnostic-canceled"
        }
        return result
    }
    if err != nil {
        result.Reason = "reason=xray-live-config-test-failed"
        return result
    }
    result.Valid = true
    result.Phase = "complete"
    return result
}

func (a *app) handleXrayCoreReadOnlyDiagnostic(w http.ResponseWriter, r *http.Request) {
    blocked := func(status int, reason string) {
        writeJSON(w, status, xrayCoreReadOnlyDiagnosticResponse{
            Success: false, Mutation: "NONE", Phase: "stopped", Reason: reason,
        })
    }
    state := xrayCoreOperationSnapshot().State
    if state == "RUNNING" || state == "STOP" || automationMutationBlockedState() {
        blocked(http.StatusConflict, "reason=xray-diagnostic-mutation-fence")
        return
    }
    if !xrayCoreReadOnlyDiagnosticMu.TryLock() {
        blocked(http.StatusConflict, "reason=xray-diagnostic-already-running")
        return
    }
    defer xrayCoreReadOnlyDiagnosticMu.Unlock()
    releaseOperation, ok := tryAcquireFreeNetOperation(a)
    if !ok {
        blocked(http.StatusConflict, "reason=xray-diagnostic-operation-busy")
        return
    }
    defer releaseOperation()
    binary, _, err := a.resolvedXrayCoreBinary()
    if err != nil {
        blocked(http.StatusServiceUnavailable, "reason=xray-diagnostic-binary-unavailable")
        return
    }
    configDir, err := a.xrayCoreLiveConfigDir()
    if err != nil {
        blocked(http.StatusServiceUnavailable, "reason=xray-diagnostic-config-unavailable")
        return
    }
    started := time.Now()
    admissionCtx, cancelAdmission := context.WithTimeout(r.Context(), xrayCoreReadOnlyAdmissionTimeout)
    releaseProbe, ok := acquireExclusiveIsolatedXrayProbe(admissionCtx)
    cancelAdmission()
    if !ok {
        blocked(http.StatusConflict, "reason=xray-diagnostic-probes-busy")
        return
    }
    defer releaseProbe()
    admissionMS := time.Since(started).Milliseconds()
    // Test only the already-installed core, and use the same exact semantic
    // deadline as the guarded updater. No backup, swap or restart occurs.
    testCtx, cancelTest := context.WithTimeout(r.Context(), xrayCoreSingleTestTimeout)
    defer cancelTest()
    result := runXrayCoreReadOnlyDiagnostic(testCtx, binary, configDir, a.routingAssetDir())
    result.AdmissionMS = admissionMS
    writeJSON(w, http.StatusOK, result)
}
