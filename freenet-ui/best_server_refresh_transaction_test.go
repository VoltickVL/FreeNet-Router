package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type refreshTransactionFixture struct {
	a             *app
	target        bestServerInternalCandidate
	dir           string
	before        []byte
	applied       []byte
	calls         string
	ready         string
	release       string
	helperPID     string
	selectionPath string
	postCalls     int
	postState     string
	postContextOK bool
	postGateOK    bool
	postWriterOK  bool
	stopCore      func()
}

const refreshTransactionHelper = `
printf '%s\n' "$1" >> "$FREENET_REFRESH_CALLS"
[ "$1" = apply-core ] || exit 81
[ "$2" = "$FREENET_REFRESH_PROFILE_ID" ] || exit 82
cmp -s "$FREENET_PROVIDER_SUBSCRIPTION_CACHE" "$FREENET_REFRESH_EXPECTED_CACHE" || exit 83
printf '%s\n' "$$" > "$FREENET_REFRESH_HELPER_PID"
printf '%s\n' "$FREENET_PROVIDER_SUBSCRIPTION_CACHE" > "$FREENET_REFRESH_SELECTION_PATH"
case "$FREENET_REFRESH_MODE" in
    no-apply)
        echo 'PRIMARY ERROR: fixture refused before mutation' >&2
        echo 'ROLLBACK ERROR/STATE: no live apply' >&2
        exit 1
        ;;
esac
cp "$FREENET_REFRESH_NEW_OUT" "$FREENET_REFRESH_OUT"
case "$FREENET_REFRESH_MODE" in
    hard-timeout)
        echo 'PRIMARY ERROR: fixture work did not complete' >&2
        echo 'ROLLBACK ERROR/STATE: rollback success' >&2
        printf ready > "$FREENET_REFRESH_READY"
        exec sleep 3
        ;;
    invalid-receipt)
        record accepted-finalizing pending
        echo 'ROLLBACK ERROR/STATE: rollback success' >&2
        exit 0
        ;;
    rollback)
        cp "$FREENET_REFRESH_OLD_OUT" "$FREENET_REFRESH_OUT"
        record rollback-finalizing rolled-back
        echo 'PRIMARY ERROR: fixture live probe failed' >&2
        echo 'ROLLBACK ERROR/STATE: rollback success' >&2
        exit 1
        ;;
    wrong-endpoint)
        cp "$FREENET_REFRESH_OLD_OUT" "$FREENET_REFRESH_OUT"
        ;;
    wrong-label)
        printf '^different$\n' > "$FREENET_REFRESH_FILTER"
        ;;
esac
record accepted-finalizing pending
printf ready > "$FREENET_REFRESH_READY"
if [ "$FREENET_REFRESH_HOLD" = yes ]; then
    while [ ! -e "$FREENET_REFRESH_RELEASE" ]; do sleep 0.01; done
fi
record accepted-finalizing accepted
echo '[FreeNet Provider] RESULT=SUCCESS'
exit 0
`

func newRefreshTransactionFixture(t *testing.T, mode string) *refreshTransactionFixture {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the live-core acceptance contract uses Linux procfs")
	}
	a, _, dir := providerFinalizationTestManualApp(t)
	// The catalog-only fixture lacks the SNI required by the actual outbound builder.
	subscription := strings.Replace(testSubscriptionPlain, "?security=reality&", "?security=reality&sni=example.test&", 1)
	raw := strings.Split(strings.TrimSpace(subscription), "\n")[0]
	profile, ok := parseSafeVLESSProfile(raw)
	if !ok {
		t.Fatal("invalid refresh profile fixture")
	}
	f := &refreshTransactionFixture{
		a: a, target: bestServerInternalCandidate{Profile: profile, Raw: raw}, dir: dir,
		calls: filepath.Join(dir, "helper.calls"), ready: filepath.Join(dir, "helper.ready"),
		release: filepath.Join(dir, "helper.release"), helperPID: filepath.Join(dir, "helper.pid"),
		selectionPath: filepath.Join(dir, "selection.path"), postState: automationHealthHealthy,
	}
	a.cfg.ProviderTimeout = 3 * time.Second
	before, err := os.ReadFile(a.cfg.OutPath)
	if err != nil {
		t.Fatal(err)
	}
	f.before = before
	outbound, err := buildBestServerProbeOutbound(raw, profile)
	if err != nil {
		t.Fatal(err)
	}
	f.applied, err = json.Marshal(map[string]any{"outbounds": []any{outbound}})
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		a.cfg.FilterPath: []byte(regexp.QuoteMeta(profile.Name) + "\n"),
		filepath.Join(dir, "profile"): []byte(profile.Name + "\n"),
		filepath.Join(dir, "old.json"): f.before,
		filepath.Join(dir, "new.json"): f.applied,
		filepath.Join(dir, "selected.lkg"): []byte(raw + "\n"),
	}
	for path, data := range files {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for key, value := range map[string]string{
		"FREENET_PROFILE_FILE": filepath.Join(dir, "profile"),
		"FREENET_REFRESH_CALLS": f.calls, "FREENET_REFRESH_READY": f.ready,
		"FREENET_REFRESH_RELEASE": f.release, "FREENET_REFRESH_HELPER_PID": f.helperPID,
		"FREENET_REFRESH_SELECTION_PATH": f.selectionPath, "FREENET_REFRESH_PROFILE_ID": profile.ID,
		"FREENET_REFRESH_EXPECTED_CACHE": filepath.Join(dir, "selected.lkg"),
		"FREENET_REFRESH_OLD_OUT": filepath.Join(dir, "old.json"),
		"FREENET_REFRESH_NEW_OUT": filepath.Join(dir, "new.json"),
		"FREENET_REFRESH_OUT": a.cfg.OutPath, "FREENET_REFRESH_FILTER": a.cfg.FilterPath,
		"FREENET_REFRESH_MODE": mode, "FREENET_REFRESH_HOLD": "no",
	} {
		t.Setenv(key, value)
	}
	t.Setenv("FREENET_PROVIDER_HELPER", writeFakeNetworkHelper(t, providerFinalizationTestWriter+refreshTransactionHelper))
	oldFetch, oldTCP, oldApp, oldPost := directSubscriptionBodyFetch, bestServerEndpointTCPProbe, bestServerEndpointApplicationProbe, bestServerRefreshPostProbe
	t.Cleanup(func() {
		directSubscriptionBodyFetch, bestServerEndpointTCPProbe = oldFetch, oldTCP
		bestServerEndpointApplicationProbe, bestServerRefreshPostProbe = oldApp, oldPost
	})
	directSubscriptionBodyFetch = func(context.Context, *url.URL) ([]byte, error) {
		return []byte(subscription), nil
	}
	bestServerEndpointTCPProbe = func(context.Context, subscriptionProfile) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Samples: []int{10}, Median: 10}
	}
	bestServerEndpointApplicationProbe = func(*app, context.Context, bestServerInternalCandidate) bestServerProbeResult {
		return bestServerProbeResult{OK: true, Samples: []int{60, 60}, Median: 60}
	}
	bestServerRefreshPostProbe = func(got *app, ctx context.Context) automationHealthProbe {
		f.postCalls++
		deadline, bounded := ctx.Deadline()
		f.postContextOK = bounded && ctx.Err() == nil && time.Until(deadline) > 0 && time.Until(deadline) <= automationHealthProbeTimeout
		f.postWriterOK = got == a && len(a.sem) == 1
		probeCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		release, acquired := acquireIsolatedXrayProbe(probeCtx)
		cancel()
		f.postGateOK = acquired
		if acquired {
			release()
		}
		return automationHealthProbe{State: f.postState}
	}

	// A real ELF process provides the existing live-core predicate. It is owned
	// by this test and never receives router arguments or performs networking.
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "xray")
	if err := os.Symlink(sleepPath, alias); err != nil {
		t.Fatal(err)
	}
	core := exec.Command(alias, "30")
	core.Env = append(os.Environ(), "XRAY_LOCATION_CONFDIR="+filepath.Dir(a.cfg.OutPath), "FREENET_XRAY_PROBE=")
	if err := core.Start(); err != nil {
		t.Fatal(err)
	}
	var stopOnce sync.Once
	f.stopCore = func() { stopOnce.Do(func() { _ = core.Process.Kill(); _ = core.Wait() }) }
	t.Cleanup(f.stopCore)
	if !a.liveXrayRunning() {
		t.Fatal("fixture did not enter the actual live-core predicate")
	}
	return f
}

type refreshTransactionResult struct {
	status   int
	response bestServerRefreshResponse
}

func (f *refreshTransactionFixture) start(t *testing.T, ctx context.Context) <-chan refreshTransactionResult {
	t.Helper()
	result := make(chan refreshTransactionResult, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		status, response := f.a.executeBestServerCurrentRefresh(ctx)
		result <- refreshTransactionResult{status, response}
	}()
	t.Cleanup(func() {
		if err := os.WriteFile(f.release, nil, 0600); err != nil {
			t.Errorf("cannot release test-owned helper: %v", err)
		}
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("refresh transaction did not drain before fixture cleanup")
		}
	})
	return result
}

func receiveRefreshTransaction(t *testing.T, result <-chan refreshTransactionResult) refreshTransactionResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("bounded refresh did not return")
		return refreshTransactionResult{}
	}
}

func assertRefreshHelperCalls(t *testing.T, f *refreshTransactionFixture, want string) {
	t.Helper()
	data, err := os.ReadFile(f.calls)
	if os.IsNotExist(err) {
		data, err = nil, nil
	}
	if err != nil || string(data) != want {
		t.Fatalf("helper calls=%q err=%v want=%q", data, err, want)
	}
}

func TestCurrentRefreshManagedTransactionSurvivesAdmissionCancellation(t *testing.T) {
	f := newRefreshTransactionFixture(t, "success")
	f.a.cfg.ProviderTimeout = 400 * time.Millisecond
	t.Setenv("FREENET_REFRESH_HOLD", "yes")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := f.start(t, ctx)
	waitProviderFinalizationTestFile(t, f.ready)
	cancel() // A lost HTTP caller/admission budget cannot abort this transaction.
	<-ctx.Done()
	// The real helper has announced finalization. Cross the independent work
	// deadline and require both fences to remain owned until its terminal exit.
	deadline := time.NewTimer(f.a.cfg.ProviderTimeout + 50*time.Millisecond)
	defer deadline.Stop()
	select {
	case got := <-done:
		t.Fatalf("admission/work deadline killed admitted finalization: %+v", got)
	case <-deadline.C:
	}
	if len(f.a.sem) != 1 {
		t.Fatal("writer fence escaped while the helper was still finalizing")
	}
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 30*time.Millisecond)
	release, acquired := acquireIsolatedXrayProbe(probeCtx)
	cancelProbe()
	if acquired {
		release()
		t.Fatal("exclusive probe fence escaped during finalization")
	}
	if err := os.WriteFile(f.release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	got := receiveRefreshTransaction(t, done)
	if got.status != http.StatusOK || !got.response.Success || !got.response.Applied || got.response.RollbackState != "NOT_NEEDED" {
		t.Fatalf("valid terminal result lost after admission expiry: %+v", got)
	}
	if f.postCalls != 1 || !f.postContextOK || !f.postGateOK || !f.postWriterOK {
		t.Fatalf("postprobe boundary calls=%d context=%v shared=%v writer=%v", f.postCalls, f.postContextOK, f.postGateOK, f.postWriterOK)
	}
	assertRefreshHelperCalls(t, f, "apply-core\n")
	pidData, err := os.ReadFile(f.helperPID)
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil || parseErr != nil {
		t.Fatal("missing actual helper PID")
	}
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); !os.IsNotExist(err) {
		t.Fatal("helper was not reaped before returning")
	}
	cache, err := os.ReadFile(f.selectionPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(strings.TrimSpace(string(cache)))); !os.IsNotExist(err) {
		t.Fatal("selected cache/owned channel staging was not removed")
	}
}

func TestCurrentRefreshAdmissionCannotStartOrChangeUnderProbeFence(t *testing.T) {
	for _, scenario := range []string{"already_canceled", "canceled_waiting", "identity_changed"} {
		t.Run(scenario, func(t *testing.T) {
			f := newRefreshTransactionFixture(t, "success")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "already_canceled" {
				cancel()
				got := receiveRefreshTransaction(t, f.start(t, ctx))
				if got.response.RollbackState != "NOT_APPLIED" || got.response.Mutation != "NONE" {
					t.Fatalf("expired admission result=%+v", got)
				}
			} else {
				release, ok := acquireIsolatedXrayProbe(context.Background())
				if !ok {
					t.Fatal("cannot reserve the diagnostic slot")
				}
				var once sync.Once
				releaseOnce := func() { once.Do(release) }
				defer releaseOnce()
				done := f.start(t, ctx)
				waitUntil := time.Now().Add(time.Second)
				for len(isolatedXrayProbeGate) != isolatedXrayProbeLimit {
					if time.Now().After(waitUntil) {
						t.Fatal("refresh did not reach the exclusive admission barrier")
					}
					time.Sleep(time.Millisecond)
				}
				assertRefreshHelperCalls(t, f, "")
				if scenario == "identity_changed" {
					if err := os.WriteFile(f.a.cfg.FilterPath, []byte("^changed$\n"), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					cancel()
				}
				releaseOnce()
				got := receiveRefreshTransaction(t, done)
				if got.response.RollbackState != "NOT_APPLIED" || got.response.Mutation != "NONE" || got.response.Applied {
					t.Fatalf("invalid admission crossed the helper boundary: %+v", got)
				}
			}
			assertRefreshHelperCalls(t, f, "")
			after, err := os.ReadFile(f.a.cfg.OutPath)
			if err != nil || string(after) != string(f.before) {
				t.Fatal("rejected admission changed live outbound")
			}
		})
	}
}

func TestCurrentRefreshFailureNeverStartsSecondRestore(t *testing.T) {
	for _, tc := range []struct {
		name, helper, post, rollback, mutation string
		stopCore, oldOut                      bool
	}{
		{"hard_timeout", "hard-timeout", automationHealthHealthy, "UNKNOWN", "UNKNOWN", false, false},
		{"invalid_receipt", "invalid-receipt", automationHealthHealthy, "UNKNOWN", "UNKNOWN", false, false},
		{"endpoint_mismatch", "wrong-endpoint", automationHealthHealthy, "UNKNOWN", "UNKNOWN", false, true},
		{"label_mismatch", "wrong-label", automationHealthHealthy, "UNKNOWN", "UNKNOWN", false, false},
		{"core_missing", "success", automationHealthHealthy, "UNKNOWN", "UNKNOWN", true, false},
		{"post_failed", "success", automationHealthFailed, "UNKNOWN", "UNKNOWN", false, false},
		{"post_uncertain", "success", automationHealthUncertain, "UNKNOWN", "UNKNOWN", false, false},
		{"verified_rollback", "rollback", automationHealthHealthy, "SUCCESS", "ROLLED_BACK", false, true},
		{"verified_no_apply", "no-apply", automationHealthHealthy, "NOT_APPLIED", "NONE", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRefreshTransactionFixture(t, tc.helper)
			f.postState = tc.post
			if tc.helper == "hard-timeout" {
				f.a.cfg.ProviderTimeout = 150 * time.Millisecond
			}
			if tc.stopCore {
				f.stopCore()
			}
			got := receiveRefreshTransaction(t, f.start(t, context.Background()))
			if got.status != http.StatusBadGateway || got.response.Success || got.response.Applied ||
				got.response.RollbackState != tc.rollback || got.response.Mutation != tc.mutation {
				t.Fatalf("refresh failure result=%+v", got)
			}
			if automationMutationBlockedState() != (tc.rollback == "UNKNOWN") {
				t.Fatal("UNKNOWN did not preserve the persistent mutation stop")
			}
			assertRefreshHelperCalls(t, f, "apply-core\n")
			want := f.applied
			if tc.oldOut {
				want = f.before
			}
			after, err := os.ReadFile(f.a.cfg.OutPath)
			if err != nil || string(after) != string(want) {
				t.Fatal("an unconfirmed result triggered an additional outer file restore")
			}
			if tc.helper == "hard-timeout" && got.response.PrimaryError != "fixture work did not complete" {
				t.Fatalf("primary cause was lost to stdout rollback text: %+v", got.response)
			}
			if tc.name == "post_failed" || tc.name == "post_uncertain" {
				if f.postCalls != 1 || !f.postGateOK || !f.postWriterOK {
					t.Fatal("post-acceptance check ran under the exclusive gate or without its writer fence")
				}
			}
			if tc.helper == "wrong-label" {
				data, err := os.ReadFile(f.a.cfg.FilterPath)
				if err != nil || string(data) != "^different$\n" {
					t.Fatal("label mismatch was silently restored outside the managed helper")
				}
			}
		})
	}
}

func TestCurrentRefreshResponseDeadlineAccountsForProviderOverride(t *testing.T) {
	for _, tc := range []struct {
		cfg  config
		want time.Duration
	}{
		{config{Timeout: 95 * time.Second, ProviderTimeout: 180 * time.Second}, 497 * time.Second},
		{config{Timeout: 95 * time.Second, ProviderTimeout: 240 * time.Second}, 557 * time.Second},
		{config{Timeout: 95 * time.Second}, 412 * time.Second},
	} {
		if got := bestServerRefreshResponseTimeout(tc.cfg); got != tc.want {
			t.Fatalf("refresh response deadline=%s want=%s", got, tc.want)
		}
	}
}
