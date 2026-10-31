package main

import (
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

const (
	automationServiceQualityMinObservations = 4
	automationServiceQualityMinSpan         = 2 * time.Minute
)

// A quality comparison carries its original, identity-bound reason through
// the long scan. The final paired check must confirm that reason again.
type automationQualityTrigger struct {
	CurrentIdentity string
	ServiceBadMask  uint8
	LatencyDegraded bool
	MediaDegraded   bool
	// BUSY before a scan may refund only its own exact cooldown slot.
	ReservedAt      string
}

func validAutomationQualityIdentity(identity string) bool {
	if len(identity) != 64 || identity != strings.ToLower(identity) {
		return false
	}
	_, err := hex.DecodeString(identity)
	return err == nil
}

func automationServiceProbeComplete(probe automationHealthProbe) bool {
	return probe.ServiceComplete && probe.ServiceOKMask&probe.ServiceBadMask == 0 &&
		probe.ServiceOKMask|probe.ServiceBadMask == bestServerServiceAllMask &&
		validAutomationQualityIdentity(probe.CurrentIdentity)
}

func clearAutomationServiceEvidence(updates map[string]string) {
	updates["QUALITY_SERVICE_IDENTITY"] = ""
	updates["QUALITY_SERVICE_BAD_MASK"] = "0"
	updates["QUALITY_SERVICE_COUNT"] = "0"
	updates["QUALITY_SERVICE_SINCE"] = ""
	updates["QUALITY_SERVICE_LAST"] = ""
}

func automationServiceEvidencePresent(state map[string]string) bool {
	for _, key := range []string{"QUALITY_SERVICE_IDENTITY", "QUALITY_SERVICE_BAD_MASK", "QUALITY_SERVICE_COUNT", "QUALITY_SERVICE_SINCE", "QUALITY_SERVICE_LAST"} {
		if value := strings.TrimSpace(state[key]); value != "" && value != "0" {
			return true
		}
	}
	return false
}

// The return mask is the same service failure observed throughout the whole
// sequence. This permits a comparison only; it never authorizes an apply or
// contributes to the independent global-VPN failure/emergency classifier.
func automationServiceQualityOptimizationPlan(state map[string]string, now time.Time, probe automationHealthProbe) (map[string]string, uint8) {
	updates := map[string]string{}
	if now.IsZero() || probe.State != automationHealthUncertain || !probe.QualityDegraded ||
		!automationServiceProbeComplete(probe) || probe.ServiceBadMask == 0 {
		if automationServiceEvidencePresent(state) {
			clearAutomationServiceEvidence(updates)
		}
		return updates, 0
	}
	now = now.UTC()
	count, countErr := strconv.Atoi(strings.TrimSpace(state["QUALITY_SERVICE_COUNT"]))
	maskValue, maskErr := strconv.ParseUint(strings.TrimSpace(state["QUALITY_SERVICE_BAD_MASK"]), 10, 8)
	start, startErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(state["QUALITY_SERVICE_SINCE"]))
	last, lastErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(state["QUALITY_SERVICE_LAST"]))
	mask := uint8(maskValue)
	validSequence := state["QUALITY_SERVICE_IDENTITY"] == probe.CurrentIdentity &&
		countErr == nil && count > 0 && count <= automationServiceQualityMinObservations &&
		maskErr == nil && mask != 0 && mask&^bestServerServiceAllMask == 0 &&
		startErr == nil && lastErr == nil && !last.Before(start) &&
		!now.Before(last) && now.Sub(start) <= automationQualityStrikeWindow
	if validSequence && now.Equal(last) {
		return updates, 0 // one observation cannot increment its own sequence twice
	}
	if !validSequence || mask&probe.ServiceBadMask == 0 {
		count, start, mask = 0, now, probe.ServiceBadMask
	} else {
		mask &= probe.ServiceBadMask
	}
	count++
	if count > automationServiceQualityMinObservations {
		count = automationServiceQualityMinObservations
	}
	updates["QUALITY_SERVICE_IDENTITY"] = probe.CurrentIdentity
	updates["QUALITY_SERVICE_BAD_MASK"] = strconv.Itoa(int(mask))
	updates["QUALITY_SERVICE_COUNT"] = strconv.Itoa(count)
	updates["QUALITY_SERVICE_SINCE"] = start.Format(time.RFC3339Nano)
	updates["QUALITY_SERVICE_LAST"] = now.Format(time.RFC3339Nano)

	if raw := strings.TrimSpace(state["QUALITY_OPTIMIZATION_LAST"]); raw != "" {
		lastOptimization, err := time.Parse(time.RFC3339, raw)
		if err != nil || now.Before(lastOptimization.Add(automationQualityOptimizationCooldown)) {
			return updates, 0
		}
	}
	if count < automationServiceQualityMinObservations || now.Sub(start) < automationServiceQualityMinSpan {
		return updates, 0
	}
	clearAutomationServiceEvidence(updates)
	updates["QUALITY_OPTIMIZATION_LAST"] = now.Format(time.RFC3339)
	return updates, mask
}

func automationReachableQualityOptimizationPlan(state map[string]string, now time.Time, probe automationHealthProbe) (map[string]string, automationQualityTrigger, bool) {
	updates := map[string]string{}
	trigger := automationQualityTrigger{}
	latencyDue := false
	// Preserve pending latency strikes across an unknown probe, as before.
	// Only a complete current probe can add evidence or start a comparison.
	if probe.State == automationHealthHealthy ||
		(probe.State == automationHealthUncertain && probe.QualityDegraded && automationServiceProbeComplete(probe) && probe.QualityPoints >= 0 && probe.QualityPoints <= 3) {
		latencyUpdates, due := automationQualityOptimizationPlan(state, now, probe.QualityPoints)
		for key, value := range latencyUpdates {
			updates[key] = value
		}
		latencyDue = due && probe.QualityPoints > 0 && automationServiceProbeComplete(probe)
	}
	serviceUpdates, serviceMask := automationServiceQualityOptimizationPlan(state, now, probe)
	for key, value := range serviceUpdates {
		updates[key] = value
	}
	mediaUpdates, mediaDue := automationMediaQualityOptimizationPlan(state, now, probe)
	for key, value := range mediaUpdates {
		updates[key] = value
	}
	if !latencyDue && serviceMask == 0 && !mediaDue {
		return updates, trigger, false
	}
	// Both signals share one persisted attempt and one cooldown reservation.
	// Clear both pending sequences so a second signal cannot launch a second
	// comparison immediately after the first one completes.
	clearAutomationServiceEvidence(updates)
	clearAutomationMediaEvidence(updates)
	updates["QUALITY_DEGRADED_COUNT"] = "0"
	updates["QUALITY_DEGRADED_SAMPLES"] = "0"
	updates["QUALITY_DEGRADED_SINCE"] = ""
	updates["QUALITY_OPTIMIZATION_LAST"] = now.UTC().Format(time.RFC3339)
	trigger = automationQualityTrigger{
		CurrentIdentity: probe.CurrentIdentity,
		ServiceBadMask:  serviceMask,
		LatencyDegraded: latencyDue,
		MediaDegraded: mediaDue,
		ReservedAt:    now.UTC().Format(time.RFC3339),
	}
	return updates, trigger, true
}

func automationReachableQualityOptimizationDue(now time.Time, probe automationHealthProbe) (automationQualityTrigger, bool) {
	// The current media sample remains visible in the health journal, but a
	// recently successful profile switch cannot launch ANOTHER media-only
	// Top-3 scan. Corroborated service/latency evidence and emergency
	// connectivity failure are handled by their separate existing paths.
	if probe.MediaDegraded && probe.QualityPoints == 0 && probe.ServiceBadMask == 0 &&
		automationMediaSwitchSettling(now, parseAutomationState(automationStatePath())["LAST_SWITCH"]) {
		probe.MediaComplete = false
		probe.MediaDegraded = false
	}
	trigger := automationQualityTrigger{}
	due := false
	err := v3UpdateState(func(state map[string]string) map[string]string {
		updates, plannedTrigger, plannedDue := automationReachableQualityOptimizationPlan(state, now, probe)
		trigger, due = plannedTrigger, plannedDue
		return updates
	})
	if err != nil {
		return automationQualityTrigger{}, false
	}
	return trigger, due
}

// A BUSY lock acquisition is not a measured comparison. Refund only our
// exact reservation; do not clear a newer or completed scan's cooldown.
func automationReleaseQualityOptimizationReservation(trigger automationQualityTrigger) {
	if !validAutomationQualityTrigger(trigger) || strings.TrimSpace(trigger.ReservedAt) == "" {
		return
	}
	_ = v3UpdateState(func(state map[string]string) map[string]string {
		if strings.TrimSpace(state["QUALITY_OPTIMIZATION_LAST"]) != trigger.ReservedAt {
			return nil
		}
		return map[string]string{"QUALITY_OPTIMIZATION_LAST": ""}
	})
}


// Complete a real quality comparison at its terminal time. Only the original
// reservation may be replaced; a newer observation/attempt is never overwritten.
// The emergency loss-of-connectivity path has independent retry rules.
func automationCompleteQualityOptimizationReservation(trigger automationQualityTrigger, finishedAt time.Time) {
	if !validAutomationQualityTrigger(trigger) || strings.TrimSpace(trigger.ReservedAt) == "" || finishedAt.IsZero() {
		return
	}
	reserved, err := time.Parse(time.RFC3339, trigger.ReservedAt)
	if err != nil || finishedAt.Before(reserved) {
		return
	}
	_ = v3UpdateState(func(state map[string]string) map[string]string {
		if strings.TrimSpace(state["QUALITY_OPTIMIZATION_LAST"]) != trigger.ReservedAt {
			return nil
		}
		return map[string]string{"QUALITY_OPTIMIZATION_LAST": finishedAt.UTC().Format(time.RFC3339)}
	})
}
