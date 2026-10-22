package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const settingsV3StateLockWait = time.Second

var errSettingsV3StateLockTimeout = errors.New("settings state lock is busy")

// The lock has a permanent inode separate from the atomically replaced state.
// Every writer, including CLI health workers, holds it across read/plan/write.
// Never unlink the lock: a waiter may already hold an open descriptor to it.
func acquireV3StateLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*os.File, error) {
		_ = file.Close()
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || stat.Nlink != 1 || stat.Uid != uint32(os.Geteuid()) {
		return fail(errors.New("settings state lock is not an owned regular file"))
	}
	if err := file.Chmod(0600); err != nil {
		return fail(err)
	}
	deadline := time.Now().Add(settingsV3StateLockWait)
	for {
		if !time.Now().Before(deadline) {
			return fail(errSettingsV3StateLockTimeout)
		}
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			current, statErr := os.Lstat(path)
			if statErr != nil {
				return fail(statErr)
			}
			if !current.Mode().IsRegular() || !os.SameFile(info, current) {
				return fail(errors.New("settings state lock changed while waiting"))
			}
			return file, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			return fail(err)
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fail(errSettingsV3StateLockTimeout)
		}
		delay := 10 * time.Millisecond
		if remaining < delay {
			delay = remaining
		}
		time.Sleep(delay)
	}
}

// Unlike the display-only parser, a state mutation must not turn an I/O error
// into an empty map and thereby erase an existing cooldown reservation.
func readV3StateForUpdate(path string) (map[string]string, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ENOENT) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("settings state is not a regular file")
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	return v3ParseStateBytes(data), nil
}

// update returns only changed keys. It must be a short, non-reentrant planner;
// nil/empty means no write. A caller may act on its decision only after nil err.
func v3UpdateState(update func(map[string]string) map[string]string) error {
	if update == nil {
		return errors.New("settings state update is missing")
	}
	path := settingsV3StatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	lock, err := acquireV3StateLock(path + ".lock")
	if err != nil {
		return err
	}
	defer func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}()
	current, err := readV3StateForUpdate(path)
	if err != nil {
		return err
	}
	updates := update(current)
	if len(updates) == 0 {
		return nil
	}
	for key, value := range updates {
		current[key] = sanitizeAutomationReason(value)
	}
	keys := make([]string, 0, len(current))
	for key := range current {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "%s=%s\n", key, strings.ReplaceAll(current[key], "\n", " "))
	}
	return atomicWrite(path, []byte(b.String()), 0600)
}
