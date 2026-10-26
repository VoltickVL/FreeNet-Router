package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// FreeNet owns Xray core updates and the restart/rollback of the existing
// production process. No XKeen UI, XKeen CLI, HTTP control service or external
// updater is needed. GID 11111 is the expected routing group on Keenetic.
const xrayCoreRoutingGID = 11111

// Test seams only; the actual implementation below uses /proc and exec.
var xrayCoreNativeRuntimeProof = xrayCoreNativeProcessMatches
var xrayCoreNativeManagedRestart = xrayCoreRestartDirect

type xrayCoreNativeProcess struct {
	pid       int
	startTime string
}

// The core update never replaces an unknown runtime. This read-only check is
// deliberately cheap: no second xray run -test/config rebuild.
func xrayCoreNativePreflight(ctx context.Context, binary string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !xrayCoreNativeRuntimeProof(binary) {
		return errors.New("FreeNet cannot identify the running Xray binary and routing group; no binary changed")
	}
	return nil
}

func xrayCoreNativeProcessMatches(binary string) bool {
	return xrayCoreNativeProcessMatchesInProc("/proc", binary)
}

func xrayCoreNativeProcessMatchesInProc(procRoot, binary string) bool {
	live, err := os.Stat(binary)
	if err != nil || !live.Mode().IsRegular() {
		return false
	}
	processes, err := os.ReadDir(procRoot)
	if err != nil {
		return false
	}
	matches := 0
	for _, process := range processes {
		if !process.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(process.Name()); err != nil {
			continue
		}
		dir := filepath.Join(procRoot, process.Name())
		comm, err := os.ReadFile(filepath.Join(dir, "comm"))
		if err != nil || strings.TrimSpace(string(comm)) != "xray" {
			continue
		}
		matches++
		if matches > 1 || !xrayCoreNativeCorrectGID(dir) {
			return false
		}
		executable, err := os.Stat(filepath.Join(dir, "exe"))
		if err != nil || !os.SameFile(live, executable) {
			return false
		}
	}
	return matches == 1
}

func xrayCoreNativeCorrectGID(procDir string) bool {
	status, err := os.ReadFile(filepath.Join(procDir, "status"))
	if err != nil {
		return false
	}
	for _, row := range strings.Split(string(status), "\n") {
		fields := strings.Fields(row)
		if len(fields) != 5 || fields[0] != "Gid:" {
			continue
		}
		for _, field := range fields[1:] {
			if field != strconv.Itoa(xrayCoreRoutingGID) {
				return false
			}
		}
		return true
	}
	return false
}

// Linux /proc/PID/stat's field 22 is the process start time. Pin it before
// signalling so a reused PID can never be treated as our Xray.
func xrayCoreNativeProcessStat(procDir string) (string, string, error) {
	b, err := os.ReadFile(filepath.Join(procDir, "stat"))
	if err != nil {
		return "", "", err
	}
	afterName := strings.LastIndexByte(string(b), ')')
	if afterName < 0 {
		return "", "", errors.New("Xray process stat is incomplete")
	}
	fields := strings.Fields(string(b)[afterName+1:])
	if len(fields) < 20 {
		return "", "", errors.New("Xray process stat has no start time")
	}
	return fields[0], fields[19], nil
}

// 0 production processes is allowed during rollback if the new Xray exited.
// Any ambiguous or non-FreeNet-compatible process fails closed; it is never
// killed, replaced or claimed as a successful restart.
func xrayCoreNativeInventory(procRoot, configDir string) (xrayCoreNativeProcess, error) {
	var found xrayCoreNativeProcess
	configDir = filepath.Clean(configDir)
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return found, errors.New("cannot read production Xray process inventory")
	}
	count := 0
	for _, item := range entries {
		if !item.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(item.Name())
		if err != nil || pid < 1 {
			continue
		}
		dir := filepath.Join(procRoot, item.Name())
		comm, err := os.ReadFile(filepath.Join(dir, "comm"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return found, errors.New("cannot inspect production process")
		}
		if strings.TrimSpace(string(comm)) != "xray" {
			continue
		}
		count++
		if count > 1 {
			return xrayCoreNativeProcess{}, errors.New("multiple production Xray processes; no restart")
		}
		if !xrayCoreNativeCorrectGID(dir) {
			return xrayCoreNativeProcess{}, errors.New("production Xray uses unexpected routing GID; no restart")
		}
		env, err := os.ReadFile(filepath.Join(dir, "environ"))
		if err != nil || !envContains(env, "XRAY_LOCATION_CONFDIR="+configDir) {
			return xrayCoreNativeProcess{}, errors.New("production Xray configuration ownership unconfirmed; no restart")
		}
		state, started, err := xrayCoreNativeProcessStat(dir)
		if err != nil {
			return xrayCoreNativeProcess{}, errors.New("production Xray identity unavailable; no restart")
		}
		if state == "Z" || state == "X" {
			continue
		}
		found = xrayCoreNativeProcess{pid: pid, startTime: started}
	}
	return found, nil
}

func xrayCoreNativeStillAlive(procRoot string, process xrayCoreNativeProcess) (bool, error) {
	if process.pid == 0 {
		return false, nil
	}
	dir := filepath.Join(procRoot, strconv.Itoa(process.pid))
	state, started, err := xrayCoreNativeProcessStat(dir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("unable to recheck Xray PID identity")
	}
	return started == process.startTime && state != "Z" && state != "X", nil
}

func xrayCoreNativeStop(ctx context.Context, process xrayCoreNativeProcess) error {
	if process.pid == 0 {
		return nil
	}
	alive, err := xrayCoreNativeStillAlive("/proc", process)
	if err != nil {
		return err
	}
	if !alive {
		return nil
	}
	// Mirror the fast native Xray lifecycle without depending on XKeen UI.
	if err := syscall.Kill(process.pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return errors.New("cannot stop the owned Xray process")
	}
	grace := time.NewTimer(1200 * time.Millisecond)
	defer grace.Stop()
	ticker := time.NewTicker(60 * time.Millisecond)
	defer ticker.Stop()
	for {
		alive, err = xrayCoreNativeStillAlive("/proc", process)
		if err != nil {
			return err
		}
		if !alive {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		case <-grace.C:
			alive, err = xrayCoreNativeStillAlive("/proc", process)
			if err != nil {
				return err
			}
			if alive {
				if err := syscall.Kill(process.pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
					return errors.New("cannot stop Xray after graceful timeout")
				}
			}
			wait := time.NewTimer(2 * time.Second)
			defer wait.Stop()
			for {
				alive, err = xrayCoreNativeStillAlive("/proc", process)
				if err != nil {
					return err
				}
				if !alive {
					return nil
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
				case <-wait.C:
					return errors.New("Xray shutdown not confirmed")
				}
			}
		}
	}
}

func xrayCoreNativeEnvironment(configDir, assetDir string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if key == "XRAY_LOCATION_CONFDIR" || key == "XRAY_LOCATION_ASSET" ||
			key == "FREENET_XRAY_PROBE" || strings.HasPrefix(key, "FREENET_OWNED_XRAY_") {
			continue
		}
		env = append(env, item)
	}
	return append(env, "XRAY_LOCATION_CONFDIR="+configDir, "XRAY_LOCATION_ASSET="+assetDir)
}

func xrayCoreNativeStart(binary, configDir, assetDir string) (<-chan error, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("FreeNet needs router root privileges to preserve Xray routing GID")
	}
	// Same Xray run mode and configuration directory as the existing stack.
	// This child outlives the HTTP request; a bounded health check follows.
	cmd := exec.Command(binary, "run")
	cmd.Env = xrayCoreNativeEnvironment(configDir, assetDir)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
		Credential: &syscall.Credential{Uid: 0, Gid: xrayCoreRoutingGID, NoSetGroups: true},
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, errors.New("cannot open native Xray output sink")
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	err = cmd.Start()
	_ = null.Close()
	if err != nil {
		return nil, errors.New("FreeNet could not launch the native Xray process")
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	return exited, nil
}

// Called only by the core update worker while it owns the FreeNet mutation
// semaphore. Both forward update and rollback use this same independent path.
func xrayCoreRestartDirect(ctx context.Context, binary, version, configDir, assetDir string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	process, err := xrayCoreNativeInventory("/proc", configDir)
	if err != nil {
		return err
	}
	if err := xrayCoreNativeStop(ctx, process); err != nil {
		return err
	}
	exited, err := xrayCoreNativeStart(binary, configDir, assetDir)
	if err != nil {
		return err
	}
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	consecutive := 0
	for {
		if ctx.Err() != nil {
			return errors.New("native FreeNet Xray restart readiness timeout")
		}
		select {
		case <-exited:
			return errors.New("new Xray process terminated during startup")
		default:
		}
		if xrayCoreNativeRuntimeProof(binary) {
			versionCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			runningVersion, versionErr := xrayCoreVersionAt(versionCtx, binary)
			cancel()
			if versionErr == nil && runningVersion == version {
				consecutive++
				if consecutive >= 3 {
					return nil
				}
			} else {
				consecutive = 0
			}
		} else {
			consecutive = 0
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("FreeNet native Xray startup not confirmed: %w", ctx.Err())
		case <-exited:
			return errors.New("new Xray process exited before readiness")
		case <-ticker.C:
		}
	}
}

func (a *app) restartXrayCoreUpdateOwned(ctx context.Context, binary, version string) error {
	return xrayCoreNativeManagedRestart(ctx, binary, version, a.routingConfigDir(), a.routingAssetDir())
}
