package main

import (
    "context"
    "strings"
    "testing"
    "time"
)

func TestAutoVPNMediaCurrentProbeIsOptIn(t *testing.T) {
    if autoVPNCurrentMediaProbe(context.Background()) {
        t.Fatal("ordinary/manual current-quality must retain strict Speedtest")
    }
    ctx := context.WithValue(context.Background(), autoVPNMediaCurrentProbeKey{}, true)
    if !autoVPNCurrentMediaProbe(ctx) {
        t.Fatal("media-only AUTO comparison lost its bounded serial current probe")
    }
    if autoVPNCurrentMediaProbe(context.WithValue(ctx, autoVPNMediaCurrentProbeKey{}, false)) {
        t.Fatal("false explicit flag unexpectedly enabled reduced measurement")
    }
}

func TestAutoVPNComparisonCooldownUsesCompletionAndExactReservation(t *testing.T) {
    a := automationQoECooldownFixture(t, time.Hour, "auto")
    _ = a
    start := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
    identity := strings.Repeat("a", 64)
    trigger := automationQualityTrigger{CurrentIdentity: identity, MediaDegraded: true, ReservedAt: start.Format(time.RFC3339)}
    if err := v3WriteState(map[string]string{"QUALITY_OPTIMIZATION_LAST": trigger.ReservedAt}); err != nil {
        t.Fatal(err)
    }
    end := start.Add(4*time.Minute)
    automationCompleteQualityOptimizationReservation(trigger, end)
    if got := v3ParseState(v3StatePath())["QUALITY_OPTIMIZATION_LAST"]; got != end.Format(time.RFC3339) {
        t.Fatalf("cooldown must start after completed deep scan, got %q", got)
    }
    // A slow scan followed by a new reservation must never erase the newer writer.
    newer := end.Add(time.Minute).Format(time.RFC3339)
    if err := v3WriteState(map[string]string{"QUALITY_OPTIMIZATION_LAST": newer}); err != nil { t.Fatal(err) }
    automationCompleteQualityOptimizationReservation(trigger, end.Add(time.Minute))
    automationReleaseQualityOptimizationReservation(trigger)
    if got := v3ParseState(v3StatePath())["QUALITY_OPTIMIZATION_LAST"]; got != newer {
        t.Fatalf("stale completion/refund overwrote new reservation: %q", got)
    }
}

func TestAutoVPNMediaCooldownDoesNotBlockEmergency(t *testing.T) {
    // This cooldown is for QoE comparison only, not confirmed VPN outage recovery.
    failed := classifyAutomationApplicationFailure(false, true)
    if got := classifyAutomationHealth(failed, true, failed); got.State != automationHealthCritical {
        t.Fatalf("emergency recovery must remain independent: %+v", got)
    }
}
