package main

import (
	"context"
	"net/url"
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
	// The selectable ID intentionally describes the logical label/transport,
	// NOT the endpoint/UUID. A second endpoint with the same label has the
	// same ID and must never masquerade as a distinct fallback.
	secondURL, err := url.Parse(first.Raw)
	if err != nil { t.Fatal(err) }
	secondURL.Host = "203.0.113.11:443"
	secondURL.User = url.User("TEST-ID-B")
	secondURL.Fragment = "DE Hamburg, Germany, Extra"
	secondRaw := secondURL.String()
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
	scan.Candidates[1].Endpoint = "203.0.113.99:443" // forged measurement mismatches exact raw
	if _, dupToken := guard.app.prepareAutomationQualityFallback(scan, scan.Candidates[0]); dupToken != "" {
		consumeBestServerSelectionSnapshot(dupToken)
		t.Fatal("forged endpoint measurement passed exact raw-profile snapshot validation")
	}
	if err := os.WriteFile(guard.app.cfg.SubPath, []byte("https://provider.example.invalid/rotated\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := guard.app.loadBestServerSelectionCandidateWithPurpose(token, second.Profile.ID,
		scan.CurrentEndpoint, filter); err == nil {
		t.Fatal("fallback bypassed subscription fingerprint drift")
	}
}

// Real Extra providers may give different selectable countries/credentials
// the same public ingress. Distinct LOGICAL profiles remain valid measured
// alternatives after the first candidate's serial media probe fails.
func TestAutomationQualityFallbackSameIngressDifferentMeasuredProfile(t *testing.T) {
	guard, scan, first, second := automationQualityTwoCandidateFixture(t)
	sharedURL, err := url.Parse(second.Raw)
	if err != nil { t.Fatal(err) }
	sharedURL.Host = profileEndpoint(first.Profile)
	sharedRaw := sharedURL.String()
	sharedProfile, ok := parseSafeVLESSProfile(sharedRaw)
	if !ok || sharedProfile.ID != second.Profile.ID ||
		!endpointsEqual(profileEndpoint(sharedProfile), profileEndpoint(first.Profile)) ||
		sharedRaw == first.Raw {
		t.Fatal("fixture must be two credential-distinct selectable profiles behind one ingress")
	}
	shared := bestServerInternalCandidate{Profile: sharedProfile, Raw: sharedRaw}
	measured := scan.Candidates[1]
	measured.Endpoint = profileEndpoint(sharedProfile)
	filter := readBestServerCurrentFilter(guard.app.cfg.FilterPath)
	endpoint := readBestServerCurrentEndpoint(guard.app.cfg.OutPath)
	newToken, err := guard.app.storeBestServerSelectionSnapshot(endpoint, filter,
		[]bestServerInternalCandidate{first, shared},
		[]bestServerQualityCandidate{scan.Candidates[0], measured})
	if err != nil || newToken == "" { t.Fatalf("shared ingress snapshot failed: %v", err) }
	defer consumeBestServerSelectionSnapshot(newToken)
	scan.Candidates[1], scan.SelectionToken = measured, newToken
	selected, fallbackToken := guard.app.prepareAutomationQualityFallback(scan, scan.Candidates[0])
	if fallbackToken == "" || selected.ID != second.Profile.ID ||
		!endpointsEqual(selected.Endpoint, first.Profile.Address+":443") {
		t.Fatalf("distinct logical profile incorrectly rejected for shared ingress: %+v / %q", selected, fallbackToken)
	}
	defer consumeBestServerSelectionSnapshot(fallbackToken)
	consumeBestServerSelectionSnapshot(newToken)
	got, manual, err := guard.app.loadBestServerSelectionCandidateWithPurpose(
		fallbackToken, sharedProfile.ID, endpoint, filter)
	if err != nil || manual || got.Raw != sharedRaw {
		t.Fatalf("independent one-time snapshot did not survive source token consumption: %v", err)
	}
	if _, _, err := guard.app.loadBestServerSelectionCandidateWithPurpose(
		fallbackToken, first.Profile.ID, endpoint, filter); err == nil {
		t.Fatal("shared ingress alternative token also authorized rejected first logical profile")
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
