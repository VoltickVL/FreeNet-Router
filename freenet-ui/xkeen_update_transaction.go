package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type xkeenUpdateDiskState struct {
	State       string `json:"state"`
	Previous    string `json:"previous,omitempty"`
	Target      string `json:"target,omitempty"`
	Rollback    string `json:"rollback"`
	Phase       string `json:"phase,omitempty"`
	Reason      string `json:"reason,omitempty"`
	SnapshotDir string `json:"snapshot_dir,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type xkeenUpdateStatus struct {
	State          string `json:"state"`
	Previous       string `json:"previous,omitempty"`
	Target         string `json:"target,omitempty"`
	Rollback       string `json:"rollback"`
	Reason         string `json:"reason,omitempty"`
	UpdatedAt      string `json:"updated_at,omitempty"`
	Mutation       string `json:"mutation"`
	// XKeen script/runtime registration is intentionally not altered by an
	// isolated update. The running Xray service is neither killed nor restarted.
	RuntimeRestarted bool `json:"runtime_restarted"`
}

var xkeenUpdateInProcess struct {
	sync.RWMutex
	active bool
}

func xkeenUpdateLocalActive() bool {
	xkeenUpdateInProcess.RLock()
	defer xkeenUpdateInProcess.RUnlock()
	return xkeenUpdateInProcess.active
}

func setXKeenUpdateLocalActive(active bool) {
	xkeenUpdateInProcess.Lock()
	xkeenUpdateInProcess.active = active
	xkeenUpdateInProcess.Unlock()
}

func xkeenUpdateStatePath(binary string) string {
	return filepath.Join(filepath.Dir(binary), ".freenet-xkeen-update-state.json")
}

func xkeenUpdateLockPath(binary string) string {
	return filepath.Join(filepath.Dir(binary), ".freenet-xkeen-update.lock")
}

func xkeenSnapshotDirValid(binary, dir string) bool {
	parent := filepath.Clean(filepath.Dir(binary))
	if !filepath.IsAbs(dir) || filepath.Dir(filepath.Clean(dir)) != parent ||
		!strings.HasPrefix(filepath.Base(dir), ".freenet-xkeen-snapshot-") {
		return false
	}
	info, err := os.Lstat(dir)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func readXKeenUpdateDiskState(binary string) (xkeenUpdateDiskState, error) {
	var record xkeenUpdateDiskState
	name := xkeenUpdateStatePath(binary)
	info, err := os.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return xkeenUpdateDiskState{State: "IDLE", Rollback: "NOT_NEEDED"}, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return record, errors.New("xkeen-update-state-unreadable")
	}
	data, err := os.ReadFile(name)
	if err != nil || json.Unmarshal(data, &record) != nil {
		return record, errors.New("xkeen-update-state-unreadable")
	}
	switch record.State {
	case "IDLE", "RUNNING", "SUCCESS", "FAILED", "ROLLED_BACK", "STOP":
	default:
		return record, errors.New("xkeen-update-state-invalid")
	}
	if record.SnapshotDir != "" && !xkeenSnapshotDirValid(binary, record.SnapshotDir) {
		// A missing snapshot during RUNNING/STOP is a STOP, not permission to
		// retry; the receipt is intentionally never deleted by the reader.
		if record.State == "RUNNING" || record.State == "STOP" {
			return record, errors.New("xkeen-snapshot-missing")
		}
	}
	return record, nil
}

func writeXKeenUpdateDiskState(binary string, record xkeenUpdateDiskState) error {
	name := xkeenUpdateStatePath(binary)
	if record.SnapshotDir != "" && !xkeenSnapshotDirValid(binary, record.SnapshotDir) {
		return errors.New("xkeen-snapshot-invalid")
	}
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.Marshal(record)
	if err != nil || len(data) > 4096 {
		return errors.New("xkeen-state-encode")
	}
	out, err := os.CreateTemp(filepath.Dir(binary), ".freenet-xkeen-state-*")
	if err != nil {
		return errors.New("xkeen-state-write-failed")
	}
	tmp := out.Name()
	defer os.Remove(tmp)
	_, writeErr := out.Write(append(data, '\n'))
	modeErr := out.Chmod(0600)
	syncErr := out.Sync()
	closeErr := out.Close()
	if writeErr != nil || modeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("xkeen-state-write-failed")
	}
	if err := os.Rename(tmp, name); err != nil {
		return errors.New("xkeen-state-write-failed")
	}
	return nil
}

func publicXKeenUpdateState(binary string) xkeenUpdateStatus {
	state, err := readXKeenUpdateDiskState(binary)
	if err != nil {
		return xkeenUpdateStatus{State: "STOP", Rollback: "UNKNOWN", Reason: "state-unverifiable", Mutation: "NONE"}
	}
	if state.State == "RUNNING" && !xkeenUpdateLocalActive() {
		return xkeenUpdateStatus{State: "STOP", Rollback: "UNKNOWN", Reason: "interrupted-update", Mutation: "NONE"}
	}
	return xkeenUpdateStatus{State: state.State, Previous: state.Previous,
		Target: state.Target, Rollback: state.Rollback, Reason: state.Reason,
		UpdatedAt: state.UpdatedAt, Mutation: "NONE"}
}

func reserveXKeenCrossProcessLock(binary string) (func(), error) {
	lock, err := os.OpenFile(xkeenUpdateLockPath(binary), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, errors.New("xkeen-lock-unavailable")
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, errors.New("xkeen-update-in-progress")
	}
	return func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}, nil
}

func xkeenTargetPaths(binary string) (string, string) {
	return binary, filepath.Join(filepath.Dir(binary), ".xkeen")
}

func xkeenLiveTreeReady(binary string) bool {
	exe, tree := xkeenTargetPaths(binary)
	if filepath.Base(exe) != "xkeen" || !filepath.IsAbs(exe) || filepath.Clean(exe) != exe {
		return false
	}
	first, e1 := os.Lstat(exe)
	second, e2 := os.Lstat(tree)
	return e1 == nil && e2 == nil && first.Mode().IsRegular() &&
		first.Mode()&0111 != 0 && second.IsDir() &&
		first.Mode()&os.ModeSymlink == 0 && second.Mode()&os.ModeSymlink == 0
}

// Hook for isolated fault-injection tests; no user-controlled steps.
var xkeenSwapTestHook = func(string) error { return nil }

// A completed Stage is verified before any live name is changed. We retain
// original XKeen files in a private same-filesystem snapshot through
// post-check, and never mutate /opt/etc/xray, /opt/etc/xkeen, netfilter,
// system cron/registrations or running Xray.
func swapXKeenVersionedFiles(binary, stage, snapshot string) error {
	if !xkeenLiveTreeReady(binary) || !xkeenSnapshotDirValid(binary, snapshot) ||
		filepath.Dir(filepath.Clean(stage)) != filepath.Dir(binary) {
		return errors.New("xkeen-live-tree-unverified")
	}
	exe, tree := xkeenTargetPaths(binary)
	steps := []struct{ from, to, code string }{
		{exe, filepath.Join(snapshot, "xkeen"), "backup-script"},
		{tree, filepath.Join(snapshot, ".xkeen"), "backup-modules"},
		{filepath.Join(stage, "_xkeen"), tree, "install-modules"},
		{filepath.Join(stage, "xkeen"), exe, "install-script"},
	}
	for _, step := range steps {
		if err := xkeenSwapTestHook(step.code); err != nil {
			return errors.New("xkeen-swap-aborted")
		}
		if err := os.Rename(step.from, step.to); err != nil {
			return errors.New("xkeen-atomic-rename-failed")
		}
	}
	return nil
}

func rollbackXKeenFiles(binary, snapshot, expected string) error {
	if !xkeenSnapshotDirValid(binary, snapshot) {
		return errors.New("xkeen-rollback-snapshot-unavailable")
	}
	exe, tree := xkeenTargetPaths(binary)
	// Snapshot contents are the only sources of truth; unexpected missing
	// backups never authorize deleting a live installation.
	restore := []struct{ backup, live, failed string }{
		{filepath.Join(snapshot, "xkeen"), exe, filepath.Join(snapshot, "new-xkeen")},
		{filepath.Join(snapshot, ".xkeen"), tree, filepath.Join(snapshot, "new-modules")},
	}
	for _, item := range restore {
		_, err := os.Lstat(item.backup)
		if errors.Is(err, os.ErrNotExist) {
			continue // the corresponding original was never moved
		}
		if err != nil {
			return errors.New("xkeen-rollback-backup-unreadable")
		}
		_, liveErr := os.Lstat(item.live)
		moved := false
		if liveErr == nil {
			if err := os.Rename(item.live, item.failed); err != nil {
				return errors.New("xkeen-rollback-live-quarantine-failed")
			}
			moved = true
		} else if !errors.Is(liveErr, os.ErrNotExist) {
			return errors.New("xkeen-rollback-live-unavailable")
		}
		if err := os.Rename(item.backup, item.live); err != nil {
			if moved {
				_ = os.Rename(item.failed, item.live)
			}
			return errors.New("xkeen-rollback-restore-failed")
		}
	}
	got, _, err := readXKeenInstalledMetadata(binary)
	if err != nil || got != expected || !xkeenLiveTreeReady(binary) {
		return errors.New("xkeen-rollback-postcheck-failed")
	}
	return nil
}
