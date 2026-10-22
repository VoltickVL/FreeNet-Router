package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
)

const defaultIsolatedXrayProbeLockFile = "/tmp/freenet-xray-probes.lock"

func isolatedXrayProbeLockFilePath() string {
	if path := strings.TrimSpace(os.Getenv("FREENET_XRAY_PROBE_LOCK_FILE")); path != "" {
		return path
	}
	return defaultIsolatedXrayProbeLockFile
}

func isolatedXrayProbeLockFileOwned(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1 && stat.Uid == uint32(os.Geteuid())
}

// Every lease opens its own descriptor: sharing one descriptor would let an
// exclusive flock replace this process's active shared leases. The inode is
// permanent and is never unlinked, including after cancellation or release.
// ctx limits acquisition only. A successful lease lasts until explicit release,
// even when its caller immediately cancels the acquisition context.
func acquireIsolatedXrayProbeFileLock(ctx context.Context, exclusive bool) (func(), error) {
	if ctx == nil {
		return nil, errIsolatedXrayProbeAdmission
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := isolatedXrayProbeLockFilePath()
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (func(), error) {
		_ = file.Close()
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	if !isolatedXrayProbeLockFileOwned(info) {
		return fail(errors.New("isolated Xray probe lock is not an owned private regular file"))
	}
	mode := syscall.LOCK_SH
	if exclusive {
		mode = syscall.LOCK_EX
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		err = syscall.Flock(int(file.Fd()), mode|syscall.LOCK_NB)
		if err == nil {
			current, statErr := os.Lstat(path)
			if statErr != nil {
				return fail(statErr)
			}
			if !isolatedXrayProbeLockFileOwned(current) || !os.SameFile(info, current) {
				return fail(errors.New("isolated Xray probe lock changed while waiting"))
			}
			if err := ctx.Err(); err != nil {
				return fail(err)
			}
			var once sync.Once
			return func() {
				once.Do(func() { _ = file.Close() })
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EINTR) {
			return fail(err)
		}
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-ticker.C:
		}
	}
}
