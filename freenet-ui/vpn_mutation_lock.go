package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultVPNMutationLockPath = "/tmp/blanc_xkeen_update.lock"

var (
	errVPNMutationBusy        = errors.New("another VPN mutation is already running")
	errVPNMutationLockUnknown = errors.New("shared VPN mutation lock is unavailable")
)

func vpnMutationLockPath() string {
	if path := strings.TrimSpace(os.Getenv("FREENET_LOCK_DIR")); path != "" {
		return path
	}
	return defaultVPNMutationLockPath
}

func vpnMutationProcRoot() string {
	if root := strings.TrimSpace(os.Getenv("FREENET_PROC_ROOT")); root != "" {
		return root
	}
	return "/proc"
}

func vpnMutationPIDActive(pid int) (bool, bool) {
	if pid <= 0 {
		return false, true
	}
	_, err := os.Stat(filepath.Join(vpnMutationProcRoot(), strconv.Itoa(pid)))
	if err == nil {
		return true, true
	}
	if os.IsNotExist(err) {
		return false, true
	}
	return false, false
}

func writeVPNMutationLockOwner(path string, pid int) error {
	return os.WriteFile(filepath.Join(path, "pid"), []byte(strconv.Itoa(pid)+"\n"), 0600)
}

func liveVPNMutationLockOwner(path string) (bool, bool) {
	data, err := os.ReadFile(filepath.Join(path, "pid"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, true
		}
		return false, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false, true
	}
	return vpnMutationPIDActive(pid)
}

func acquireVPNMutationLock() (func(), error) {
	path := vpnMutationLockPath()
	pid := os.Getpid()
	tryCreate := func() error {
		if err := os.Mkdir(path, 0700); err != nil {
			return err
		}
		if err := writeVPNMutationLockOwner(path, pid); err != nil {
			_ = os.RemoveAll(path)
			return err
		}
		return nil
	}
	if err := tryCreate(); err == nil {
		return releaseVPNMutationLock(path, pid), nil
	} else if !os.IsExist(err) {
		return nil, errVPNMutationLockUnknown
	}

	// Shell updater/provider writers create the directory before the pid file.
	// Give that tiny ownership window time to settle before classifying a lock
	// without a readable pid as stale.
	time.Sleep(time.Second)
	active, observable := liveVPNMutationLockOwner(path)
	if active || !observable {
		return nil, errVPNMutationBusy
	}

	// Re-check immediately before stale cleanup so a just-started owner cannot
	// be removed after publishing its pid.
	active, observable = liveVPNMutationLockOwner(path)
	if active || !observable {
		return nil, errVPNMutationBusy
	}
	if err := os.RemoveAll(path); err != nil {
		return nil, errVPNMutationLockUnknown
	}
	if err := tryCreate(); err != nil {
		if os.IsExist(err) {
			return nil, errVPNMutationBusy
		}
		return nil, errVPNMutationLockUnknown
	}
	return releaseVPNMutationLock(path, pid), nil
}

func releaseVPNMutationLock(path string, pid int) func() {
	return func() {
		data, err := os.ReadFile(filepath.Join(path, "pid"))
		if err != nil {
			return
		}
		owner, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || owner != pid {
			return
		}
		_ = os.RemoveAll(path)
	}
}

func vpnMutationLockMessage(err error) string {
	if errors.Is(err, errVPNMutationBusy) {
		return "another VPN/Xray mutation is already running"
	}
	return "shared VPN/Xray mutation lock is unavailable"
}
