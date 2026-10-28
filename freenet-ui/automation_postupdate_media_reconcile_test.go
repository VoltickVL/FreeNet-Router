package main

import (
    "os"
    "path/filepath"
    "testing"
)

func automationPostUpdateMediaFixture(t *testing.T) (*app, automationHealthProbe) {
    t.Helper()
    dir := t.TempDir()
    out := filepath.Join(dir, "04_outbounds.json")
    filter := filepath.Join(dir, "active.filter")
    update := filepath.Join(dir, "update.state")
    t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "auto.state"))
    t.Setenv("FREENET_AUTOMATION_HISTORY", filepath.Join(dir, "auto.history"))
    t.Setenv("FREENET_VPN_TRANSACTION_DIR", filepath.Join(dir, "vpn-transaction.pending"))
    if err := os.WriteFile(out, []byte(`{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"192.0.2.55","port":443}]}}]}`), 0600); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filter, []byte("BE Brussels, Belgium, Extra\n"), 0600); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(update, []byte("STATE=SUCCESS\nTARGET_VERSION=v"+version+"\n"), 0600); err != nil {
        t.Fatal(err)
    }
    a := &app{cfg: config{
        OutPath: out, FilterPath: filter, UpdateState: update,
        UpdateLock: filepath.Join(dir, "update.lock"),
        LockPath: filepath.Join(dir, "writer.lock"),
    }}
    endpoint := readBestServerCurrentEndpoint(out)
    if endpoint != "192.0.2.55:443" {
        t.Fatalf("bad current endpoint fixture: %q", endpoint)
    }
    probe := automationHealthProbe{
        State: automationHealthUncertain, QualityDegraded: true,
        MediaComplete: true, MediaDegraded: true,
        ServiceOKMask: 1,
        CurrentIdentity: bestServerCurrentQualityIdentityHash(endpoint, readBestServerCurrentFilter(filter)),
    }
    if !automationExactDegradedMedia(a, probe) {
        t.Fatal("fixture must prove completed media degradation over exact selected VPN")
    }
    return a, probe
}

func TestAutoVPNReconcilesBeta47BusyLatchAndAllowsMediaFailover(t *testing.T) {
    a, probe := automationPostUpdateMediaFixture(t)
    writeAutomationStateV2("failed",
        "Подтверждённый VPN не применён: another FreeNet operation is already running", "unknown", false)
    if !automationMutationBlockedState() {
        t.Fatal("legacy beta.47 UNKNOWN was not latched")
    }
    reconciled, blocked := automationRollbackGuardResult(a, probe)
    if blocked || reconciled.State != "" || automationMutationBlockedState() {
        t.Fatalf("proven pre-mutation writer conflict not reconciled: %+v, blocked=%v", reconciled, blocked)
    }
    after, blocked := automationPostUpdateGuardResult(a, probe)
    if blocked || after.State != "" || automationPendingPostUpdateTarget(a) != "" {
        t.Fatalf("degraded but reachable VPN was trapped by post-update hold: %+v, blocked=%v", after, blocked)
    }
    if _, err := os.Stat(vpnTransactionCheckpointPath()); !os.IsNotExist(err) {
        t.Fatalf("read-only reconciliation created a transaction checkpoint: %v", err)
    }
    if got := parseAutomationState(automationStatePath()); got["MUTATION_BLOCKED"] != "no" ||
        got["POST_UPDATE_ACK"] != "v"+version {
        t.Fatalf("wrong recovery state %+v", got)
    }
}

func TestAutoVPNMediaPostUpdateHoldAllowsOnlyExactDegradedEvidence(t *testing.T) {
    for _, tc := range []struct {
        name string
        change func(*automationHealthProbe)
    }{
        {"incomplete media", func(p *automationHealthProbe) { p.MediaComplete = false }},
        {"missing application success", func(p *automationHealthProbe) { p.ServiceOKMask = 0 }},
        {"stale different VPN identity", func(p *automationHealthProbe) { p.CurrentIdentity = "wrong" }},
        {"unknown non-media state", func(p *automationHealthProbe) { p.MediaDegraded = false }},
    } {
        t.Run(tc.name, func(t *testing.T) {
            a, probe := automationPostUpdateMediaFixture(t)
            tc.change(&probe)
            result, blocked := automationPostUpdateGuardResult(a, probe)
            if !blocked || result.State != automationHealthUncertain || automationPendingPostUpdateTarget(a) == "" {
                t.Fatalf("incomplete proof escaped post-update STOP: %+v blocked=%v", result, blocked)
            }
        })
    }
}

func TestAutoVPNLegacyBusyReconciliationKeepsActualUnknownAndLocks(t *testing.T) {
    for _, tc := range []struct {
        name string
        reason string
        rollback string
        checkpoint bool
        lock bool
    }{
        {"real rollback", "real apply rollback unknown", "unknown", false, false},
        {"real failed rollback", "Подтверждённый VPN не применён: another FreeNet operation is already running", "FAILED/UNKNOWN", false, false},
        {"pending checkpoint", "Подтверждённый VPN не применён: another FreeNet operation is already running", "unknown", true, false},
        {"owner lock present", "Подтверждённый VPN не применён: another FreeNet operation is already running", "unknown", false, true},
    } {
        t.Run(tc.name, func(t *testing.T) {
            a, probe := automationPostUpdateMediaFixture(t)
            writeAutomationStateV2("failed", tc.reason, tc.rollback, false)
            if tc.checkpoint {
                if err := os.Mkdir(vpnTransactionCheckpointPath(), 0700); err != nil { t.Fatal(err) }
            }
            if tc.lock {
                if err := os.Mkdir(a.cfg.LockPath, 0700); err != nil { t.Fatal(err) }
            }
            result, blocked := automationRollbackGuardResult(a, probe)
            if !blocked || result.State != automationHealthUncertain || !automationMutationBlockedState() {
                t.Fatalf("unproven UNKNOWN was cleared: %+v blocked=%v", result, blocked)
            }
            _, blocked = automationPostUpdateGuardResult(a, probe)
            if !blocked || automationPendingPostUpdateTarget(a) == "" {
                t.Fatal("genuine unresolved rollback bypassed post-update hold")
            }
        })
    }
}
