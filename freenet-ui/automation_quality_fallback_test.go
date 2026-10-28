package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture uses only synthetic TEST-ID-A/B profiles. No subscription or
// runtime secrets, external hosts or real Xray processes are involved.
func automationQualityTwoCandidateFixture(t *testing.T) (*automationQualityAdmission, bestServerQualityResponse, bestServerInternalCandidate, bestServerInternalCandidate) {
	t.Helper()
	guard, first := automationQualityAdmissionFixture(t)
	if !strings.Contains(first.Raw, "TEST-ID-A@203.0.113.10:443") {
		t.Fatal("synthetic first profile changed")
	}
	secondRaw := strings.Replace(first.Raw, "TEST-ID-A@203.0.113.10:443", "TEST-ID-B@203.0.113.11:443", 1)
	secondProfile, ok := parseSafeVLESSProfile(secondRaw)
	if !ok || secondProfile.ID == first.Profile.ID || endpointsEqual(profileEndpoint(secondProfile), profileEndpoint(first.Profile)) {
		t.Fatal("synthetic fallback must be an independently selectable endpoint")
	}
	second := bestServerInternalCandidate{Profile: secondProfile, Raw: secondRaw}
	measured := guard.candidate
	measured.ID, measured.Name, measured.CountryCode = secondProfile.ID, secondProfile.Name, secondProfile.CountryCode
	measured.Endpoint, measured.Current = profileEndpoint(secondProfile), false
	measured.Tested, measured.Available, measured.Eligible = true, true, true
	filter := readBestServerCurrentFilter(guard.app.cfg.FilterPath)
	endpoint := readBestServerCurrentEndpoint(guard.app.cfg.OutPath)
	token, err := guard.app.storeBestServerSelectionSnapshot(endpoint, filter,
		[]bestServerInternalCandidate{first, second}, []bestServerQualityCandidate{guard.candidate, measured})
	if err != nil || token == "" {
		t.Fatalf("measured two-candidate snapshot: %v", err)
	}
	return guard, bestServerQualityResponse{
		Success: true, CurrentEndpoint: endpoint, CurrentIdentity: guard.trigger.CurrentIdentity,
		Candidates: []bestServerQualityCandidate{guard.candidate, measured}, SelectionToken: token,
	}, first, second
}

func TestAutomationQualityFallbackIsDistinctOneTimeMeasuredSnapshot(t *testing.T) {
	guard, scan, first, second := automationQualityTwoCandidateFixture(t)
	got, token := guard.app.prepareAutomationQualityFallback(scan, scan.Candidates[0])
	if token == "" || got.ID != second.Profile.ID || token == scan.SelectionToken {
		t.Fatalf("fallback did not get an independent measured token: %+v / %q", got, token)
	}
	defer consumeBestServerSelectionSnapshot(token)
	filter := readBestServerCurrentFilter(guard.app.cfg.FilterPath)
	consumeBestServerSelectionSnapshot(scan.SelectionToken)
	selected, manual, err := guard.app.loadBestServerSelectionCandidateWithPurpose(token,
		second.Profile.ID, scan.CurrentEndpoint, filter)
	if err != nil || manual || selected.Raw != second.Raw {
		t.Fatalf("fallback was not independently authenticated after first token was consumed: %v", err)
	}
	if _, _, err := guard.app.loadBestServerSelectionCandidateWithPurpose(token, first.Profile.ID, scan.CurrentEndpoint, filter); err == nil {
		t.Fatal("fallback token also authorized the previously rejected candidate")
	}
	scan.Candidates[1].Endpoint = scan.Candidates[0].Endpoint
	if _, dupToken := guard.app.prepareAutomationQualityFallback(scan, scan.Candidates[0]); dupToken != "" {
		consumeBestServerSelectionSnapshot(dupToken)
		t.Fatal("same endpoint must not be attempted as an independent fallback")
	}
	if err := os.WriteFile(guard.app.cfg.SubPath, []byte("https://provider.example.invalid/rotated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := guard.app.loadBestServerSelectionCandidateWithPurpose(token, second.Profile.ID,
		scan.CurrentEndpoint, filter); err == nil {
		t.Fatal("fallback bypassed subscription fingerprint drift")
	}
}

func TestAutomationQualityFallbackOnlyAfterPreMutationCandidateFailure(t *testing.T) {
	oldCurrentQuality, oldForeign := automationBestCurrentQuality, automationMediaForeignScan
	oldCandidate, oldCurrent := automationQualityCandidateProbe, automationQualityCurrentProbe
	t.Cleanup(func() {
		automationBestCurrentQuality, automationMediaForeignScan = oldCurrentQuality, oldForeign
		automationQualityCandidateProbe, automationQualityCurrentProbe = oldCandidate, oldCurrent
	})
	for _, tc := range []struct {
		name string
		firstBad, secondBad, recovered bool
		want, calls string
	}{
		{"fallback replaces a bad first candidate", true, false, false, "switched", "plan\nplan\napply-core\n"},
		{"both candidates fail, current untouched", true, true, false, "uncertain", "plan\nplan\n"},
		{"current recovered, no fallback", false, false, true, "same", "plan\n"},
	} {
		t.Run(tc.name,func(t *testing.T) {
			guard, scan, first, second := automationQualityTwoCandidateFixture(t)
			// The watchdog has identity-bound slow media evidence. A slow
			// current was measured; eligible alternatives were measured too.
			guard.trigger = automationQualityTrigger{
				CurrentIdentity: guard.trigger.CurrentIdentity, MediaDegraded: true,
			}
			callsPath := filepath.Join(filepath.Dir(guard.app.cfg.ConfigPath),"provider-calls")
			t.Setenv("FREENET_TEST_QUALITY_CALLS", callsPath)
			t.Setenv("FREENET_TEST_QUALITY_OUT", guard.app.cfg.OutPath)
			provider := writeFakeNetworkHelper(t, `
printf '%s\n' "$1" >> "$FREENET_TEST_QUALITY_CALLS"
if grep -F 'TEST-ID-B@203.0.113.11:443' "$FREENET_PROVIDER_SUBSCRIPTION_CACHE" >/dev/null; then
  endpoint=203.0.113.11:443
  address=203.0.113.11
else
  endpoint=203.0.113.10:443
  address=203.0.113.10
fi
cat <<EOF
========== FreeNet Provider Plan ==========
PROFILE_ID=$2
PROFILE_NAME=Frankfurt, Germany, Extra
ENDPOINT=$endpoint
CURRENT_OUTBOUND=present
XRAY_RUNNING=yes
CANDIDATE_XRAY_VALID=yes
CANDIDATE_ROUTE_OK=yes
EXPECTED_DELTA=replace exactly one vless-reality outbound
EXPECTED_NO_DELTA=ISP/DNS/routing unchanged
MUTATION=NONE
========== END ==========
EOF
if [ "$1" = apply-core ]; then
  cat > "$FREENET_TEST_QUALITY_OUT" <<EOF
{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"$address","port":443}]}}]}
EOF
  echo '[FreeNet Provider] RESULT=SUCCESS'
fi
exit 0`)
			network := writeFakeNetworkHelper(t, "[ \"$1\" = plan ] || exit 9\ncat <<'EOF'\n"+supportedPlanOutput()+"\nEOF")
			t.Setenv("FREENET_PROVIDER_HELPER", provider)
			t.Setenv("FREENET_NETWORK_HELPER", network)
			probeNames := []string{}
			automationBestCurrentQuality = func(*app, context.Context) (bestServerQualityResponse,error) {
				return guard.baseline,nil
			}
			automationMediaForeignScan = func(*app, context.Context, automationSettings, string) (bestServerQualityResponse,error) {
				return scan,nil
			}
			automationQualityCandidateProbe = func(_ *app, ctx context.Context, selected bestServerInternalCandidate) automationHealthProbe {
				if ctx.Err()!=nil {t.Fatal("probe deadline expired")}
				probeNames=append(probeNames,selected.Profile.ID)
				probe:=automationQualityFreshCandidate()
				probe.MediaComplete=true
				if (selected.Profile.ID==first.Profile.ID && tc.firstBad) ||
					(selected.Profile.ID==second.Profile.ID && tc.secondBad) {
					probe.State=automationHealthUncertain
					probe.QualityDegraded=true
					probe.MediaDegraded=true
				}
				return probe
			}
			automationQualityCurrentProbe=func(_ *app,ctx context.Context)automationHealthProbe{
				p:=autoMediaHealth(guard.trigger.CurrentIdentity,true,true)
				if tc.recovered {p=autoMediaHealth(guard.trigger.CurrentIdentity,false,true)}
				return p
			}
			settings:=readAutomationSettings(guard.app.cfg.ConfigPath)
			result,err:=guard.app.runAutomationQualityCycle(context.Background(),settings,guard.trigger)
			if err!=nil || result.Result!=tc.want || result.Mutated!=(tc.want=="switched") {
				t.Fatalf("quality result=%+v err=%v want=%s",result,err,tc.want)
			}
			gotCalls,readErr:=os.ReadFile(callsPath)
			if readErr!=nil || string(gotCalls)!=tc.calls {
				t.Fatalf("helper calls=%q err=%v want %q",gotCalls,readErr,tc.calls)
			}
			wantProbes:=1
			if tc.firstBad {wantProbes=2}
			if len(probeNames)!=wantProbes || probeNames[0]!=first.Profile.ID ||
				(wantProbes==2 && probeNames[1]!=second.Profile.ID) {
				t.Fatalf("candidate validation order = %v; need first and at most one distinct fallback",probeNames)
			}
			if tc.want=="switched" {
				if result.ProfileID!=second.Profile.ID || readBestServerCurrentEndpoint(guard.app.cfg.OutPath)!=profileEndpoint(second.Profile) {
					t.Fatalf("fallback did not become live exact selection: %+v",result)
				}
			} else if readBestServerCurrentEndpoint(guard.app.cfg.OutPath)!=guard.baseline.CurrentEndpoint {
				t.Fatal("rejected candidates or current recovery changed the working VPN")
			}
			if result.Result!="switched" && result.RollbackState!="NOT_APPLIED" {
				t.Fatalf("failed candidate guard did not preserve NOT_APPLIED: %+v",result)
			}
			// The first snapshot is single use, even when its candidate failed.
			if _, _, err:=guard.app.loadBestServerSelectionCandidateWithPurpose(scan.SelectionToken,
				first.Profile.ID,scan.CurrentEndpoint,readBestServerCurrentFilter(guard.app.cfg.FilterPath));err==nil{
				t.Fatal("first selection token remained replayable after apply admission")
			}
		})
	}
}
