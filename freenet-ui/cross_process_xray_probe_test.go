package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const probeGateChildArgument = "freenet-probe-gate-fixture"

func init() {
	if len(os.Args) > 1 && os.Args[1] == probeGateChildArgument {
		os.Exit(runProbeGateChildFixture())
	}
}

// This is another OS process with its own package-level channel. It exercises
// the shipped acquisition API, not a second flock implementation in the test.
// It exits before TestMain can replace the explicitly inherited lock path.
func runProbeGateChildFixture() int {
	if len(os.Args) != 4 || os.Getenv("FREENET_XRAY_PROBE_LOCK_FILE") == "" {
		return 90
	}
	watchdog := time.AfterFunc(30*time.Second, func() { os.Exit(91) })
	defer watchdog.Stop()
	timeout, err := time.ParseDuration(os.Args[3])
	if err != nil || timeout <= 0 || timeout > 20*time.Second {
		return 92
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	fmt.Println("attempting")
	var release func()
	var ok bool
	switch os.Args[2] {
	case "shared":
		release, ok = acquireIsolatedXrayProbe(ctx)
	case "exclusive":
		release, ok = acquireExclusiveIsolatedXrayProbe(ctx)
	default:
		return 93
	}
	if !ok {
		fmt.Println("blocked")
		return 0
	}
	defer release()
	fmt.Println("acquired")
	commands := bufio.NewScanner(os.Stdin)
	commands.Buffer(make([]byte, 64), 64)
	for commands.Scan() {
		switch commands.Text() {
		case "cancel":
			cancel()
			fmt.Println("canceled")
		case "release":
			release()
			release() // A duplicate cleanup must not release another permit.
			fmt.Println("released")
			return 0
		default:
			return 94
		}
	}
	return 95
}

type probeGateChild struct {
	cmd      *exec.Cmd
	input    io.WriteCloser
	events   <-chan string
	done     <-chan struct{}
	readDone <-chan struct{}
	stderr   bytes.Buffer
	waitErr  error
}

func startProbeGateChild(t *testing.T, mode string, timeout time.Duration) *probeGateChild {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	child := &probeGateChild{cmd: exec.Command(executable, probeGateChildArgument, mode, timeout.String())}
	child.cmd.Env = os.Environ()
	child.cmd.Stdout, child.cmd.Stderr = write, &child.stderr
	child.input, err = child.cmd.StdinPipe()
	if err != nil {
		_ = read.Close()
		_ = write.Close()
		t.Fatal(err)
	}
	if err := child.cmd.Start(); err != nil {
		_ = child.input.Close()
		_ = read.Close()
		_ = write.Close()
		t.Fatal(err)
	}
	_ = write.Close()
	events, done, readDone := make(chan string, 8), make(chan struct{}), make(chan struct{})
	child.events, child.done, child.readDone = events, done, readDone
	go func() {
		defer close(readDone)
		defer close(events)
		defer read.Close()
		scanner := bufio.NewScanner(read)
		for scanner.Scan() {
			events <- scanner.Text()
		}
	}()
	go func() {
		child.waitErr = child.cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = child.input.Close()
		select {
		case <-done:
		default:
			_ = child.cmd.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("probe gate fixture did not exit after cleanup")
		}
		_ = read.Close()
		select {
		case <-readDone:
		case <-time.After(time.Second):
			t.Error("probe gate fixture output reader did not exit")
		}
	})
	child.expect(t, "attempting")
	return child
}

func (child *probeGateChild) expect(t *testing.T, want string) {
	t.Helper()
	select {
	case got, ok := <-child.events:
		if !ok || got != want {
			t.Fatalf("probe gate child event=%q open=%v, want %q", got, ok, want)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("probe gate child did not report %q", want)
	}
}

func (child *probeGateChild) send(t *testing.T, command string) {
	t.Helper()
	if _, err := io.WriteString(child.input, command+"\n"); err != nil {
		t.Fatal(err)
	}
}

func (child *probeGateChild) wait(t *testing.T, killed bool) {
	t.Helper()
	select {
	case <-child.done:
		if !killed && child.waitErr != nil {
			t.Fatalf("probe gate child failed: %v; %s", child.waitErr, child.stderr.String())
		}
		if killed && child.waitErr == nil {
			t.Fatal("owner-death fixture exited normally instead of being killed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("probe gate child Wait did not complete")
	}
}

func (child *probeGateChild) release(t *testing.T) {
	t.Helper()
	child.send(t, "release")
	child.expect(t, "released")
	child.wait(t, false)
}

func probeGateTestPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "probes.lock")
	t.Setenv("FREENET_XRAY_PROBE_LOCK_FILE", path)
	return path
}

func acquireProbeGateTestMode(ctx context.Context, mode string) (func(), bool) {
	if mode == "exclusive" {
		return acquireExclusiveIsolatedXrayProbe(ctx)
	}
	return acquireIsolatedXrayProbe(ctx)
}

func probeGateTestFDCount(t *testing.T, path string) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if target == path {
			count++
		}
	}
	return count
}

func waitProbeGateTestCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline, tick := time.NewTimer(3*time.Second), time.NewTicker(5*time.Millisecond)
	defer deadline.Stop()
	defer tick.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("probe gate fixture did not reach its observable handshake")
		}
	}
}

func TestCrossProcessProbeGateSharedAndExclusiveLifetime(t *testing.T) {
	for _, modes := range [][2]string{{"shared", "exclusive"}, {"exclusive", "shared"}} {
		t.Run(modes[0]+"_blocks_"+modes[1], func(t *testing.T) {
			probeGateTestPath(t)
			child := startProbeGateChild(t, modes[0], 5*time.Second)
			child.expect(t, "acquired")
			// Callers cancel their acquisition context immediately after success.
			// A valid lease must still last until explicit release, across PIDs.
			child.send(t, "cancel")
			child.expect(t, "canceled")
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			release, ok := acquireProbeGateTestMode(ctx, modes[1])
			cancel()
			if ok {
				release()
				t.Fatal("a different process bypassed the held probe lease")
			}
			child.release(t)
			ctx, cancel = context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			release, ok = acquireProbeGateTestMode(ctx, modes[1])
			if !ok {
				t.Fatal("probe lease was not available after the other process exited")
			}
			release()
		})
	}
}

func TestCrossProcessProbeGateSharedReadersKeepLocalLimit(t *testing.T) {
	probeGateTestPath(t)
	child := startProbeGateChild(t, "shared", 5*time.Second)
	child.expect(t, "acquired")
	var releases []func()
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	// SH flock admits other processes; the existing two-slot cap is LOCAL.
	// This deliberately makes no claim of a two-worker global concurrency cap.
	for i := 0; i < isolatedXrayProbeLimit; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		release, ok := acquireIsolatedXrayProbe(ctx)
		cancel()
		if !ok {
			t.Fatal("shared reader in another process serialized local readers")
		}
		releases = append(releases, release)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if release, ok := acquireIsolatedXrayProbe(ctx); ok {
		release()
		t.Fatal("the existing local shared-worker limit was lost")
	}
	child.release(t)
}

func TestCrossProcessProbeGateCancellationReleasesSlotsAndDescriptors(t *testing.T) {
	for _, modes := range [][2]string{{"shared", "exclusive"}, {"exclusive", "shared"}} {
		t.Run(modes[1], func(t *testing.T) {
			path := probeGateTestPath(t)
			child := startProbeGateChild(t, modes[0], 5*time.Second)
			child.expect(t, "acquired")
			ctx, cancel := context.WithCancel(context.Background())
			result, completed := make(chan bool, 1), make(chan struct{})
			go func() {
				defer close(completed)
				release, ok := acquireProbeGateTestMode(ctx, modes[1])
				if ok {
					release()
				}
				result <- ok
			}()
			defer func() {
				cancel()
				select {
				case <-completed:
				case <-time.After(3 * time.Second):
					t.Error("canceled probe acquisition did not return")
				}
			}()
			wantSlots := 1
			if modes[1] == "exclusive" {
				wantSlots = isolatedXrayProbeLimit
			}
			waitProbeGateTestCondition(t, func() bool {
				return len(isolatedXrayProbeGate) == wantSlots && probeGateTestFDCount(t, path) == 1
			})
			cancel()
			select {
			case ok := <-result:
				if ok {
					t.Fatal("cancellation admitted a contended probe lease")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("canceled acquisition did not finish")
			}
			if len(isolatedXrayProbeGate) != 0 || probeGateTestFDCount(t, path) != 0 {
				t.Fatal("canceled acquisition leaked a local slot or lock descriptor")
			}
			child.release(t)
			ctx, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			release, ok := acquireExclusiveIsolatedXrayProbe(ctx)
			if !ok {
				t.Fatal("canceled acquisition prevented the next complete cutover lease")
			}
			release()
		})
	}
}

func TestCrossProcessProbeGateOwnerDeathReleasesStableInode(t *testing.T) {
	path := probeGateTestPath(t)
	child := startProbeGateChild(t, "exclusive", 5*time.Second)
	child.expect(t, "acquired")
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	child.wait(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, ok := acquireIsolatedXrayProbe(ctx)
	if !ok {
		t.Fatal("dead owner's kernel lock prevented a new reader")
	}
	release()
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("lock recovery removed or replaced the shared inode: %v", err)
	}
	// This child owns no Xray subprocess. The test does not claim that death
	// of an owner makes a surviving orphan safe; shell inventory still stops it.
}

func TestCrossProcessProbeGateUnsafeLockFailsClosed(t *testing.T) {
	for _, kind := range []string{"directory", "symlink", "hardlink", "parent_file", "mode"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path, target := filepath.Join(dir, "probe.lock"), filepath.Join(dir, "sentinel")
			original := []byte("unchanged\n")
			if err := os.WriteFile(target, original, 0600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch kind {
			case "directory":
				err = os.Mkdir(path, 0700)
			case "symlink":
				err = os.Symlink(target, path)
			case "hardlink":
				err = os.Link(target, path)
			case "parent_file":
				path = filepath.Join(target, "probe.lock")
			case "mode":
				err = os.WriteFile(path, nil, 0644)
				if err == nil {
					err = os.Chmod(path, 0644)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("FREENET_XRAY_PROBE_LOCK_FILE", path)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			for _, mode := range []string{"shared", "exclusive"} {
				if release, ok := acquireProbeGateTestMode(ctx, mode); ok {
					release()
					t.Fatalf("unsafe %s lock admitted %s", kind, mode)
				}
			}
			if ctx.Err() != nil {
				t.Fatal("unsafe lock rejection depended on context expiration")
			}
			if err := runReadOnlyXrayValidation(ctx, filepath.Join(dir, "not-xray"), dir, dir); !errors.Is(err, errIsolatedXrayProbeAdmission) {
				t.Fatalf("validation did not preserve a non-context admission failure: %v", err)
			}
			if _, err := runSetupFinalizeOwned(ctx, "plan"); !errors.Is(err, errIsolatedXrayProbeAdmission) {
				t.Fatalf("setup validation did not preserve an admission failure: %v", err)
			}
			if len(isolatedXrayProbeGate) != 0 || probeGateTestFDCount(t, path) != 0 {
				t.Fatal("unsafe lock failure leaked admission resources")
			}
			after, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(original, after) {
				t.Fatalf("unsafe lock admission changed the sentinel: %v", err)
			}
		})
	}
}

func TestCrossProcessProbeGateCanceledValidationKeepsContextError(t *testing.T) {
	path := probeGateTestPath(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, mode := range []string{"shared", "exclusive"} {
		if release, ok := acquireProbeGateTestMode(ctx, mode); ok {
			release()
			t.Fatal("already canceled context admitted work")
		}
		if release, ok := acquireProbeGateTestMode(nil, mode); ok {
			release()
			t.Fatal("nil context admitted work")
		}
	}
	if err := runReadOnlyXrayValidation(ctx, "not-xray", t.TempDir(), t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("validation lost cancellation: %v", err)
	}
	if _, err := runSetupFinalizeOwned(ctx, "plan"); !errors.Is(err, context.Canceled) {
		t.Fatalf("setup validation lost cancellation: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("already canceled admission created a lock: %v", err)
	}
}

func isolateProbeGateProviderState(t *testing.T, dir string) {
	t.Helper()
	// Successful manual acceptance records AUTO handoff/history and clears its
	// retry marker. Keep those side effects inside this fixture as well.
	for key, name := range map[string]string{
		"FREENET_AUTOMATION_HISTORY":              "automation.history",
		"FREENET_SETTINGS_V3_STATE":               "settings.state",
		"FREENET_SETTINGS_V3_HISTORY":             "settings.history",
		"FREENET_TECH_JOURNAL":                    "technical.jsonl",
		"FREENET_AUTO_EMERGENCY_COOLDOWN_STATE":    "emergency.state",
		"FREENET_PROFILE_FILE":                   "profile.name",
	} {
		t.Setenv(key, filepath.Join(dir, name))
	}
}

func TestProviderAdmissionRejectsExternalProbeWithoutMutation(t *testing.T) {
	probeGateTestPath(t)
	a, request, dir := providerFinalizationTestManualApp(t)
	isolateProbeGateProviderState(t, dir)
	profilePath := filepath.Join(dir, "profile.name")
	if err := os.WriteFile(profilePath, []byte("old profile\n"), 0600); err != nil {
		t.Fatal(err)
	}
	called := filepath.Join(dir, "helper.called")
	t.Setenv("FREENET_PROBE_GATE_HELPER_CALLED", called)
	t.Setenv("FREENET_PROVIDER_HELPER", writeFakeNetworkHelper(t, `printf called > "$FREENET_PROBE_GATE_HELPER_CALLED"
exit 88`))
	before := make(map[string][]byte)
	for _, path := range []string{a.cfg.OutPath, a.cfg.FilterPath, profilePath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = data
	}
	child := startProbeGateChild(t, "shared", 20*time.Second)
	child.expect(t, "acquired")
	// This intentionally exercises the real, fixed 12-second API drain bound.
	code, response := a.executeProviderProfileApply(request)
	if code != http.StatusConflict || response.Success || response.Applied || response.RollbackState != "NOT_APPLIED" ||
		!strings.Contains(response.Error, "isolated Xray diagnostics are still running") {
		t.Fatalf("external worker did not stop actual provider admission: %d %+v", code, response)
	}
	if _, err := os.Lstat(called); !os.IsNotExist(err) {
		t.Fatalf("helper was launched despite the external shared lease: %v", err)
	}
	for path, expected := range before {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(expected, data) {
			t.Fatalf("rejected admission changed a live file: %v", err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "transaction.pending")); !os.IsNotExist(err) {
		t.Fatalf("rejected admission created a checkpoint: %v", err)
	}
	select {
	case a.sem <- struct{}{}:
		<-a.sem
	default:
		t.Fatal("rejected admission retained its writer fence")
	}
	child.release(t)
}

func TestProviderFinalizationRetainsFenceAgainstExternalProbe(t *testing.T) {
	probeGateTestPath(t)
	a, request, dir := providerFinalizationTestManualApp(t)
	isolateProbeGateProviderState(t, dir)
	ready, finish := filepath.Join(dir, "ready"), filepath.Join(dir, "finish")
	t.Setenv("FREENET_FINALIZATION_TEST_READY", ready)
	t.Setenv("FREENET_FINALIZATION_TEST_RELEASE", finish)
	t.Setenv("FREENET_PROVIDER_HELPER", writeFakeNetworkHelper(t, providerFinalizationTestWriter+`[ "$1" = apply-core ] || exit 20
cat > "$FREENET_FINALIZATION_TEST_OUT" <<'OUT'
{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"203.0.113.10","port":443}]}}]}
OUT
record accepted-finalizing pending
printf ready > "$FREENET_FINALIZATION_TEST_READY"
COUNT=0
while [ ! -e "$FREENET_FINALIZATION_TEST_RELEASE" ]; do
    COUNT=$((COUNT + 1))
    [ "$COUNT" -lt 250 ] || exit 89
    sleep 0.02
done
record accepted-finalizing accepted
echo '[FreeNet Provider] RESULT=SUCCESS'
exit 0`))
	type result struct {
		code int
		response networkApplyResponse
	}
	resultCh, completed := make(chan result, 1), make(chan struct{})
	go func() {
		defer close(completed)
		code, response := a.executeProviderProfileApply(request)
		resultCh <- result{code, response}
	}()
	defer func() {
		if err := os.WriteFile(finish, []byte("finish\n"), 0600); err != nil {
			t.Error(err)
		}
		select {
		case <-completed:
		case <-time.After(25 * time.Second):
			t.Error("provider fixture did not finish before test-owned state cleanup")
		}
	}()
	waitProbeGateTestCondition(t, func() bool {
		_, err := os.Stat(ready)
		return err == nil
	})
	child := startProbeGateChild(t, "shared", 100*time.Millisecond)
	child.expect(t, "blocked")
	child.wait(t, false)
	select {
	case <-completed:
		t.Fatal("provider returned before its terminal helper receipt")
	default:
	}
	if err := os.WriteFile(finish, []byte("finish\n"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-resultCh:
		if got.code != http.StatusOK || !got.response.Success || !got.response.Applied {
			t.Fatalf("provider did not accept its confirmed terminal receipt: %d %+v", got.code, got.response)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not return after terminal helper completion")
	}
	resumed := startProbeGateChild(t, "shared", time.Second)
	resumed.expect(t, "acquired")
	resumed.release(t)
}
