package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Unlike a status badge, recovery must distinguish no process from an unknown
// or ambiguous process inventory. This function never starts or kills a core.
func (a *app) manualRecoveryCoreState() string {
	if a == nil {
		return "unknown"
	}
	root := strings.TrimSpace(os.Getenv("FREENET_XRAY_PROC_ROOT"))
	if root == "" {
		root = "/proc"
	}
	return manualRecoveryCoreStateInProc(root, filepath.Dir(a.cfg.OutPath))
}

func manualRecoveryCoreStateInProc(root, configDir string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "unknown"
	}
	count, recognized := 0, 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if pid, err := strconv.Atoi(entry.Name()); err != nil || pid <= 0 {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		comm, err := os.ReadFile(filepath.Join(dir, "comm"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "unknown"
		}
		if strings.TrimSpace(string(comm)) != "xray" {
			continue
		}
		env, err := os.ReadFile(filepath.Join(dir, "environ"))
		if err != nil {
			return "unknown"
		}
		if envContains(env, isolatedXrayProbeFlag) {
			continue
		}
		count++
		exact := configDir != "" && configDir != "." && envContains(env, "XRAY_LOCATION_CONFDIR="+filepath.Clean(configDir))
		status, err := os.ReadFile(filepath.Join(dir, "status"))
		if err != nil {
			return "unknown"
		}
		for _, line := range strings.Split(string(status), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == "Gid:" && fields[1] == "11111" {
				exact = true
			}
		}
		if exact {
			recognized++
		}
	}
	if count == 0 {
		return "stopped"
	}
	if count == 1 && recognized == 1 {
		return "running"
	}
	return "unknown"
}

// A complete interrupted attempt can be superseded only by another explicitly
// selected, validated candidate after its actual current bytes are snapshotted.
// Its snapshots are archived, never erased or silently treated as rollback OK.
func (a *app) manualRecoveryAttemptReadable(checkpoint string) bool {
	if a == nil {
		return false
	}
	attempt := filepath.Join(checkpoint, "recovery.pending")
	info, err := os.Lstat(attempt)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	marker, status := vpnTransactionRegularFile(filepath.Join(attempt, "state"), 128)
	if status != "ok" || strings.TrimSpace(string(marker)) != "PENDING" {
		return false
	}
	core, status := vpnTransactionRegularFile(filepath.Join(attempt, "core.before"), 128)
	if status != "missing" && (status != "ok" || (strings.TrimSpace(string(core)) != "running" && strings.TrimSpace(string(core)) != "stopped")) {
		return false
	}
	for kind, live := range map[string]string{"out": a.cfg.OutPath, "profile": vpnTransactionProfilePath(), "filter": a.cfg.FilterPath} {
		state := vpnTransactionFileMatch(attempt, kind, live)
		if state != "matched" && state != "different" {
			return false
		}
	}
	return true
}
