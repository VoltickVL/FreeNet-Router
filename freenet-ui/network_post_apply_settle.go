package main

import "time"

// A controlled XKeen restart can briefly make the first read-only post-apply
// plan report an unverified Xray identity. Do not start a second mutation or
// rollback on one transient snapshot. Recheck a strictly bounded number of
// times; a persistent or untrustworthy result remains a hard failure.
const (
	splitDNSPostApplyRechecks = 3
	splitDNSPostApplyInterval = 2 * time.Second
)

func verifiedSplitDNSPostApplyPlan(plan networkPlanResponse, err error) bool {
	return err == nil &&
		plan.Success && plan.Supported && plan.Active &&
		plan.Mutation == "NONE" &&
		plan.DNSMode == "xkeen" && plan.EffectiveDNSMode == "xkeen"
}

func settleSplitDNSPostApplyPlan(
	first networkPlanResponse,
	firstErr error,
	readPlan func() (networkPlanResponse, error),
	pause func(time.Duration),
) (networkPlanResponse, error) {
	if verifiedSplitDNSPostApplyPlan(first, firstErr) {
		return first, nil
	}
	// Never retry malformed, unsupported or erroring plans; no mutation is
	// authorized by this helper, and those results must fail closed.
	if firstErr != nil || !first.Success || !first.Supported || first.Mutation != "NONE" {
		return first, firstErr
	}

	latest, lastErr := first, firstErr
	for attempt := 0; attempt < splitDNSPostApplyRechecks; attempt++ {
		pause(splitDNSPostApplyInterval)
		latest, lastErr = readPlan()
		if verifiedSplitDNSPostApplyPlan(latest, lastErr) {
			return latest, nil
		}
		if lastErr != nil || !latest.Success || !latest.Supported || latest.Mutation != "NONE" {
			break
		}
	}
	return latest, lastErr
}
