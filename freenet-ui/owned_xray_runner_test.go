package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type ownedChildRecord struct {
	PID int
	PPID int
	Role string
	ConfigDir string
	Comm string
	Probe bool
	Control bool
	Port int
}

func init() {
	if os.Getenv("FREENET_OWNED_CHILD_FIXTURE") == "1" {
		os.Exit(ownedChildFixtureMain())
	}
}

func ownedChildWriteRecord(record ownedChildRecord) bool {
	data, err := json.Marshal(record)
	path := filepath.Join(os.Getenv("FREENET_OWNED_CHILD_RECORDS"), record.Role+".json")
	if record.Role == "curl" {
		path = filepath.Join(os.Getenv("FREENET_OWNED_CHILD_RECORDS"), fmt.Sprintf("curl-%d.json", record.PID))
	}
	return err == nil && os.WriteFile(path+".tmp", data, 0600) == nil && os.Rename(path+".tmp", path) == nil
}

func ownedChildFixtureMain() int {
	// Tests execute the actual test binary through the production alias.
	// No shell/sleep descendant stands in for the owned Xray process.
	go func() {
		time.Sleep(25 * time.Second)
		os.Exit(91)
	}()
	if (len(os.Args) > 1 && os.Args[1] == "owned-helper") || filepath.Base(os.Args[0]) == "provider-helper" {
		return ownedHelperFixtureMain()
	}
	args := os.Args[1:]
	value := func(flag string) string {
		for i := 0; i+1 < len(args); i++ {
			if args[i] == flag { return args[i+1] }
		}
		return ""
	}
	comm, err := os.ReadFile("/proc/self/comm")
	if err != nil { return 92 }
	record := ownedChildRecord{
		PID: os.Getpid(), PPID: os.Getppid(), Comm: strings.TrimSpace(string(comm)),
		Probe: os.Getenv("FREENET_XRAY_PROBE") == "1",
		Control: os.Getenv("FREENET_OWNED_XRAY_DIR") != "" || os.Getenv("FREENET_OWNED_XRAY_NONCE") != "" ||
			os.Getenv("FREENET_OWNED_XRAY_WORK_DIR") != "" || os.Getenv("FREENET_PROVIDER_FINALIZATION_STATUS") != "" ||
			os.Getenv("FREENET_PROVIDER_FINALIZATION_NONCE") != "",
	}
	switch filepath.Base(os.Args[0]) {
	case "core-control", "xkeen":
		record.Role = "core-launch"
		if !ownedChildWriteRecord(record) { return 93 }
		return 0
	case "pidof":
		fmt.Println(os.Getenv("FREENET_OWNED_CHILD_CORE_PID"))
		return 0
	case "netstat":
		var running ownedChildRecord
		data, err := os.ReadFile(filepath.Join(os.Getenv("FREENET_OWNED_CHILD_RECORDS"), "route-run.json"))
		if err == nil && json.Unmarshal(data, &running) == nil {
			address := net.JoinHostPort("127.0.0.1", strconv.Itoa(running.Port))
			conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				fmt.Printf("tcp 0 0 %s 0.0.0.0:* LISTEN\n", address)
			}
		}
		return 0
	case "curl":
		if value("--connect-timeout") != "4" || value("--max-time") != "8" || value("-w") != "%{http_code}" {
			return 94
		}
		if len(args) == 0 { return 94 }
		switch args[len(args)-1] {
		case "https://www.gstatic.com/generate_204", "https://cp.cloudflare.com/generate_204", "https://1.1.1.1/cdn-cgi/trace":
		default: return 94
		}
		address := value("--socks5-hostname")
		host, _, err := net.SplitHostPort(address)
		if err != nil || host != "127.0.0.1" { return 94 }
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err != nil { return 94 }
		_ = conn.Close()
		record.Role = "curl"
		if !ownedChildWriteRecord(record) { return 93 }
		fmt.Print("204")
		return 0
	}
	if len(args) == 1 && args[0] == "production" {
		record.Role = "production"
		if !ownedChildWriteRecord(record) { return 93 }
		for { time.Sleep(20 * time.Millisecond) }
	}
	if len(args) < 3 || args[0] != "run" { return 95 }
	record.ConfigDir = value("-confdir")
	isTest := false
	for _, arg := range args { isTest = isTest || arg == "-test" }
	var listener net.Listener
	if filepath.Base(record.ConfigDir) == "provider-route-probe" {
		var config struct {
			Inbounds []struct { Listen string; Port int }
			Outbounds []struct { StreamSettings struct { Sockopt struct { Mark int } } }
		}
		data, err := os.ReadFile(filepath.Join(record.ConfigDir, "00_probe.json"))
		if err != nil || json.Unmarshal(data, &config) != nil || len(config.Inbounds) != 1 ||
			config.Inbounds[0].Listen != "127.0.0.1" || len(config.Outbounds) != 1 ||
			config.Outbounds[0].StreamSettings.Sockopt.Mark != 255 { return 96 }
		record.Role = "route-test"
		if !isTest {
			record.Role = "route-run"
			listener, err = net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(config.Inbounds[0].Port)))
			if err != nil { return 96 }
			defer listener.Close()
			record.Port = listener.Addr().(*net.TCPAddr).Port
		}
	} else {
		if !isTest { return 95 }
		data, err := os.ReadFile(filepath.Join(record.ConfigDir, "04_outbounds.json"))
		if err != nil || !json.Valid(data) { return 96 }
		record.Role = "candidate"
		if record.ConfigDir == os.Getenv("FREENET_OWNED_CHILD_LIVE_DIR") { record.Role = "live" }
	}
	if !ownedChildWriteRecord(record) { return 93 }
	if os.Getenv("FREENET_OWNED_CHILD_BLOCK") == record.Role {
		release := filepath.Join(os.Getenv("FREENET_OWNED_CHILD_RECORDS"), record.Role+".release")
		for {
			if _, err := os.Stat(release); err == nil { break }
			time.Sleep(5 * time.Millisecond)
		}
	}
	if record.Role == "live" {
		delay, _ := strconv.Atoi(os.Getenv("FREENET_OWNED_CHILD_SLOW_LIVE"))
		if delay == 1 { delay = 300 }
		if delay > 0 && delay <= 5000 { time.Sleep(time.Duration(delay) * time.Millisecond) }
	}
	if isTest {
		fmt.Println("Configuration OK.")
		return 0
	}
	for {
		conn, err := listener.Accept()
		if err != nil { return 97 }
		_ = conn.Close()
	}
}

func ownedHelperFixtureMain() int {
	mode := os.Getenv("FREENET_OWNED_CHILD_HELPER_CASE")
	dir, nonce := os.Getenv("FREENET_OWNED_XRAY_DIR"), os.Getenv("FREENET_OWNED_XRAY_NONCE")
	work := os.Getenv("FREENET_OWNED_XRAY_WORK_DIR")
	if dir == "" || work == "" || nonce == "" { return 80 }
	stage, configDir := "live-test", os.Getenv("FREENET_OWNED_CHILD_LIVE_DIR")
	pid, sequence := strconv.Itoa(os.Getpid()), "1"
	writeStatus := func(outcome string) bool {
		path := os.Getenv("FREENET_PROVIDER_FINALIZATION_STATUS")
		data := fmt.Sprintf("%s\t%s\t%s\taccepted-finalizing\t%s\n",
			providerFinalizationProtocol, os.Getenv("FREENET_PROVIDER_FINALIZATION_NONCE"), pid, outcome)
		return os.WriteFile(path+".fixture", []byte(data), 0600) == nil && os.Rename(path+".fixture", path) == nil
	}
	if mode == "plan" {
		stage, configDir = "candidate-test", filepath.Join(work, "conf")
		if os.Mkdir(configDir, 0700) != nil { return 81 }
		if os.WriteFile(filepath.Join(configDir, "04_outbounds.json"), []byte(`{"outbounds":[]}`), 0600) != nil { return 81 }
	}
	if mode == "unclosed_probe" || mode == "blocked_probe" {
		stage, configDir = "route-start", filepath.Join(work, "provider-route-probe")
		if os.Mkdir(configDir, 0700) != nil { return 81 }
		config := `{"inbounds":[{"listen":"127.0.0.1","port":0}],"outbounds":[{"streamSettings":{"sockopt":{"mark":255}}}]}`
		if os.WriteFile(filepath.Join(configDir, "00_probe.json"), []byte(config), 0600) != nil { return 81 }
	}
	if mode == "fifo_log" {
		if syscall.Mkfifo(filepath.Join(work, "xray-test.log"), 0600) != nil { return 81 }
	}
	if mode == "hardlink_log" {
		if os.Link(os.Getenv("FREENET_OWNED_CHILD_PROTECTED_FILE"), filepath.Join(work, "xray-test.log")) != nil { return 81 }
	}
	switch mode {
	case "wrong_nonce": nonce = strings.Repeat("0", 32)
	case "wrong_pid": pid = "0"
	case "wrong_stage": stage = "start-production"
	case "wrong_path": configDir = filepath.Dir(configDir)
	}
	request := fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\n", ownedXrayProtocol, nonce, pid, sequence, stage, configDir)
	if mode == "request_after_seal" {
		if !writeStatus("pending") { return 82 }
		time.Sleep(900 * time.Millisecond)
	}
	path := filepath.Join(dir, "request")
	switch mode {
	case "fifo_request":
		if syscall.Mkfifo(path, 0600) != nil { return 81 }
	case "symlink_request":
		if os.WriteFile(path+".outside", []byte(request), 0600) != nil || os.Symlink(path+".outside", path) != nil { return 81 }
	default:
		if os.WriteFile(path+".fixture", []byte(request), 0600) != nil { return 81 }
		if mode == "public_request" && os.Chmod(path+".fixture", 0644) != nil { return 81 }
		if os.Rename(path+".fixture", path) != nil { return 81 }
	}
	if mode == "request_after_seal" {
		if !writeStatus("accepted") { return 82 }
		return 0
	}
	if mode == "phase_with_live" {
		if !writeStatus("pending") { return 82 }
		fmt.Println("[FreeNet Provider] RESULT=SUCCESS")
		for { time.Sleep(10 * time.Millisecond) }
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "response"))
		fields := strings.Split(strings.TrimSpace(string(data)), "\t")
		if err == nil && len(fields) == 7 && fields[3] == "1" {
			if mode == "replay" {
				replayed := strings.Replace(request, "\tlive-test\t", "\tcandidate-test\t", 1)
				if os.WriteFile(path+".fixture", []byte(replayed), 0600) != nil || os.Rename(path+".fixture", path) != nil { return 81 }
				for { time.Sleep(10 * time.Millisecond) }
			}
			if fields[5] != "OK" { return 83 }
			if mode == "unclosed_probe" {
				// Start acknowledges ownership before the child is scheduled.
				// Publish the test's evidence before deliberately abandoning it.
				for {
					var running ownedChildRecord
					data, err := os.ReadFile(filepath.Join(os.Getenv("FREENET_OWNED_CHILD_RECORDS"), "route-run.json"))
					if err == nil && json.Unmarshal(data, &running) == nil && strconv.Itoa(running.PID) == fields[6] && running.ConfigDir == configDir {
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
			if mode == "plan" { fmt.Println("MUTATION=NONE"); return 0 }
			if mode == "blocked_probe" { for { time.Sleep(10 * time.Millisecond) } }
			if mode == "budget" {
				out := `{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"203.0.113.10","port":443}]}}]}`
				if os.WriteFile(os.Getenv("FREENET_FINALIZATION_TEST_OUT"), []byte(out), 0600) != nil { return 84 }
			}
			if !writeStatus("accepted") { return 82 }
			fmt.Println("[FreeNet Provider] RESULT=SUCCESS")
			return 0
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func ownedFixtureBase(t *testing.T) (string, string, string) {
	t.Helper()
	if _, err := os.Stat("/proc/self/stat"); err != nil { t.Skip("owned child contract requires Linux procfs") }
	dir := t.TempDir()
	records, live := filepath.Join(dir, "records"), filepath.Join(dir, "configs")
	for _, path := range []string{records, live} {
		if err := os.Mkdir(path, 0700); err != nil { t.Fatal(err) }
	}
	if err := os.WriteFile(filepath.Join(live, "04_outbounds.json"), []byte(`{"outbounds":[]}`), 0600); err != nil { t.Fatal(err) }
	executable, err := os.Executable()
	if err != nil { t.Fatal(err) }
	t.Setenv("FREENET_OWNED_CHILD_FIXTURE", "1")
	t.Setenv("FREENET_OWNED_CHILD_RECORDS", records)
	t.Setenv("FREENET_OWNED_CHILD_LIVE_DIR", live)
	t.Setenv("FREENET_XRAY_BIN", executable)
	t.Setenv("FREENET_CONFIG_DIR", live)
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	return dir, records, executable
}

func readOwnedRecord(t *testing.T, records, role string) ownedChildRecord {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var record ownedChildRecord
		data, err := os.ReadFile(filepath.Join(records, role+".json"))
		if err == nil && json.Unmarshal(data, &record) == nil { return record }
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("real %s worker did not reach the expected stage", role)
	return ownedChildRecord{}
}

func assertOwnedChildExited(t *testing.T, record ownedChildRecord) {
	t.Helper()
	if record.PID <= 0 { t.Fatal("fixture child PID is missing") }
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(record.PID))); !os.IsNotExist(err) {
		t.Fatalf("owned child %d remains after return: %v", record.PID, err)
	}
}

func assertOwnedXrayRecord(t *testing.T, record ownedChildRecord) {
	t.Helper()
	if record.PPID != os.Getpid() || record.Comm != isolatedXrayProbeName || !record.Probe || record.Control {
		t.Fatalf("Xray was not a directly owned, isolated child without control credentials: %+v", record)
	}
}

func TestOwnedXrayRejectsInvalidRequestsAndUnsafeLogs(t *testing.T) {
	for _, name := range []string{"wrong_nonce", "wrong_pid", "wrong_stage", "wrong_path", "public_request", "fifo_request", "symlink_request", "fifo_log", "hardlink_log", "replay"} {
		t.Run(name, func(t *testing.T) {
			dir, records, executable := ownedFixtureBase(t)
			protected := filepath.Join(dir, "protected")
			const original = "must remain byte-identical\n"
			if err := os.WriteFile(protected, []byte(original), 0600); err != nil { t.Fatal(err) }
			t.Setenv("FREENET_OWNED_CHILD_PROTECTED_FILE", protected)
			t.Setenv("FREENET_OWNED_CHILD_HELPER_CASE", name)
			selection := providerFinalizationTestDir(t)
			cmd := exec.Command(executable, "owned-helper")
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := runProviderCommandWithFinalizationGrace(ctx, cmd, selection, time.Second)
			if !providerFinalizationUnknown(err) || !strings.Contains(err.Error(), "owned Xray") {
				t.Fatalf("unsafe request/log did not cause immediate owned-channel UNKNOWN: %v", err)
			}
			if ctx.Err() != nil || cmd.ProcessState == nil { t.Fatal("unsafe request blocked cancellation or returned without helper Wait") }
			data, err := os.ReadFile(protected)
			if err != nil || string(data) != original { t.Fatal("unsafe log changed the protected inode") }
			if name == "replay" {
				record := readOwnedRecord(t, records, "live")
				assertOwnedXrayRecord(t, record)
				assertOwnedChildExited(t, record)
			} else if entries, err := os.ReadDir(records); err != nil || len(entries) != 0 {
				t.Fatalf("unsafe request reached an Xray executable: %v %v", entries, err)
			}
		})
	}
}

func TestOwnedXrayUnfinishedWorkCannotBecomeTerminalReceipt(t *testing.T) {
	for _, name := range []string{"unclosed_probe", "blocked_probe", "phase_with_live", "request_after_seal"} {
		t.Run(name, func(t *testing.T) {
			_, records, executable := ownedFixtureBase(t)
			t.Setenv("FREENET_OWNED_CHILD_HELPER_CASE", name)
			if name == "phase_with_live" { t.Setenv("FREENET_OWNED_CHILD_BLOCK", "live") }
			cmd := exec.Command(executable, "owned-helper")
			ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
			defer cancel()
			_, err := runProviderCommandWithFinalizationGrace(ctx, cmd, providerFinalizationTestDir(t), 2*time.Second)
			if !providerFinalizationUnknown(err) || cmd.ProcessState == nil { t.Fatalf("unfinished work became a terminal result: %v", err) }
			if name == "request_after_seal" {
				if !strings.Contains(err.Error(), "unfinished or invalid") { t.Fatalf("fixture did not reach sealed-channel completion: %v", err) }
				entries, readErr := os.ReadDir(records)
				if readErr != nil || len(entries) != 0 { t.Fatal("sealed finalization launched a new Xray") }
			} else {
				role := "live"
				if name == "unclosed_probe" || name == "blocked_probe" { role = "route-run" }
				record := readOwnedRecord(t, records, role)
				assertOwnedXrayRecord(t, record)
				assertOwnedChildExited(t, record)
			}
		})
	}
}

type ownedProviderFixture struct {
	dir string
	records string
	selection string
	script string
	profileID string
	production ownedChildRecord
}

func setupOwnedProviderFixture(t *testing.T, block string) ownedProviderFixture {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil { t.Skip("actual provider shell contract requires jq") }
	dir, records, executable := ownedFixtureBase(t)
	bin, assets, selection := filepath.Join(dir, "bin"), filepath.Join(dir, "dat"), filepath.Join(dir, "selection")
	for _, path := range []string{bin, assets, selection} {
		if err := os.Mkdir(path, 0700); err != nil { t.Fatal(err) }
	}
	for _, name := range []string{"xray", "curl", "netstat", "pidof", "core-control", "xkeen"} {
		if err := os.Symlink(executable, filepath.Join(bin, name)); err != nil { t.Fatal(err) }
	}
	// Global sync is unrelated to the validator ownership contract.
	if err := os.WriteFile(filepath.Join(bin, "sync"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil { t.Fatal(err) }
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FREENET_OWNED_CHILD_BLOCK", block)
	t.Setenv("FREENET_OWNED_CHILD_SLOW_LIVE", "1")
	t.Setenv("FREENET_XRAY_BIN", filepath.Join(bin, "xray"))
	t.Setenv("FREENET_XKEEN_BIN", filepath.Join(bin, "xkeen"))
	t.Setenv("FREENET_XRAY_CORE_RESTART_HELPER", filepath.Join(bin, "core-control"))
	t.Setenv("FREENET_CURL_BIN", filepath.Join(bin, "curl"))
	t.Setenv("FREENET_PROVIDER_ROUTE_PROBE_BIN", "")
	t.Setenv("FREENET_ASSET_DIR", assets)
	t.Setenv("FREENET_PROFILE_FILE", filepath.Join(dir, "profile"))
	t.Setenv("FREENET_FILTER_FILE", filepath.Join(dir, "filter"))
	t.Setenv("FREENET_LOCK_DIR", filepath.Join(dir, "lock"))
	t.Setenv("FREENET_VPN_TRANSACTION_DIR", filepath.Join(dir, "pending"))
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "automation.state"))
	t.Setenv("FREENET_AUTOMATION_HISTORY", filepath.Join(dir, "history"))
	t.Setenv("FREENET_PROVIDER_RTT_MANUAL", "1")
	t.Setenv("FREENET_PROVIDER_RECOVERY_MANUAL", "0")
	const source = "https://fixture.invalid/subscription"
	subPath := filepath.Join(dir, "subscription")
	if err := os.WriteFile(subPath, []byte(source+"\n"), 0600); err != nil { t.Fatal(err) }
	t.Setenv("FREENET_SUB_FILE", subPath)
	raw := strings.Split(strings.TrimSpace(testSubscriptionPlain), "\n")[0]
	profile, ok := parseSafeVLESSProfile(raw)
	if !ok { t.Fatal("invalid synthetic provider fixture") }
	cache := filepath.Join(selection, "provider.lkg")
	if err := os.WriteFile(cache, []byte(raw+"\n"), 0600); err != nil { t.Fatal(err) }
	sum := sha256.Sum256([]byte(source))
	if err := os.WriteFile(cache+".source", []byte(fmt.Sprintf("%x\n", sum)), 0600); err != nil { t.Fatal(err) }
	t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_CACHE", cache)
	t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_SOURCE", cache+".source")
	for _, name := range []string{"profile", "filter"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("original\n"), 0600); err != nil { t.Fatal(err) }
	}
	for name, data := range map[string]string{
		"01_log.json": "{}", "03_inbounds.json": `{"inbounds":[]}`, "05_routing.json": `{"routing":{"rules":[]}}`,
		"04_outbounds.json": `{"outbounds":[{"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"}]}`,
	} {
		if err := os.WriteFile(filepath.Join(os.Getenv("FREENET_CONFIG_DIR"), name), []byte(data), 0600); err != nil { t.Fatal(err) }
	}
	productionCtx, cancelProduction := context.WithCancel(context.Background())
	production := exec.CommandContext(productionCtx, filepath.Join(bin, "xray"), "production")
	if err := production.Start(); err != nil { cancelProduction(); t.Fatal(err) }
	productionDone := make(chan struct{})
	go func() { _ = production.Wait(); close(productionDone) }()
	t.Cleanup(func() { cancelProduction(); <-productionDone })
	core := readOwnedRecord(t, records, "production")
	t.Setenv("FREENET_OWNED_CHILD_CORE_PID", strconv.Itoa(core.PID))
	script, err := filepath.Abs("../scripts/apply_provider_profile.sh")
	if err != nil { t.Fatal(err) }
	return ownedProviderFixture{dir, records, selection, script, profile.ID, core}
}

func TestOwnedProviderActualHelperWaitsForSlowLiveTestAndHTTPS(t *testing.T) {
	fixture := setupOwnedProviderFixture(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	release, ok := acquireExclusiveIsolatedXrayProbe(ctx)
	if !ok { t.Fatal("fixture could not acquire production cutover fence") }
	defer release()
	cmd := exec.Command("sh", fixture.script, "apply-core", fixture.profileID)
	cmd.Env = append(os.Environ(), "FREENET_XRAY_PROBE=1")
	started := time.Now()
	output, err := runProviderCommandWithFinalizationGrace(ctx, cmd, fixture.selection, time.Second)
	if err != nil || !strings.Contains(string(output), "[FreeNet Provider] RESULT=SUCCESS") {
		t.Fatalf("actual provider helper did not complete: %v\n%s", err, sanitizeOutput(string(output)))
	}
	if time.Since(started) < 300*time.Millisecond { t.Fatal("slow live validation was bypassed") }
	for _, role := range []string{"candidate", "live", "route-test", "route-run"} {
		record := readOwnedRecord(t, fixture.records, role)
		assertOwnedXrayRecord(t, record)
		assertOwnedChildExited(t, record)
	}
	live := readOwnedRecord(t, fixture.records, "live")
	if live.ConfigDir != os.Getenv("FREENET_CONFIG_DIR") { t.Fatal("second full validation did not use actual live config") }
	curlRecords, err := filepath.Glob(filepath.Join(fixture.records, "curl-*.json"))
	if err != nil || len(curlRecords) != 2 { t.Fatalf("mandatory live two-URL HTTPS path was bypassed: %v %v", curlRecords, err) }
	core := readOwnedRecord(t, fixture.records, "core-launch")
	if core.Control || core.Probe { t.Fatalf("production core launch inherited private/probe state: %+v", core) }
	if syscall.Kill(fixture.production.PID, 0) != nil { t.Fatal("owned runner killed unrelated production core") }
	for _, name := range []string{"pending", "lock"} {
		if _, err := os.Lstat(filepath.Join(fixture.dir, name)); !os.IsNotExist(err) { t.Fatalf("accepted receipt retained %s: %v", name, err) }
	}
}

func TestOwnedProviderCancellationDrainsLiveValidatorBeforeReturning(t *testing.T) {
	fixture := setupOwnedProviderFixture(t, "live")
	ctx, cancel := context.WithCancel(context.Background())
	cutoverCtx, cancelCutover := context.WithTimeout(context.Background(), time.Second)
	release, ok := acquireExclusiveIsolatedXrayProbe(cutoverCtx)
	cancelCutover()
	if !ok { cancel(); t.Fatal("fixture could not acquire cutover fence") }
	defer release()
	cmd := exec.Command("sh", fixture.script, "apply-core", fixture.profileID)
	cmd.Env = append(os.Environ(), "FREENET_XRAY_PROBE=1")
	done := make(chan error, 1)
	completed := make(chan struct{})
	go func() {
		defer close(completed)
		_, err := runProviderCommandWithFinalizationGrace(ctx, cmd, fixture.selection, 2*time.Second)
		done <- err
	}()
	defer func() { cancel(); <-completed }()
	live := readOwnedRecord(t, fixture.records, "live")
	assertOwnedXrayRecord(t, live)
	if _, err := os.Stat(filepath.Join(fixture.dir, "pending", "state")); err != nil { t.Fatal("fixture never committed its protected transaction") }
	if _, err := os.Stat(filepath.Join(fixture.records, "route-run.json")); !os.IsNotExist(err) { t.Fatal("blocked live validator was bypassed") }
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 30*time.Millisecond)
	probeRelease, acquired := acquireIsolatedXrayProbe(probeCtx)
	cancelProbe()
	if acquired { probeRelease(); t.Fatal("live validator did not retain the caller's exclusive fence") }
	cancel()
	select {
	case err := <-done:
		if !providerFinalizationUnknown(err) || !errors.Is(err, context.Canceled) { t.Fatalf("cancellation lost UNKNOWN/context cause: %v", err) }
	case <-time.After(3 * time.Second): t.Fatal("cancel did not drain actual validator/helper")
	}
	assertOwnedChildExited(t, live)
	if cmd.ProcessState == nil { t.Fatal("caller returned before helper Wait") }
	if syscall.Kill(fixture.production.PID, 0) != nil { t.Fatal("cancellation signaled the production core") }
	if _, err := os.Stat(filepath.Join(fixture.dir, "pending", "state")); err != nil { t.Fatal("hard cancellation discarded the recovery checkpoint") }
	if _, err := os.Stat(filepath.Join(fixture.selection, "finalization.status")); !os.IsNotExist(err) { t.Fatal("unvalidated live config received an early finalization phase") }
}


func TestOwnedProviderUsesWorkBudgetBeforeTerminalPhase(t *testing.T) {
	// Scale the observed 95s deadline / 108s elapsed work boundary by 1/100.
	// The synthetic delay is not a claim about one real Xray test's duration.
	a, request, _ := providerFinalizationTestManualApp(t)
	dir, records, executable := ownedFixtureBase(t)
	helper := filepath.Join(dir, "provider-helper")
	if err := os.Symlink(executable, helper); err != nil { t.Fatal(err) }
	t.Setenv("FREENET_PROVIDER_HELPER", helper)
	t.Setenv("FREENET_OWNED_CHILD_HELPER_CASE", "budget")
	t.Setenv("FREENET_OWNED_CHILD_SLOW_LIVE", "1080")
	a.cfg.Timeout = 950 * time.Millisecond
	a.cfg.ProviderTimeout = 1800 * time.Millisecond
	type result struct { code int; response networkApplyResponse }
	done := make(chan result, 1)
	completed := make(chan struct{})
	go func() {
		defer close(completed)
		code, response := a.executeProviderProfileApply(request)
		done <- result{code, response}
	}()
	// The configured finite work deadline also bounds cleanup after Fatal.
	defer func() { <-completed }()
	live := readOwnedRecord(t, records, "live")
	assertOwnedXrayRecord(t, live)
	select {
	case a.sem <- struct{}{}:
		<-a.sem
		t.Fatal("actual owned validator did not retain the API writer fence")
	default:
	}
	probeCtx, cancelProbe := context.WithTimeout(context.Background(), 30*time.Millisecond)
	release, ok := acquireIsolatedXrayProbe(probeCtx)
	cancelProbe()
	if ok { release(); t.Fatal("actual owned validator did not retain the API probe fence") }
	select {
	case result := <-done:
		if result.code != http.StatusOK || !result.response.Success || !result.response.Applied {
			t.Fatalf("old ordinary action deadline still cut off unfinished work: %d %+v", result.code, result.response)
		}
	case <-time.After(3*time.Second):
		t.Fatal("bounded provider work did not reach its terminal receipt")
	}
	assertOwnedChildExited(t, live)
}

func TestOwnedProviderLeafPlanRetainsOneSlotThroughDrainAndCleanup(t *testing.T) {
	dir, records, executable := ownedFixtureBase(t)
	helper := filepath.Join(dir, "provider-helper")
	tmp := filepath.Join(dir, "tmp")
	if err := os.Symlink(executable, helper); err != nil { t.Fatal(err) }
	if err := os.Mkdir(tmp, 0700); err != nil { t.Fatal(err) }
	t.Setenv("TMPDIR", tmp)
	t.Setenv("FREENET_PROVIDER_HELPER", helper)
	t.Setenv("FREENET_OWNED_CHILD_HELPER_CASE", "plan")
	t.Setenv("FREENET_OWNED_CHILD_BLOCK", "candidate")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	completed := make(chan struct{})
	go func() {
		defer close(completed)
		_, err := runProviderHelperOwned(ctx, "plan", "0123456789abcdef")
		done <- err
	}()
	defer func() { cancel(); <-completed }()
	candidate := readOwnedRecord(t, records, "candidate")
	assertOwnedXrayRecord(t, candidate)
	if len(isolatedXrayProbeGate) != 1 { t.Fatal("one plan did not own exactly one shared slot") }
	secondCtx, cancelSecond := context.WithTimeout(context.Background(), 100*time.Millisecond)
	second, ok := acquireIsolatedXrayProbe(secondCtx)
	cancelSecond()
	if !ok { t.Fatal("one plan recursively occupied both probe slots") }
	second()
	cutoverDone := make(chan error, 1)
	go func() {
		waitCtx, stopWaiting := context.WithTimeout(context.Background(), 3*time.Second)
		defer stopWaiting()
		release, ok := acquireExclusiveIsolatedXrayProbe(waitCtx)
		if !ok { cutoverDone <- errors.New("cutover did not resume after plan cancellation"); return }
		defer release()
		if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(candidate.PID))); !os.IsNotExist(err) {
			cutoverDone <- errors.New("plan released its slot before actual Xray Wait"); return
		}
		entries, err := os.ReadDir(tmp)
		if err != nil || len(entries) != 0 {
			cutoverDone <- errors.New("plan released its slot before temporary directory cleanup"); return
		}
		cutoverDone <- nil
	}()
	// Ensure the secondary observer cannot outlive fixture env/files on Fatal.
	defer func() {
		cancel()
		if err := <-cutoverDone; err != nil { t.Error(err) }
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil { t.Fatal("canceled read-only plan reported success") }
	case <-time.After(3*time.Second): t.Fatal("plan did not drain its live validation")
	}
	assertOwnedChildExited(t, candidate)
}

func TestProviderWorkBudgetAndHTTPEnvelope(t *testing.T) {
	for _, tc := range []struct { action, provider time.Duration; explicitAction, explicitProvider bool; want time.Duration }{
		{95*time.Second, defaultProviderTimeout, false, false, 180*time.Second},
		{95*time.Second, defaultProviderTimeout, true, false, 95*time.Second},
		{2*time.Second, 4*time.Second, true, true, 4*time.Second},
	} {
		if got := providerTimeoutOverride(tc.action, tc.provider, tc.explicitAction, tc.explicitProvider); got != tc.want {
			t.Fatalf("explicit timeout priority changed: got %s want %s", got, tc.want)
		}
	}
	if got := providerWorkTimeout(config{Timeout: 20*time.Millisecond}); got != 20*time.Millisecond { t.Fatal("zero provider fixture budget stopped preserving explicit short timeout") }
	if got := providerWorkTimeout(config{Timeout: 95*time.Second, ProviderTimeout: defaultProviderTimeout}); got != 180*time.Second { t.Fatal("provider retained the old 95-second work budget") }
	const preplan = 35 * time.Second
	const quiesce = 12 * time.Second
	const waitDelay = 2 * time.Second
	if required := preplan+quiesce+defaultProviderTimeout+providerFinalizationGrace+waitDelay; controlCenterWriteTimeout < required {
		t.Fatalf("provider envelope %s exceeds HTTP write timeout %s", required, controlCenterWriteTimeout)
	}
}

