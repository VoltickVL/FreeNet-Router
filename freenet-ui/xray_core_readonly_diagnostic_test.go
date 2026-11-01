package main

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

func writeXrayReadOnlyTestFixture(t *testing.T, dir, contents string) string {
    t.Helper()
    path := filepath.Join(dir, "xray")
    if err := os.WriteFile(path, []byte(contents), 0700); err != nil {
        t.Fatal(err)
    }
    return path
}

func TestXrayCoreReadOnlyDiagnosticPassAndDoesNotTouchConfigs(t *testing.T) {
    dir := t.TempDir()
    configs := filepath.Join(dir, "configs")
    if err := os.Mkdir(configs, 0700); err != nil { t.Fatal(err) }
    original := []byte(`{"outbounds":[]}`)
    configPath := filepath.Join(configs, "04_outbounds.json")
    if err := os.WriteFile(configPath, original, 0600); err != nil { t.Fatal(err) }
    script := "#!/bin/sh\n[ \"$1\" = run ] && [ \"$2\" = -test ] && [ \"$3\" = -confdir ] && [ \"$FREENET_XRAY_PROBE\" = 1 ] || exit 23\nexit 0\n"
    binary := writeXrayReadOnlyTestFixture(t, dir, script)
    result := runXrayCoreReadOnlyDiagnostic(context.Background(), binary, configs, dir)
    if !result.Success || !result.Valid || !result.ChildStarted || result.Phase != "complete" || result.Mutation != "NONE" || result.Reason != "" {
        t.Fatalf("expected harmless semantic PASS, got %+v", result)
    }
    after, err := os.ReadFile(configPath)
    if err != nil || string(after) != string(original) {
        t.Fatal("read-only diagnostic altered live configuration")
    }
    if _, err := os.Stat(binary); err != nil { t.Fatal("read-only diagnostic altered core binary") }
}

func TestXrayCoreReadOnlyDiagnosticClassifiesTimeoutWithoutSecretLeak(t *testing.T) {
    dir := t.TempDir()
    script := "#!/bin/sh\necho 'private-vless-secret-and-config' >&2\nsleep 2\n"
    binary := writeXrayReadOnlyTestFixture(t, dir, script)
    ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
    defer cancel()
    result := runXrayCoreReadOnlyDiagnostic(ctx, binary, dir, dir)
    payload, err := json.Marshal(result)
    if err != nil { t.Fatal(err) }
    if !result.ChildStarted || result.Valid || result.Reason != "reason=xray-live-test-timeout" || result.Mutation != "NONE" {
        t.Fatalf("unexpected child timeout verdict: %+v", result)
    }
    if strings.Contains(string(payload), "private-vless-secret") || strings.Contains(string(payload), dir) {
        t.Fatal("unsafe child stdout/stderr or file paths escaped diagnostics")
    }
}

func TestXrayCoreReadOnlyDiagnosticClassifiesChildFailureWithoutLeak(t *testing.T) {
    dir := t.TempDir()
    binary := writeXrayReadOnlyTestFixture(t, dir, "#!/bin/sh\necho 'confidential-client-config' >&2\nexit 23\n")
    result := runXrayCoreReadOnlyDiagnostic(context.Background(), binary, dir, dir)
    payload, err := json.Marshal(result)
    if err != nil { t.Fatal(err) }
    if result.Valid || !result.ChildStarted || result.ExitCode != 23 || result.Reason != "reason=xray-live-config-test-failed" {
        t.Fatalf("expected classified semantic failure, got %+v", result)
    }
    if strings.Contains(string(payload), "confidential-client-config") || strings.Contains(string(payload), dir) {
        t.Fatal("confidential config leaked in diagnostic response")
    }
}

func TestXrayCoreReadOnlyDiagnosticRouteAuthenticated(t *testing.T) {
    a := &app{sem: make(chan struct{}, 1)}
    mux := http.NewServeMux()
    registerXrayCoreReadOnlyDiagnostic(mux, a)
    req := httptest.NewRequest(http.MethodGet, "/api/xray/core/read-only-test", nil)
    res := httptest.NewRecorder()
    mux.ServeHTTP(res, req)
    if res.Code == http.StatusOK || res.Code == http.StatusNotFound {
        t.Fatalf("read-only Xray diagnostic must exist and require auth, got HTTP %d", res.Code)
    }
}

func TestXrayCoreReadOnlyDiagnosticHTTPDoesNotChangeBinary(t *testing.T) {
    dir := t.TempDir()
    configDir := filepath.Join(dir, "configs")
    if err := os.Mkdir(configDir, 0700); err != nil { t.Fatal(err) }
    if err := os.WriteFile(filepath.Join(configDir, "04_outbounds.json"), []byte(`{"outbounds":[]}`), 0600); err != nil { t.Fatal(err) }
    binary := writeXrayReadOnlyTestFixture(t, dir, "#!/bin/sh\n[ \"$1\" = run ] && exit 0\nexit 0\n")
    original, _ := os.ReadFile(binary)
    t.Setenv("FREENET_XRAY_BIN", binary)
    t.Setenv("FREENET_ROUTING_CONFIG_DIR", configDir)
    t.Setenv("FREENET_XRAY_PROBE_LOCK_FILE", filepath.Join(dir, "probe.lock"))
    a := &app{cfg: config{OutPath: filepath.Join(configDir, "04_outbounds.json")}, sem: make(chan struct{}, 1)}
    res := httptest.NewRecorder()
    a.handleXrayCoreReadOnlyDiagnostic(res, httptest.NewRequest(http.MethodGet, "/api/xray/core/read-only-test", nil))
    var result xrayCoreReadOnlyDiagnosticResponse
    if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil { t.Fatal(err) }
    if res.Code != http.StatusOK || !result.Valid || result.Mutation != "NONE" {
        t.Fatalf("safe diagnostic request failed: HTTP %d %+v", res.Code, result)
    }
    after, err := os.ReadFile(binary)
    if err != nil || string(after) != string(original) {
        t.Fatal("read-only test changed Xray binary")
    }
}

func TestXrayCoreReadOnlyDiagnosticBrowserMustBeExplicit(t *testing.T) {
    data, err := os.ReadFile("web/xray-core-manager.js")
    if err != nil { t.Fatal(err) }
    content := string(data)
    begin := strings.Index(content, "const diagnosticButton = document.createElement('button');")
    end := strings.Index(content, "const search = document.createElement('input');")
    if begin < 0 || end <= begin { t.Fatal("read-only diagnostic is not rendered separately in Xray picker") }
    snippet := content[begin:end]
    for _, want := range []string{
        "Диагностика Xray (без обновления)",
        "diagnosticButton.addEventListener('click'",
        "fetch('/api/xray/core/read-only-test'",
        "result.mutation === 'NONE'",
        "result.child_ms",
        "result.user_cpu_ms",
        "result.system_cpu_ms",
        "Xray не изменён",
    } {
        if !strings.Contains(snippet, want) { t.Fatalf("missing browser diagnostic contract %q", want) }
    }
    for _, forbidden := range []string{"method:'POST'", "/api/xray/core/apply", "location.reload()"} {
        if strings.Contains(snippet, forbidden) { t.Fatalf("read-only Xray diagnostic may not mutate: %q", forbidden) }
    }
}

func TestXrayCoreUpdateReconcilesDetachedReceiptBeforeFailure(t *testing.T) {
    data, err := os.ReadFile("web/xray-core-manager.js")
    if err != nil { t.Fatal(err) }
    content := string(data)
    begin := strings.Index(content, "async function applyRelease(release)")
    if begin < 0 { t.Fatal("Xray apply UI missing") }
    s := content[begin:]
    if !strings.Contains(s, "const receipt = await readCoreUpdateReceipt();") ||
        !strings.Contains(s, "await renderCoreUpdateReceipt(release.version)") {
        t.Fatal("browser HTTP error cannot be classified before detached Xray operation receipt")
    }
    if strings.Index(s, "const receipt = await readCoreUpdateReceipt();") > strings.Index(s, "const rollbackFailed = result.rollback === 'FAILED';") {
        t.Fatal("detached worker receipt must be reconciled before terminal error")
    }
}


func TestXrayPendingReadOnlyDiagnosticGateFailClosed(t *testing.T) {
    d := vpnTransactionDoctorResponse{
        Mode: "read-only", Mutation: "NONE", Checkpoint: "pending",
        Outbound: "matched", Profile: "matched", Filter: "matched",
        FilesRestored: true, XrayCore: "stopped", MutationBlocked: true,
        LockState: "absent", Decision: "STOP_XRAY_NOT_CONFIRMED",
    }
    inv := xrayProcessInventoryResponse{
        Success: true, Mutation: "NONE", State: "production-xray-absent",
    }
    if !xrayPendingReadOnlyEligible(d, inv) {
        t.Fatal("verified stopped/matched checkpoint should permit only read-only diagnosis")
    }
    cases := []struct {
        name string
        breakProof func(*vpnTransactionDoctorResponse, *xrayProcessInventoryResponse)
    }{
        {"no-checkpoint", func(d *vpnTransactionDoctorResponse, _ *xrayProcessInventoryResponse) { d.Checkpoint = "absent" }},
        {"live-files-changed", func(d *vpnTransactionDoctorResponse, _ *xrayProcessInventoryResponse) { d.Outbound = "different" }},
        {"files-unrestored", func(d *vpnTransactionDoctorResponse, _ *xrayProcessInventoryResponse) { d.FilesRestored = false }},
        {"core-running", func(d *vpnTransactionDoctorResponse, _ *xrayProcessInventoryResponse) { d.XrayCore = "running" }},
        {"core-unknown", func(d *vpnTransactionDoctorResponse, _ *xrayProcessInventoryResponse) { d.XrayCore = "unknown" }},
        {"unknown-rollback", func(d *vpnTransactionDoctorResponse, _ *xrayProcessInventoryResponse) { d.Decision = "STOP_FILES_UNRESOLVED" }},
        {"recovery-interrupted", func(d *vpnTransactionDoctorResponse, _ *xrayProcessInventoryResponse) { d.RecoveryAttempt = "unresolved" }},
        {"orphan-writer", func(d *vpnTransactionDoctorResponse, _ *xrayProcessInventoryResponse) { d.LockState = "owner_pid_absent" }},
        {"active-writer", func(d *vpnTransactionDoctorResponse, _ *xrayProcessInventoryResponse) { d.LockState = "pid_present_unverified" }},
        {"untrusted-binary", func(_ *vpnTransactionDoctorResponse, inv *xrayProcessInventoryResponse) { inv.Success = false }},
        {"production-present", func(_ *vpnTransactionDoctorResponse, inv *xrayProcessInventoryResponse) { inv.ProductionCount = 1 }},
        {"probe-active", func(_ *vpnTransactionDoctorResponse, inv *xrayProcessInventoryResponse) { inv.ProbeCount = 1 }},
        {"unknown-process", func(_ *vpnTransactionDoctorResponse, inv *xrayProcessInventoryResponse) { inv.UnknownCount = 1 }},
        {"zombie-process", func(_ *vpnTransactionDoctorResponse, inv *xrayProcessInventoryResponse) { inv.ZombieCount = 1 }},
        {"inventory-unknown", func(_ *vpnTransactionDoctorResponse, inv *xrayProcessInventoryResponse) { inv.State = "inventory-unknown" }},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            dd, ii := d, inv
            tc.breakProof(&dd, &ii)
            if xrayPendingReadOnlyEligible(dd, ii) {
                t.Fatal("missing checkpoint/process proof bypassed fail-closed gate")
            }
        })
    }
}

func TestXrayDiagnosticHookStateClassifiesRemovedModuleWithoutExecuting(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "proxy.sh")
    if xrayDiagnosticHookState(path) != "absent" {
        t.Fatal("absent hook must remain distinct")
    }
    secret := "SENSITIVE-SECRET-SHOULD-NEVER-APPEAR"
    legacy := "#!/bin/sh\n. '/opt/sbin/.xkeen/01_info/03_info_cpu.sh'\n# " + secret
    if err := os.WriteFile(path, []byte(legacy), 0700); err != nil { t.Fatal(err) }
    if got := xrayDiagnosticHookState(path); got != "legacy-removed-module-reference" || strings.Contains(got, secret) {
        t.Fatalf("legacy hook classified unsafely: %q", got)
    }
    if err := os.WriteFile(path, []byte("#!/bin/sh\n# XKeen: Auto-generated file. DO NOT EDIT!\n"), 0700); err != nil { t.Fatal(err) }
    if xrayDiagnosticHookState(path) != "modern-generated-hook" {
        t.Fatal("verified modern hook header not recognized")
    }
    if err := os.Remove(path); err != nil { t.Fatal(err) }
    if err := os.Symlink(filepath.Join(dir, "missing"), path); err == nil {
        if xrayDiagnosticHookState(path) != "untrusted" {
            t.Fatal("symlinked hook must never be trusted")
        }
    }
}

func TestXrayReadOnlyDiagnosticDuringPendingPreservesCheckpoint(t *testing.T) {
    a, checkpoint, live := newVPNTransactionDoctorFixture(t)
    dir := t.TempDir()
    proc := filepath.Join(dir, "proc")
    if err := os.Mkdir(proc, 0700); err != nil { t.Fatal(err) }
    t.Setenv("FREENET_XRAY_PROC_ROOT", proc)
    t.Setenv("FREENET_XRAY_PROBE_LOCK_FILE", filepath.Join(dir, "probe.lock"))
    t.Setenv("FREENET_ROUTING_CONFIG_DIR", filepath.Dir(live["out"]))
    bin := writeXrayReadOnlyTestFixture(t, dir, "#!/bin/sh\necho 'SENSITIVE-VLESS-SECRET' >&2\nexit 23\n")
    t.Setenv("FREENET_XRAY_BIN", bin)
    a.cfg.LockPath = filepath.Join(dir, "unheld-writer-lock")
    before, err := os.ReadFile(live["out"])
    if err != nil { t.Fatal(err) }
    response := httptest.NewRecorder()
    a.handleXrayCoreReadOnlyDiagnostic(response,
        httptest.NewRequest(http.MethodGet, "/api/xray/core/read-only-test", nil))
    var result xrayCoreReadOnlyDiagnosticResponse
    if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil { t.Fatal(err) }
    if response.Code != http.StatusOK || result.Mutation != "NONE" ||
        !result.ChildStarted || result.Valid || result.Reason != "reason=xray-live-config-test-failed" {
        t.Fatalf("pending read-only semantic FAIL was not safely classified: HTTP %d %+v", response.Code, result)
    }
    if !vpnTransactionPending() { t.Fatal("read-only diagnostic removed original checkpoint") }
    after, err := os.ReadFile(live["out"])
    if err != nil || string(before) != string(after) {
        t.Fatal("read-only diagnostic changed live VPN outbound")
    }
    if _, err := os.Stat(filepath.Join(checkpoint, "state")); err != nil {
        t.Fatal("read-only diagnostic altered durable checkpoint marker")
    }
    if strings.Contains(response.Body.String(), "SENSITIVE-VLESS-SECRET") ||
        strings.Contains(response.Body.String(), checkpoint) {
        t.Fatal("private data leaked from semantic diagnostic")
    }
}
