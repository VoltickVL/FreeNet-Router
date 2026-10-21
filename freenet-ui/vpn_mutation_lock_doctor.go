package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// vpnMutationLockState is deliberately read-only. In particular, a lock with
// a missing owner PID is NOT permission to remove that lock: the writer could
// have died after changing live VPN files. The checkpoint is a separate gate.
// Never return the PID, its command line, or any contents of private files.
func vpnMutationLockState(lockDir, procRoot string) string {
	if strings.TrimSpace(lockDir) == "" || strings.TrimSpace(procRoot) == "" {
		return "unknown"
	}
	info, err := os.Lstat(lockDir)
	if errors.Is(err, os.ErrNotExist) {
		return "absent"
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "unknown"
	}

	// Application update fences have a separate verified recovery protocol.
	// Never classify them as orphaned VPN writers merely because their PID
	// is no longer visible.
	marker, status := vpnTransactionRegularFile(filepath.Join(lockDir, "freenet-update-owner"), 512)
	switch status {
	case "ok":
		if strings.TrimSpace(string(marker)) == "" {
			return "unknown"
		}
		return "update_owner_unverified"
	case "missing":
		// The independent VPN writer must have its own strict numeric PID.
	default:
		return "unknown"
	}

	pidBytes, status := vpnTransactionRegularFile(filepath.Join(lockDir, "pid"), 32)
	if status != "ok" {
		return "unknown"
	}
	pidText := strings.TrimSpace(string(pidBytes))
	if pidText == "" || strings.Trim(pidText, "0123456789") != "" {
		return "unknown"
	}
	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 1 {
		return "unknown"
	}

	proc, err := os.Lstat(filepath.Join(procRoot, strconv.Itoa(pid)))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "unknown"
	}
	if err == nil && (!proc.IsDir() || proc.Mode()&os.ModeSymlink != 0) {
		return "unknown"
	}

	// Recheck the same lock directory and bytes to avoid treating an obviously
	// replaced writer as an orphaned PID during this read-only snapshot. This
	// check is not an atomic unlock protocol and never authorizes mutation.
	after, afterErr := os.Lstat(lockDir)
	if afterErr != nil || !os.SameFile(info, after) {
		return "unknown"
	}
	pidAfter, afterStatus := vpnTransactionRegularFile(filepath.Join(lockDir, "pid"), 32)
	if afterStatus != "ok" || string(pidAfter) != string(pidBytes) {
		return "unknown"
	}
	if err == nil {
		// The PID may have been reused. Without a persistent starttime token
		// the process cannot be assumed to be the original lock owner.
		return "pid_present_unverified"
	}
	return "owner_pid_absent"
}

func vpnRecoveryCoreBefore(attempt string) string {
	value, status := vpnTransactionRegularFile(filepath.Join(attempt, "core.before"), 128)
	if status == "missing" {
		return "missing"
	}
	if status != "ok" {
		return "unknown"
	}
	switch strings.TrimSpace(string(value)) {
	case "running":
		return "running"
	case "stopped":
		return "stopped"
	default:
		return "unknown"
	}
}
