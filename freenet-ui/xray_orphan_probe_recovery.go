package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var errOrphanedXrayProbeUnconfirmed = errors.New("an isolated Xray probe has no verifiable FreeNet owner; existing VPN preserved")

// Only an orphaned, fully authenticated FreeNet diagnostic child may be
// retired. The production Xray and every unknown/owned process are untouched.
// The caller MUST hold the exclusive local + cross-process probe fence.
func reapVerifiedOrphanedXrayProbes(ctx context.Context) error {
	binary := strings.TrimSpace(os.Getenv("FREENET_XRAY_BIN"))
	if binary == "" {
		binary = defaultBestServerXrayPath
	}
	return reapVerifiedOrphanedXrayProbesWith(ctx, "/proc", binary, func(pid int) error {
		return syscall.Kill(pid, syscall.SIGTERM)
	})
}

func xrayProbePrivateConfigDir(dir string) bool {
	clean := filepath.Clean(dir)
	tmp := filepath.Clean(os.TempDir())
	relative, err := filepath.Rel(tmp, clean)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	first := strings.Split(relative, string(filepath.Separator))[0]
	if !strings.HasPrefix(first, "freenet-") || len(first) <= len("freenet-") {
		return false
	}
	root := filepath.Join(tmp, first)
	for _, path := range []string{root, clean} {
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 ||
			info.Mode()&(os.ModeSymlink|os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return false
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Geteuid()) {
			return false
		}
	}
	// The same root-owned, mode 0700 directory must contain FreeNet's exact
	// isolated probe configuration; an arbitrary argv -confdir is not proof.
	info, err := os.Lstat(filepath.Join(clean, "00_probe.json"))
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return false
	}
	return true
}

func orphanedXrayProbeIdentity(procRoot, binary string, pid int) (string, bool, error) {
	dir := filepath.Join(procRoot, strconv.Itoa(pid))
	comm, err := os.ReadFile(filepath.Join(dir, "comm"))
	if err != nil || !isXrayProcessComm(string(comm)) {
		return "", false, nil
	}
	env, err := os.ReadFile(filepath.Join(dir, "environ"))
	if err != nil || !envContains(env, isolatedXrayProbeFlag) {
		return "", false, nil
	}
	status, err := os.ReadFile(filepath.Join(dir, "status"))
	if err != nil {
		return "", false, errOrphanedXrayProbeUnconfirmed
	}
	ppid, uid, state := "", "", ""
	for _, line := range strings.Split(string(status), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		switch parts[0] {
		case "PPid:":
			ppid = parts[1]
		case "Uid:":
			uid = parts[1]
		case "State:":
			state = parts[1]
		}
	}
	if ppid != "1" {
		return "", false, nil // a live parent still owns the diagnostic child
	}
	if uid != strconv.Itoa(os.Geteuid()) || (state != "R" && state != "S") {
		return "", false, errOrphanedXrayProbeUnconfirmed
	}
	args, err := os.ReadFile(filepath.Join(dir, "cmdline"))
	if err != nil {
		return "", false, errOrphanedXrayProbeUnconfirmed
	}
	command := strings.Split(strings.TrimRight(string(args), "\x00"), "\x00")
	if len(command) != 4 || filepath.Base(command[0]) != isolatedXrayProbeName ||
		command[1] != "run" || command[2] != "-confdir" ||
		!xrayProbePrivateConfigDir(command[3]) {
		return "", false, errOrphanedXrayProbeUnconfirmed
	}
	expected, expectedErr := os.Stat(binary)
	executable, exeErr := os.Stat(filepath.Join(dir, "exe"))
	if expectedErr != nil || exeErr != nil || !os.SameFile(expected, executable) {
		return "", false, errOrphanedXrayProbeUnconfirmed
	}
	procState, start, err := xrayCoreNativeProcessStat(dir)
	if err != nil || (procState != "R" && procState != "S") || start == "" {
		return "", false, errOrphanedXrayProbeUnconfirmed
	}
	return start, true, nil
}

func reapVerifiedOrphanedXrayProbesWith(ctx context.Context, procRoot, binary string, signal func(int) error) error {
	if ctx == nil || ctx.Err() != nil || signal == nil {
		return errOrphanedXrayProbeUnconfirmed
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return errOrphanedXrayProbeUnconfirmed
	}
	for _, entry := range entries {
		if !entry.IsDir() || ctx.Err() != nil {
			if ctx.Err() != nil {
				return errOrphanedXrayProbeUnconfirmed
			}
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 {
			continue
		}
		start, orphan, err := orphanedXrayProbeIdentity(procRoot, binary, pid)
		if err != nil {
			return errOrphanedXrayProbeUnconfirmed
		}
		if !orphan {
			continue
		}
		// Re-verify PID/starttime immediately before the ONLY permitted
		// signal. Do not call kill(0), SIGKILL, group kill or xkeen restart.
		if current, yes, err := orphanedXrayProbeIdentity(procRoot, binary, pid); err != nil || !yes || current != start {
			return errOrphanedXrayProbeUnconfirmed
		}
		if err := signal(pid); err != nil && !errors.Is(err, syscall.ESRCH) {
			return errOrphanedXrayProbeUnconfirmed
		}
		ticker := time.NewTicker(50 * time.Millisecond)
		deadline := time.NewTimer(2 * time.Second)
		for {
			dir := filepath.Join(procRoot, strconv.Itoa(pid))
			state, latest, err := xrayCoreNativeProcessStat(dir)
			if os.IsNotExist(err) {
				break // only confirmed disappearance permits normal preflight
			}
			if err != nil || latest != start || state == "Z" || state == "X" {
				ticker.Stop()
				deadline.Stop()
				return errOrphanedXrayProbeUnconfirmed
			}
			select {
			case <-ctx.Done():
				ticker.Stop()
				deadline.Stop()
				return errOrphanedXrayProbeUnconfirmed
			case <-deadline.C:
				ticker.Stop()
				return errOrphanedXrayProbeUnconfirmed
			case <-ticker.C:
			}
		}
		ticker.Stop()
		deadline.Stop()
	}
	return nil
}
