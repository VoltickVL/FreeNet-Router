package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderMeasuredPlanDistinctFromFreshRoute(t *testing.T) {
	plan, err := parseProviderPlan(strings.Replace(providerPlanOutput(testProviderID), "CANDIDATE_ROUTE_OK=yes", "CANDIDATE_ROUTE_OK=measured", 1))
	if err != nil || !plan.Success || !plan.CandidateValid || plan.CandidateRouteOK ||
		!plan.CandidateRouteMeasured || plan.Mutation != "NONE" {
		t.Fatalf("measured != freshly verified: %+v, err=%v", plan, err)
	}
	for _, status := range []string{"skipped", "unknown", "partial", ""} {
		plan, err := parseProviderPlan(strings.Replace(providerPlanOutput(testProviderID), "CANDIDATE_ROUTE_OK=yes", "CANDIDATE_ROUTE_OK="+status, 1))
		if err != nil || plan.CandidateRouteOK || plan.CandidateRouteMeasured || plan.CandidateValid {
			t.Fatalf("unverified route became eligible: %q %+v %v", status, plan, err)
		}
	}
}

func setupMeasuredPlanFixture(t *testing.T, guard *automationQualityAdmission) string {
	t.Helper()
	calls := filepath.Join(filepath.Dir(guard.app.cfg.ConfigPath), "measured-plan-calls")
	t.Setenv("FREENET_TEST_ROUTE_PLAN_CALLS", calls)
	t.Setenv("FREENET_TEST_ROUTE_PLAN_OUT", guard.app.cfg.OutPath)
	provider := writeFakeNetworkHelper(t, `
printf '%s measured=%s\n' "$1" "${FREENET_PROVIDER_PLAN_MEASURED:-unset}" >> "$FREENET_TEST_ROUTE_PLAN_CALLS"
case "$1" in
 plan)
   ROUTE=yes
   [ "${FREENET_PROVIDER_PLAN_MEASURED:-0}" = 1 ] && ROUTE=measured
   cat <<EOF
========== FreeNet Provider Plan ==========
PROFILE_ID=$2
PROFILE_NAME=Frankfurt, Germany, Extra
ENDPOINT=203.0.113.10:443
CURRENT_OUTBOUND=present
XRAY_RUNNING=yes
CANDIDATE_XRAY_VALID=yes
CANDIDATE_ROUTE_OK=$ROUTE
EXPECTED_DELTA=replace exactly one vless-reality outbound
EXPECTED_NO_DELTA=ISP/DNS/routing unchanged
MUTATION=NONE
========== END ==========
EOF
   exit 0 ;;
 apply-core)
   [ "${FREENET_PROVIDER_PLAN_MEASURED:-0}" = 0 ] || exit 17
   printf '%s\n' '{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"203.0.113.10","port":443}]}}]}' > "$FREENET_TEST_ROUTE_PLAN_OUT"
   printf '%s\n' '[FreeNet Provider] RESULT=SUCCESS'
   exit 0 ;;
 *) exit 18 ;;
esac`)
	network := writeFakeNetworkHelper(t, "[ \"$1\" = plan ] || exit 9\ncat <<'EOF'\n"+supportedPlanOutput()+"\nEOF")
	t.Setenv("FREENET_PROVIDER_HELPER", provider)
	t.Setenv("FREENET_NETWORK_HELPER", network)
	return calls
}

func TestAutoMeasuredPlanDefersOnlyDuplicateProbeToGuardedApply(t *testing.T) {
	guard, _ := persistentMediaGuardFixture(t)
	callsPath := setupMeasuredPlanFixture(t, guard)
	oldCandidate, oldCurrent := automationQualityCandidateProbe, automationQualityCurrentProbe
	t.Cleanup(func() {
		automationQualityCandidateProbe, automationQualityCurrentProbe = oldCandidate, oldCurrent
	})
	automationQualityCandidateProbe = func(_ *app, ctx context.Context, _ bestServerInternalCandidate) automationHealthProbe {
		if ctx.Err() != nil { t.Fatal("cancelled candidate") }
		p := automationQualityFreshCandidate()
		p.MediaComplete = true
		return p
	}
	automationQualityCurrentProbe = func(_ *app, ctx context.Context) automationHealthProbe {
		if ctx.Err() != nil { t.Fatal("cancelled current") }
		return healthyMediaCurrentAfterTransientRecovery(guard.trigger.CurrentIdentity)
	}
	status, result := guard.app.executeProviderProfileApplyWithAdmission(networkApplyRequest{
		Operation:"provider", ProfileID:guard.candidate.ID, SelectionToken:guard.token, Confirm:true,
	}, nil, &providerApplyAdmission{
		beforeQuiesce:guard.confirm, beforeMutation:guard.final, measuredRoutePlan:true,
	})
	calls, err := os.ReadFile(callsPath)
	if err != nil { t.Fatal(err) }
	if status != http.StatusOK || !result.Success || !result.Applied ||
		string(calls) != "plan measured=1\napply-core measured=0\n" ||
		len(guard.app.sem) != 0 || len(isolatedXrayProbeGate) != 0 ||
		guard.stopped != nil || guard.confirmedAt.IsZero() {
		t.Fatalf("route preview skipped too much or admission failed: status=%d result=%+v calls=%q stopped=%+v",
			status, result, string(calls), guard.stopped)
	}
}

func TestUnadmittedMeasuredPlanCannotAuthorizeProviderApply(t *testing.T) {
	for _, tc := range []struct {
		name string
		confirm, final bool
	}{
		{"neither guard", false, false},
		{"confirmation only", true, false},
		{"final only", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guard, _ := persistentMediaGuardFixture(t)
			callsPath := setupMeasuredPlanFixture(t, guard)
			// Simulate an untrusted helper reporting MEASURED even though
			// AUTO did not authorize the shortcut.
			provider := writeFakeNetworkHelper(t, `
printf '%s measured=%s\n' "$1" "${FREENET_PROVIDER_PLAN_MEASURED:-unset}" >> "$FREENET_TEST_ROUTE_PLAN_CALLS"
if [ "$1" = plan ]; then
cat <<EOF
========== FreeNet Provider Plan ==========
PROFILE_ID=$2
PROFILE_NAME=Frankfurt, Germany, Extra
ENDPOINT=203.0.113.10:443
CURRENT_OUTBOUND=present
XRAY_RUNNING=yes
CANDIDATE_XRAY_VALID=yes
CANDIDATE_ROUTE_OK=measured
MUTATION=NONE
========== END ==========
EOF
exit 0
fi
exit 19`)
			t.Setenv("FREENET_PROVIDER_HELPER", provider)
			admission := &providerApplyAdmission{measuredRoutePlan:true}
			if tc.confirm {
				admission.beforeQuiesce = func(bestServerInternalCandidate) error {t.Fatal("unsafe plan reached confirmation");return nil}
			}
			if tc.final {
				admission.beforeMutation = func() error {t.Fatal("unsafe plan reached mutation");return nil}
			}
			status, result := guard.app.executeProviderProfileApplyWithAdmission(networkApplyRequest{
				Operation:"provider", ProfileID:guard.candidate.ID, SelectionToken:guard.token, Confirm:true,
			}, nil, admission)
			calls, err := os.ReadFile(callsPath)
			if err != nil {t.Fatal(err)}
			if status != http.StatusConflict || result.Applied || result.RollbackState != "NOT_APPLIED" ||
				string(calls) != "plan measured=0\n" {
				t.Fatalf("unverified plan authorized mutation: status=%d result=%+v calls=%q",status,result,string(calls))
			}
		})
	}
}

func TestOrdinaryProviderPlanStillRequiresFreshRoute(t *testing.T) {
	guard, candidate := automationQualityAdmissionFixture(t)
	callsPath := setupMeasuredPlanFixture(t, guard)
	plan, err := guard.app.runProviderSelectionPlan(candidate)
	calls, readErr := os.ReadFile(callsPath)
	if readErr != nil {t.Fatal(readErr)}
	if err != nil || !plan.Success || !plan.CandidateValid || !plan.CandidateRouteOK ||
		plan.CandidateRouteMeasured || plan.Mutation != "NONE" ||
		string(calls) != "plan measured=0\n" {
		t.Fatalf("ordinary plan lost its route preflight: %+v err=%v calls=%q",plan,err,string(calls))
	}
}
