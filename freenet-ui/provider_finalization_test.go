package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This fixture is a child of the real runner, not a replacement for its
// deadline, status reader, receipt validation or Wait path.
const providerFinalizationTestWriter = `set -eu
record() {
    record_tmp="$FREENET_PROVIDER_FINALIZATION_STATUS.test.$$"
    (umask 077; printf 'FREENET_PROVIDER_FINALIZATION_V1\t%s\t%s\t%s\t%s\n' \
        "${3:-$FREENET_PROVIDER_FINALIZATION_NONCE}" "${4:-$$}" "$1" "$2" > "$record_tmp")
    chmod 600 "$record_tmp"
    mv -f "$record_tmp" "$FREENET_PROVIDER_FINALIZATION_STATUS"
}
`

func providerFinalizationTestDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "selection")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func providerFinalizationTestCommand(body string) *exec.Cmd {
	cmd := exec.Command("sh", "-c", providerFinalizationTestWriter+body)
	cmd.WaitDelay = 30 * time.Millisecond
	return cmd
}

func TestProviderFinalizationCompletesAfterSoftDeadline(t *testing.T) {
	for _, tc := range []struct {
		name string
		phase string
		outcome string
		exitCode int
	}{
		{"accepted", "accepted-finalizing", "accepted", 0},
		{"rollback", "rollback-finalizing", "rolled-back", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := providerFinalizationTestDir(t)
			body := fmt.Sprintf("record %s pending\nsleep 0.8\nrecord %s %s\nexit %d\n", tc.phase, tc.phase, tc.outcome, tc.exitCode)
			cmd := providerFinalizationTestCommand(body)
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			_, err := runProviderCommandWithFinalizationGrace(ctx, cmd, dir, 2*time.Second)
			if ctx.Err() != context.DeadlineExceeded {
				t.Fatal("fixture did not cross the ordinary action deadline")
			}
			if providerFinalizationUnknown(err) {
				t.Fatalf("verified terminal receipt was lost after the soft deadline: %v", err)
			}
			if tc.exitCode == 0 && err != nil {
				t.Fatalf("accepted finalization failed: %v", err)
			}
			if tc.exitCode != 0 {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) || exitErr.ExitCode() != tc.exitCode {
					t.Fatalf("rollback exit was not preserved: %v", err)
				}
			}
			if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != tc.exitCode {
				t.Fatal("runner returned without waiting for the child")
			}
		})
	}
}

func TestProviderFinalizationHardStopsUnconfirmedResults(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		grace time.Duration
	}{
		{"missing_phase", "echo 'ROLLBACK ERROR/STATE: rollback success'\nsleep 3\n", time.Second},
		{"forged_stdout", "printf 'FREENET_PROVIDER_FINALIZATION_V1\\t%s\\t%s\\taccepted-finalizing\\tpending\\n' \"$FREENET_PROVIDER_FINALIZATION_NONCE\" \"$$\"\nsleep 3\n", time.Second},
		{"wrong_nonce", "record accepted-finalizing pending \"x$FREENET_PROVIDER_FINALIZATION_NONCE\"\nsleep 3\n", time.Second},
		{"wrong_pid", "record accepted-finalizing pending \"$FREENET_PROVIDER_FINALIZATION_NONCE\" 0\nsleep 3\n", time.Second},
		{"mutable_phase", "record apply-in-progress pending\nsleep 3\n", time.Second},
		{"phase_only", "record accepted-finalizing pending\nsleep 0.5\nexit 0\n", time.Second},
		{"missing_terminal", "record accepted-finalizing pending\nsleep 0.5\nrm -f \"$FREENET_PROVIDER_FINALIZATION_STATUS\"\nexit 0\n", time.Second},
		{"contradictory_phase", "record accepted-finalizing pending\nsleep 0.5\nrecord rollback-finalizing rolled-back\nexit 1\n", time.Second},
		{"contradictory_exit", "record rollback-finalizing pending\nsleep 0.5\nrecord rollback-finalizing rolled-back\nexit 0\n", time.Second},
		{"reserve_exhausted", "record rollback-finalizing pending\nsleep 3\n", 150 * time.Millisecond},
		{"reserve_not_renewed", "record rollback-finalizing pending\ni=0\nwhile [ \"$i\" -lt 30 ]; do sleep 0.1; record rollback-finalizing pending; i=$((i+1)); done\n", 150 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := providerFinalizationTestDir(t)
			cmd := providerFinalizationTestCommand(tc.body)
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()
			started := time.Now()
			_, err := runProviderCommandWithFinalizationGrace(ctx, cmd, dir, tc.grace)
			if !providerFinalizationUnknown(err) {
				t.Fatalf("unconfirmed finalization must return a typed UNKNOWN: %v", err)
			}
			if cmd.ProcessState == nil {
				t.Fatal("hard stop returned before child Wait")
			}
			if time.Since(started) > 2*time.Second {
				t.Fatal("unconfirmed child retained an unbounded execution window")
			}
			if strings.HasPrefix(tc.name, "reserve_") && !strings.Contains(err.Error(), "reserve expired") {
				t.Fatalf("fixture did not exercise the separate hard deadline: %v", err)
			}
		})
	}
}

func waitProviderFinalizationTestFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("helper did not reach the expected fixture phase")
}

func TestProviderFinalizationCancelNeverGrantsGrace(t *testing.T) {
	dir := providerFinalizationTestDir(t)
	ready := filepath.Join(dir, "ready")
	cmd := providerFinalizationTestCommand("record accepted-finalizing pending\nprintf ready > \"$FREENET_FINALIZATION_TEST_READY\"\nsleep 3\n")
	cmd.Env = append(os.Environ(), "FREENET_FINALIZATION_TEST_READY="+ready)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	completed := make(chan struct{})
	defer func() {
		cancel()
		<-completed
	}()
	go func() {
		defer close(completed)
		_, err := runProviderCommandWithFinalizationGrace(ctx, cmd, dir, time.Second)
		done <- err
	}()
	waitProviderFinalizationTestFile(t, ready)
	cancel()
	select {
	case err := <-done:
		if !providerFinalizationUnknown(err) || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation was converted into a grace/success result: %v", err)
		}
		if cmd.ProcessState == nil {
			t.Fatal("cancellation released the caller before Wait")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("cancellation incorrectly granted finalization grace")
	}
}

func TestProviderFinalizationStatusRejectsUnsafeOrContradictoryRecords(t *testing.T) {
	for _, name := range []string{"valid", "wrong_nonce", "wrong_pid", "extra_line", "nul", "oversize", "public_mode", "symlink", "directory", "replaced_parent"} {
		t.Run(name, func(t *testing.T) {
			dir := providerFinalizationTestDir(t)
			status, err := newProviderFinalizationStatus(dir)
			if err != nil {
				t.Fatal(err)
			}
			data := fmt.Sprintf("%s\t%s\t42\taccepted-finalizing\tpending\n", providerFinalizationProtocol, status.nonce)
			switch name {
			case "wrong_nonce":
				data = strings.Replace(data, status.nonce, "x"+status.nonce, 1)
			case "wrong_pid":
				data = strings.Replace(data, "\t42\t", "\t0042\t", 1)
			case "extra_line":
				data += "accepted\n"
			case "nul":
				data = strings.Replace(data, "pending", "pending\x00", 1)
			case "oversize":
				data += strings.Repeat("x", providerFinalizationMaxBytes)
			case "replaced_parent":
				if err := os.Rename(dir, dir+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(status.path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "public_mode":
				if err := os.Chmod(status.path, 0644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(status.path, status.path+".regular"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(status.path+".regular", status.path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(status.path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(status.path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			record, present, err := status.read(42)
			if !present {
				t.Fatal("fixture status unexpectedly absent")
			}
			if name == "valid" {
				if err != nil || record.phase != "accepted-finalizing" || record.outcome != "pending" {
					t.Fatalf("valid owned status rejected: %+v %v", record, err)
				}
			} else if err == nil {
				t.Fatal("unsafe status was accepted as proof")
			}
		})
	}
}

func providerFinalizationTestManualApp(t *testing.T) (*app, networkApplyRequest, string) {
	t.Helper()
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "provider.lkg")
	t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_CACHE", cachePath)
	t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_SOURCE", cachePath+".source")
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "automation.state"))
	t.Setenv("FREENET_VPN_TRANSACTION_DIR", filepath.Join(dir, "transaction.pending"))
	t.Setenv("FREENET_XRAY_PROC_ROOT", t.TempDir())
	const source = "https://provider.example.invalid/subscription-token"
	sourcePath := filepath.Join(dir, "subscription.url")
	if err := os.WriteFile(sourcePath, []byte(source+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := saveProviderSubscriptionCache(source, []byte(testSubscriptionPlain)); err != nil {
		t.Fatal(err)
	}
	raw := strings.Split(strings.TrimSpace(testSubscriptionPlain), "\n")[0]
	profile, ok := parseSafeVLESSProfile(raw)
	if !ok {
		t.Fatal("fixture profile is invalid")
	}
	a := testNetworkApp(t, "DNS_MODE=firmware\n")
	a.cfg.SubPath = sourcePath
	a.cfg.OutPath = filepath.Join(dir, "04_outbounds.json")
	a.cfg.FilterPath = filepath.Join(dir, "filter")
	if err := a.setXrayRuntimeDesiredState(xrayRuntimeDesiredStopped); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.cfg.OutPath, []byte(`{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"192.0.2.99","port":443}]}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.cfg.FilterPath, []byte("^old$\n"), 0600); err != nil {
		t.Fatal(err)
	}
	setAutomationMutationBlocked(false)
	t.Setenv("FREENET_FINALIZATION_TEST_OUT", a.cfg.OutPath)
	return a, networkApplyRequest{
		Operation: "provider", ProfileID: profile.ID, ExpectedEndpoint: profileEndpoint(profile), ManualOverride: true, Confirm: true,
	}, dir
}

func TestProviderFinalizationManagedApplyRetainsFencesAndAcceptsReceipt(t *testing.T) {
	a, request, dir := providerFinalizationTestManualApp(t)
	a.cfg.Timeout = 200 * time.Millisecond
	ready, release := filepath.Join(dir, "ready"), filepath.Join(dir, "release")
	t.Setenv("FREENET_FINALIZATION_TEST_READY", ready)
	t.Setenv("FREENET_FINALIZATION_TEST_RELEASE", release)
	helper := writeFakeNetworkHelper(t, providerFinalizationTestWriter+`[ "$1" = apply-core ] || exit 20
cat > "$FREENET_FINALIZATION_TEST_OUT" <<'OUT'
{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"203.0.113.10","port":443}]}}]}
OUT
record accepted-finalizing pending
printf ready > "$FREENET_FINALIZATION_TEST_READY"
while [ ! -e "$FREENET_FINALIZATION_TEST_RELEASE" ]; do sleep 0.02; done
record accepted-finalizing accepted
echo '[FreeNet Provider] RESULT=SUCCESS'
exit 0
`)
	t.Setenv("FREENET_PROVIDER_HELPER", helper)
	type result struct {
		status int
		response networkApplyResponse
	}
	done := make(chan result, 1)
	completed := make(chan struct{})
	defer func() {
		if err := os.WriteFile(release, []byte("release\n"), 0600); err != nil {
			t.Errorf("cannot release finalization fixture during cleanup: %v", err)
		}
		// This also runs after Fatal/missing-ready. Test-owned files and env
		// must outlive the actual API worker and its child Wait.
		<-completed
	}()
	go func() {
		defer close(completed)
		status, response := a.executeProviderProfileApply(request)
		done <- result{status, response}
	}()
	waitProviderFinalizationTestFile(t, ready)
	time.Sleep(a.cfg.Timeout + 50*time.Millisecond)
	select {
	case result := <-done:
		t.Fatalf("managed apply returned while finalization was still blocked: %+v", result)
	default:
	}
	select {
	case a.sem <- struct{}{}:
		<-a.sem
		t.Fatal("writer fence was released during finalization grace")
	default:
	}
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 30*time.Millisecond)
	releaseProbe, acquired := acquireIsolatedXrayProbe(probeCtx)
	cancelProbe()
	if acquired {
		releaseProbe()
		t.Fatal("isolated probe fence was released during finalization grace")
	}
	if err := os.WriteFile(release, []byte("release\n"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.status != http.StatusOK || !result.response.Success || !result.response.Applied {
			t.Fatalf("soft ctx expiry discarded a valid terminal receipt: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("verified finalization failed to return")
	}
}

func TestProviderFinalizationManagedHardStopOverridesPrintedRollback(t *testing.T) {
	a, request, _ := providerFinalizationTestManualApp(t)
	a.cfg.Timeout = 500 * time.Millisecond
	before, err := os.ReadFile(a.cfg.OutPath)
	if err != nil {
		t.Fatal(err)
	}
	helper := writeFakeNetworkHelper(t, `echo '[FreeNet Provider] ERROR: PRIMARY ERROR: fixture core acceptance failed' >&2
echo '[FreeNet Provider] ERROR: ROLLBACK ERROR/STATE: rollback success' >&2
sleep 3
exit 1
`)
	t.Setenv("FREENET_PROVIDER_HELPER", helper)
	code, result := a.executeProviderProfileApply(request)
	if code != http.StatusBadGateway || result.Success || result.RollbackState != "UNKNOWN" {
		t.Fatalf("stdout rollback marker overrode the hard timeout: %d %+v", code, result)
	}
	if result.PrimaryError != "fixture core acceptance failed" || !automationMutationBlockedState() {
		t.Fatalf("primary cause or mandatory mutation fence was lost: %+v", result)
	}
	after, err := os.ReadFile(a.cfg.OutPath)
	if err != nil || string(after) != string(before) {
		t.Fatal("read-only timeout fixture changed the live outbound")
	}
}
