package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

type setupOwnedValidatorRecord struct {
	PID        int
	ParentPID  int
	Arg0       string
	Comm       string
	ConfigDir  string
	Probe      bool
	ControlEnv bool
}

// The real finalize shell requests this executable through the production
// protocol. No shell wrapper can hide a validator descendant from cmd.Wait.
func init() {
	if os.Getenv("FREENET_SETUP_OWNED_VALIDATOR_FIXTURE") == "1" {
		os.Exit(setupOwnedValidatorMain())
	}
}

func setupOwnedValidatorMain() int {
	go func() {
		time.Sleep(10 * time.Second)
		os.Exit(90)
	}()
	args := os.Args[1:]
	if len(args) != 4 || args[0] != "run" || args[1] != "-test" || args[2] != "-confdir" {
		return 91
	}
	config, err := os.ReadFile(os.Getenv("FREENET_CONFIG_FILE"))
	if err != nil {
		return 92
	}
	phase := "pre"
	if strings.Contains("\n"+string(config), "\nSETUP_COMPLETE=yes\n") {
		phase = "post"
	}
	comm, err := os.ReadFile("/proc/self/comm")
	if err != nil {
		return 93
	}
	record := setupOwnedValidatorRecord{
		PID: os.Getpid(), ParentPID: os.Getppid(), Arg0: os.Args[0],
		Comm: strings.TrimSpace(string(comm)), ConfigDir: args[3],
		Probe: os.Getenv("FREENET_XRAY_PROBE") == "1",
		ControlEnv: os.Getenv("FREENET_OWNED_XRAY_NONCE") != "" ||
			os.Getenv("FREENET_OWNED_XRAY_DIR") != "" || os.Getenv("FREENET_OWNED_XRAY_WORK_DIR") != "",
	}
	path := filepath.Join(os.Getenv("FREENET_SETUP_OWNED_RECORDS"), phase+".json")
	data, err := json.Marshal(record)
	if err != nil || os.WriteFile(path+".tmp", data, 0600) != nil || os.Rename(path+".tmp", path) != nil {
		return 94
	}
	if os.Getenv("FREENET_SETUP_OWNED_BLOCK") == phase {
		for {
			if _, err := os.Stat(path + ".release"); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	if os.Getenv("FREENET_SETUP_OWNED_FAIL") == phase {
		return 1
	}
	fmt.Println("Configuration OK.")
	return 0
}

type setupOwnedFixture struct {
	root      string
	tmp       string
	records   string
	config    string
	configDir string
	out       string
}

func newSetupOwnedFixture(t *testing.T, block, fail string) setupOwnedFixture {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("the router process-identity contract uses Linux procfs")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatal("the real finalize helper integration requires jq")
	}
	root := t.TempDir()
	opt := filepath.Join(root, "opt")
	f := setupOwnedFixture{
		root: root, tmp: filepath.Join(root, "tmp"), records: filepath.Join(root, "records"),
		config: filepath.Join(opt, "etc/freenet/freenet.conf"), configDir: filepath.Join(opt, "etc/xray/configs"),
	}
	f.out = filepath.Join(f.configDir, "04_outbounds.json")
	for _, dir := range []string{f.tmp, f.records, f.configDir, filepath.Join(opt, "etc/xray/dat"),
		filepath.Join(opt, "etc/freenet"), filepath.Join(opt, "etc/init.d"), filepath.Join(opt, "sbin"), filepath.Join(opt, "lib/freenet")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(f.config, "INSTALL_SCENARIO=existing_stack\nSETUP_COMPLETE=no\nDNS_MODE=firmware\nAUTO_VPN_V1=no\nAUTO_VPN_MODE=best\nAUTO_VPN_V1_INTERVAL=manual\n")
	write(f.out, `{"outbounds":[{"tag":"vless-reality","protocol":"freedom"}]}`+"\n")
	write(filepath.Join(opt, "etc/xray/sub.url"), "https://subscription.invalid/fixture\n")
	write(filepath.Join(opt, "etc/freenet/vpn_profile_name"), "Fixture\n")
	write(filepath.Join(opt, "etc/xray/blanc_profile_filter.regex"), "^Fixture$\n")
	write(filepath.Join(opt, "etc/init.d/S05xkeen"), "#!/bin/sh\nstart_auto=\"off\"\n")
	write(filepath.Join(root, "state"), "XRAY_RUNNING=yes\n")
	write(filepath.Join(root, "cron"), "# fixture cron\n")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	xray := filepath.Join(opt, "sbin/xray")
	if err := os.Symlink(executable, xray); err != nil {
		t.Fatal(err)
	}
	// These dependencies operate only inside the fixture. The production
	// finalize helper, owned request/response protocol and Go runner are real.
	writeExecutableTestFile(t, filepath.Join(opt, "sbin/xkeen"), `#!/bin/sh
[ "${FREENET_XRAY_PROBE:-}" != 1 ] || exit 71
[ "${1:-}" = -auto ] || exit 72
case "${2:-}" in on|off) ;; *) exit 73 ;; esac
sed -i "s/^start_auto=.*/start_auto=\"$2\"/" "$FREENET_ROOT/etc/init.d/S05xkeen"
`)
	writeExecutableTestFile(t, filepath.Join(opt, "sbin/freenet-ui"), `#!/bin/sh
[ "${FREENET_XRAY_PROBE:-}" != 1 ] || exit 71
[ "${1:-}" = settings-v3-reconcile ] && [ "${2:-}" = --config ] && [ "${3:-}" = "$FREENET_CONFIG_FILE" ] || exit 72
printf '# BEGIN FREENET\n# END FREENET\n' > "$FREENET_SETUP_OWNED_CRON"
`)
	writeExecutableTestFile(t, filepath.Join(root, "crontab"), `#!/bin/sh
if [ "${1:-}" = -l ]; then cat "$FREENET_SETUP_OWNED_CRON"; else cp "$1" "$FREENET_SETUP_OWNED_CRON"; fi
`)
	network := filepath.Join(opt, "lib/freenet/network.sh")
	writeExecutableTestFile(t, network, `#!/bin/sh
[ "${1:-}" = plan ] || exit 2
printf 'SUPPORTED=yes\nEFFECTIVE_DNS_MODE=firmware\nPROXY_DNS=off\nMUTATION=NONE\nREASON=fixture\n'
`)
	helper, err := os.ReadFile(filepath.Join("..", "scripts", "finalize_setup.sh"))
	if err != nil {
		t.Fatal(err)
	}
	helperPath := filepath.Join(root, "finalize-setup.sh")
	writeExecutableTestFile(t, helperPath, string(helper))
	for key, value := range map[string]string{
		"FREENET_ROOT": opt, "FREENET_CONFIG_FILE": f.config,
		"FREENET_SUB_FILE": filepath.Join(opt, "etc/xray/sub.url"),
		"FREENET_PROFILE_FILE": filepath.Join(opt, "etc/freenet/vpn_profile_name"),
		"FREENET_FILTER_FILE": filepath.Join(opt, "etc/xray/blanc_profile_filter.regex"),
		"FREENET_OUT_FILE": f.out, "FREENET_XRAY_CONFIG_DIR": f.configDir,
		"FREENET_XRAY_ASSET_DIR": filepath.Join(opt, "etc/xray/dat"), "FREENET_XRAY_BIN": xray,
		"FREENET_XKEEN_BIN": filepath.Join(opt, "sbin/xkeen"), "FREENET_UI_BIN": filepath.Join(opt, "sbin/freenet-ui"),
		"FREENET_NETWORK_HELPER": network, "FREENET_CRONTAB_BIN": filepath.Join(root, "crontab"),
		"FREENET_FINALIZE_HELPER": helperPath, "FREENET_FINALIZE_TEST_MODE": "yes",
		"FREENET_FINALIZE_TEST_STATE": filepath.Join(root, "state"), "FREENET_SETUP_OWNED_CRON": filepath.Join(root, "cron"),
		"FREENET_SETUP_OWNED_VALIDATOR_FIXTURE": "1", "FREENET_SETUP_OWNED_RECORDS": f.records,
		"FREENET_SETUP_OWNED_BLOCK": block, "FREENET_SETUP_OWNED_FAIL": fail,
		"FREENET_READONLY_WORKER_FIXTURE": "", "FREENET_XRAY_PROBE": "", "TMPDIR": f.tmp,
		"GORACE": os.Getenv("GORACE") + " atexit_sleep_ms=0",
	} {
		t.Setenv(key, value)
	}
	return f
}

func waitSetupOwnedRecord(t *testing.T, records, phase string) setupOwnedValidatorRecord {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(records, phase+".json"))
		var record setupOwnedValidatorRecord
		if err == nil && json.Unmarshal(data, &record) == nil {
			return record
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("real finalize helper did not reach the %s validator barrier", phase)
	return setupOwnedValidatorRecord{}
}

func assertSetupOwnedInventory(t *testing.T, f setupOwnedFixture, record setupOwnedValidatorRecord) {
	t.Helper()
	if record.ParentPID != os.Getpid() || record.ConfigDir != f.configDir || record.ControlEnv {
		t.Fatalf("validator ownership/path/control isolation differs from the Go contract: %+v", record)
	}
	assertReadonlyWorkerIsolation(t, readonlyWorkerRecord{PID: record.PID, Arg0: record.Arg0, Comm: record.Comm, Probe: record.Probe})
	// Use the actual live worker's proc files alongside one deterministic core
	// fixture, avoiding unrelated host processes and their access permissions.
	procRoot := filepath.Join(f.root, "proc-view")
	core := filepath.Join(procRoot, "999999999")
	probe := filepath.Join(procRoot, strconv.Itoa(record.PID))
	for _, dir := range []string{core, probe} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, value := range map[string]string{
		"comm": "xray\n", "environ": "XRAY_LOCATION_CONFDIR=" + f.configDir + "\x00", "status": "Gid:\t0\t0\t0\t0\n",
	} {
		if err := os.WriteFile(filepath.Join(core, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join("/proc", strconv.Itoa(record.PID), name), filepath.Join(probe, name)); err != nil {
			t.Fatal(err)
		}
	}
	if state := manualRecoveryCoreStateInProc(procRoot, f.configDir); state != "running" {
		t.Fatalf("live setup validator polluted the production core inventory: %s", state)
	}
}

func TestSetupFinalizeOwnedRealHelperLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, mode, block, fail string
		cancel                 bool
	}{
		{"plan_cancel", "plan", "pre", "", true},
		{"apply_pre_cancel", "apply", "pre", "", true},
		{"apply_post_cancel", "apply", "post", "", true},
		{"plan_success", "plan", "pre", "", false},
		{"apply_success", "apply", "post", "", false},
		{"plan_validation_failure", "plan", "pre", "pre", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newSetupOwnedFixture(t, tc.block, tc.fail)
			outBefore, err := os.ReadFile(f.out)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			done := make(chan struct{})
			var output []byte
			var callErr error
			go func() {
				defer close(done)
				output, callErr = runSetupFinalizeOwned(ctx, tc.mode)
			}()
			defer func() {
				cancel()
				select {
				case <-done:
				case <-time.After(4 * time.Second):
					t.Error("finalize helper or its owned validator did not drain")
				}
			}()
			record := waitSetupOwnedRecord(t, f.records, tc.block)
			assertSetupOwnedInventory(t, f, record)
			assertReadonlyWorkerLease(t)
			channelDir := filepath.Dir(record.Arg0)
			ownerDir := filepath.Dir(channelDir)
			request, err := os.ReadFile(filepath.Join(channelDir, "request"))
			fields := strings.Split(strings.TrimSuffix(string(request), "\n"), "\t")
			if err != nil || len(fields) != 6 || fields[0] != ownedXrayProtocol || fields[4] != "setup-test" || fields[5] != f.configDir {
				t.Fatal("real finalize helper did not use the exact owned setup-test request")
			}
			helperPID, err := strconv.Atoi(fields[2])
			if err != nil || helperPID <= 1 || helperPID == record.PID {
				t.Fatal("owned request did not identify the shell helper")
			}
			workers := []readonlyWorkerRecord{{PID: record.PID, Dir: ownerDir}, {PID: helperPID}}
			if tc.block == "post" {
				pre := waitSetupOwnedRecord(t, f.records, "pre")
				if pre.PID == record.PID {
					t.Fatal("post-apply validation reused a stale validator result")
				}
				workers = append(workers, readonlyWorkerRecord{PID: pre.PID})
			}
			cutover := readonlyWorkerCutoverAfterCleanup(t, workers)
			if tc.cancel {
				cancel()
			} else if err := os.WriteFile(filepath.Join(f.records, tc.block+".json.release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("finalize helper did not complete its real child Wait")
			}
			if err := <-cutover; err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.cancel:
				if !errors.Is(callErr, context.Canceled) || strings.Contains(string(output), "RESULT=SUCCESS") {
					t.Fatalf("canceled finalize reported acceptance: err=%v output=%q", callErr, output)
				}
			case tc.fail != "":
				if callErr == nil || errors.Is(callErr, errSetupFinalizeWorkerState) ||
					!strings.Contains(string(output), "XRAY_VALID=no") || !strings.Contains(string(output), "READY=no") {
					t.Fatalf("validator failure was hidden or mistaken for an ownership failure: err=%v output=%q", callErr, output)
				}
			default:
				want := "XRAY_VALID=yes"
				if tc.mode == "apply" {
					want = "[FreeNet Setup Finalize] RESULT=SUCCESS"
				}
				if callErr != nil || !strings.Contains(string(output), want) {
					t.Fatalf("naturally completed finalize was not accepted: err=%v output=%q", callErr, output)
				}
			}
			config, err := os.ReadFile(f.config)
			wantComplete := tc.mode == "apply" && tc.block == "post"
			if err != nil || strings.Contains(string(config), "SETUP_COMPLETE=yes") != wantComplete {
				t.Fatal("fixture setup state did not match the actual cancellation stage")
			}
			outAfter, err := os.ReadFile(f.out)
			if err != nil || string(outAfter) != string(outBefore) {
				t.Fatal("finalize validation changed the live outbound")
			}
			entries, err := os.ReadDir(f.tmp)
			if err != nil || len(entries) != 0 {
				t.Fatalf("owned finalize staging was retained: entries=%d err=%v", len(entries), err)
			}
		})
	}
}

func TestSetupFinalizeOwnedGateWaitUsesCallerDeadline(t *testing.T) {
	for _, mode := range []string{"plan", "apply"} {
		t.Run(mode, func(t *testing.T) {
			f := newSetupOwnedFixture(t, "pre", "")
			gateCtx, cancelGate := context.WithTimeout(context.Background(), time.Second)
			defer cancelGate()
			release, ok := acquireExclusiveIsolatedXrayProbe(gateCtx)
			if !ok {
				t.Fatal("cannot reserve the exclusive cutover gate")
			}
			defer release()
			ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
			defer cancel()
			start := time.Now()
			if _, err := runSetupFinalizeOwned(ctx, mode); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
				t.Fatalf("setup gate wait did not retain the caller's deadline: %v", err)
			}
			for _, dir := range []string{f.records, f.tmp} {
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("blocked setup helper spawned or staged work: entries=%d err=%v", len(entries), err)
				}
			}
		})
	}
}
