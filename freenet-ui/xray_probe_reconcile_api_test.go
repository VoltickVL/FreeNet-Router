package main

import "testing"

func TestProbeReconcileAdmissionFailClosed(t *testing.T) {
    samples := []struct {
        name string
        inv  xrayProcessInventoryResponse
        want bool
    }{
        {"one trusted core with one isolated probe", xrayProcessInventoryResponse{
            Success: true, State: "isolated-probe-active", ProductionCount: 1, ProbeCount: 1,
        }, true},
        {"one core and two isolated probes", xrayProcessInventoryResponse{
            Success: true, State: "isolated-probe-active", ProductionCount: 1, ProbeCount: 2,
        }, true},
        {"already quiescent has no cleanup target", xrayProcessInventoryResponse{
            Success: true, State: "ready", ProductionCount: 1, RestartReady: true,
        }, false},
        {"missing core", xrayProcessInventoryResponse{
            Success: true, State: "isolated-probe-active", ProbeCount: 1,
        }, false},
        {"two production cores", xrayProcessInventoryResponse{
            Success: true, State: "isolated-probe-active", ProductionCount: 2, ProbeCount: 1,
        }, false},
        {"unknown process", xrayProcessInventoryResponse{
            Success: true, State: "isolated-probe-active", ProductionCount: 1, ProbeCount: 1, UnknownCount: 1,
        }, false},
        {"zombie process", xrayProcessInventoryResponse{
            Success: true, State: "isolated-probe-active", ProductionCount: 1, ProbeCount: 1, ZombieCount: 1,
        }, false},
        {"no actual probe", xrayProcessInventoryResponse{
            Success: true, State: "isolated-probe-active", ProductionCount: 1,
        }, false},
        {"unreadable inventory", xrayProcessInventoryResponse{
            State: "isolated-probe-active", ProductionCount: 1, ProbeCount: 1,
        }, false},
        {"unknown overall state", xrayProcessInventoryResponse{
            Success: true, State: "unknown-xray-process", ProductionCount: 1, ProbeCount: 1,
        }, false},
    }
    for _, tt := range samples {
        t.Run(tt.name, func(t *testing.T) {
            got := probeReconcileAdmissible(tt.inv)
            if got != tt.want {
                t.Fatalf("admission=%v want=%v inventory=%+v", got, tt.want, tt.inv)
            }
        })
    }
}
