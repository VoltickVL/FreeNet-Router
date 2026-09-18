package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func selfUpdateProcRoot() string {
	if root := strings.TrimSpace(os.Getenv("FREENET_PROC_ROOT")); root != "" {
		return root
	}
	return "/proc"
}

func updaterCmdlineMatches(data []byte, helper string) bool {
	parts := bytes.Split(data, []byte{0})
	for i := 0; i < len(parts); i++ {
		if string(parts[i]) != helper {
			continue
		}
		if i+1 < len(parts) && string(parts[i+1]) == "apply" {
			return true
		}
	}
	return false
}

func (a *app) selfUpdateProcessActivity() (active bool, observable bool) {
	root := selfUpdateProcRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		return false, false
	}
	observable = true

	if b, err := os.ReadFile(filepath.Join(a.cfg.UpdateLock, "owner.pid")); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
			if cmdline, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "cmdline")); err == nil && updaterCmdlineMatches(cmdline, a.cfg.SelfUpdatePath) {
				return true, true
			}
		}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join(root, entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		if updaterCmdlineMatches(cmdline, a.cfg.SelfUpdatePath) {
			return true, true
		}
	}
	return false, true
}

func staleUpdateUnlockSafe(kv map[string]string) bool {
	state := strings.TrimSpace(kv["STATE"])
	rollback := strings.TrimSpace(kv["ROLLBACK_STATE"])
	if state == "ROLLBACK_FAILED" || rollback == "FAILED_UNKNOWN" || rollback == "PENDING" {
		return false
	}
	switch state {
	case "", "IDLE", "CHECKING", "BUSY", "FAILED", "SUCCESS":
		return rollback == "" || rollback == "NOT_NEEDED" || rollback == "SUCCESS"
	default:
		return false
	}
}

func (a *app) updateLockStatus() (held bool, staleSafe bool) {
	a.updateMu.Lock()
	launching := a.updateLaunching
	a.updateMu.Unlock()
	if launching {
		return true, false
	}
	if _, err := os.Stat(a.cfg.UpdateLock); err != nil {
		return false, false
	}
	kv := readStateFile(a.cfg.UpdateState)
	if !staleUpdateUnlockSafe(kv) {
		return true, false
	}
	active, observable := a.selfUpdateProcessActivity()
	if active || !observable {
		return true, false
	}
	return true, true
}

func writeUnlockedUpdateState(path string, kv map[string]string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".freenet-update-state.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	from := strings.TrimSpace(kv["FROM_VERSION"])
	target := strings.TrimSpace(kv["TARGET_VERSION"])
	lines := []string{
		"STATE=IDLE",
		"FROM_VERSION=" + from,
		"TARGET_VERSION=" + target,
		"MESSAGE=Зависшая блокировка обновления снята; можно безопасно повторить обновление",
		"PRIMARY_ERROR=",
		"ROLLBACK_STATE=NOT_NEEDED",
		"BACKUP_DIR=",
		"UPDATED_AT=" + time.Now().UTC().Format(time.RFC3339),
	}
	if _, err := tmp.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (a *app) unlockStaleUpdateLock() error {
	a.updateMu.Lock()
	defer a.updateMu.Unlock()

	if a.updateLaunching {
		return errors.New("updater process is still launching")
	}
	if _, err := os.Stat(a.cfg.UpdateLock); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return errors.New("cannot inspect update lock")
	}
	kv := readStateFile(a.cfg.UpdateState)
	if !staleUpdateUnlockSafe(kv) {
		return errors.New("update lock is not safe to unlock automatically")
	}
	active, observable := a.selfUpdateProcessActivity()
	if active {
		return errors.New("updater process is still running")
	}
	if !observable {
		return errors.New("updater process state cannot be verified")
	}
	if err := os.RemoveAll(a.cfg.UpdateLock); err != nil {
		return errors.New("cannot remove stale update lock")
	}
	if err := writeUnlockedUpdateState(a.cfg.UpdateState, kv); err != nil {
		return errors.New("stale update lock removed but updater state could not be normalized")
	}
	return nil
}
