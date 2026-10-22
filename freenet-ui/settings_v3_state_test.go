package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

const v3StateReservationTestStamp = "2026-10-22T01:00:00Z"

// This is an actual second process using the production update function, not
// a separate in-memory mutex. The parent controls when its locked plan ends.
func TestV3StateReservationWriterProcess(t *testing.T) {
	if os.Getenv("FREENET_V3_STATE_TEST_CHILD") != "reservation" {
		return
	}
	var waitErr error
	err := v3UpdateState(func(state map[string]string) map[string]string {
		if state["EMERGENCY_SCAN_CURSOR"] != "7" {
			waitErr = errors.New("child did not read the existing state")
			return nil
		}
		if err := os.WriteFile(os.Getenv("FREENET_V3_STATE_TEST_READY"), []byte("ready"), 0600); err != nil {
			waitErr = err
			return nil
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(os.Getenv("FREENET_V3_STATE_TEST_RELEASE")); err == nil {
				return map[string]string{"QUALITY_OPTIMIZATION_LAST": v3StateReservationTestStamp}
			}
			if !time.Now().Before(deadline) {
				waitErr = errors.New("parent did not release the child planner")
				return nil
			}
			time.Sleep(5 * time.Millisecond)
		}
	})
	if err != nil || waitErr != nil {
		t.Fatalf("child update failed: update=%v wait=%v", err, waitErr)
	}
}

func TestV3StateCrossProcessHealthWritePreservesQualityReservation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.state")
	t.Setenv("FREENET_SETTINGS_V3_STATE", path)
	if err := v3WriteState(map[string]string{"EMERGENCY_SCAN_CURSOR": "7"}); err != nil {
		t.Fatal(err)
	}
	lockBefore, err := os.Lstat(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	ready, release := filepath.Join(dir, "ready"), filepath.Join(dir, "release")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestV3StateReservationWriterProcess$", "-test.timeout=8s")
	cmd.Env = append(os.Environ(),
		"FREENET_V3_STATE_TEST_CHILD=reservation",
		"FREENET_V3_STATE_TEST_READY="+ready,
		"FREENET_V3_STATE_TEST_RELEASE="+release,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	childDone, childCompleted := make(chan error, 1), make(chan struct{})
	go func() {
		childDone <- cmd.Wait()
		close(childCompleted)
	}()
	var healthCompleted chan struct{}
	defer func() {
		_ = os.WriteFile(release, []byte("release"), 0600)
		cancel()
		<-childCompleted
		if healthCompleted != nil {
			<-healthCompleted
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-childDone:
			t.Fatalf("child exited before its locked plan: %v; %s %s", err, stdout.String(), stderr.String())
		default:
		}
		if !time.Now().Before(deadline) {
			t.Fatal("child did not reach its locked plan")
		}
		time.Sleep(5 * time.Millisecond)
	}

	healthStarted, healthDone := make(chan struct{}), make(chan error, 1)
	healthCompleted = make(chan struct{})
	go func() {
		defer close(healthCompleted)
		close(healthStarted)
		healthDone <- v3WriteState(map[string]string{"HEALTH_LAST": "2026-10-22T01:00:01Z"})
	}()
	<-healthStarted
	select {
	case err := <-healthDone:
		t.Fatalf("health writer bypassed the other process's locked reservation: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if err := os.WriteFile(release, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-childDone:
		if err != nil {
			t.Fatalf("child update: %v; %s %s", err, stdout.String(), stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("child did not finish after release")
	}
	select {
	case err := <-healthDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("health writer did not finish after the child released its lock")
	}
	state := v3ParseState(path)
	if state["QUALITY_OPTIMIZATION_LAST"] != v3StateReservationTestStamp ||
		state["HEALTH_LAST"] != "2026-10-22T01:00:01Z" || state["EMERGENCY_SCAN_CURSOR"] != "7" {
		t.Fatalf("concurrent state merge lost a reservation or unrelated key: %#v", state)
	}
	lockAfter, err := os.Lstat(path + ".lock")
	if err != nil || !os.SameFile(lockBefore, lockAfter) || lockAfter.Mode().Perm() != 0600 {
		t.Fatalf("state writes replaced or exposed their permanent lock inode: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("state file is not private: %v", err)
	}
}

func TestV3StateContendedLockCannotAdmitDueOptimization(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.state")
	t.Setenv("FREENET_SETTINGS_V3_STATE", path)
	now := time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC)
	if err := v3WriteState(map[string]string{
		"QUALITY_DEGRADED_COUNT":   "3",
		"QUALITY_DEGRADED_SAMPLES": "1",
		"QUALITY_DEGRADED_SINCE":   now.Add(-time.Minute).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(path+".lock", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		t.Fatal(err)
	}
	completed, result := make(chan struct{}), make(chan bool, 1)
	defer func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
		<-completed
	}()
	started := time.Now()
	go func() {
		defer close(completed)
		result <- automationQualityOptimizationDue(now, 3)
	}()
	select {
	case due := <-result:
		if due {
			t.Fatal("a due planner admitted work without persisting its cooldown")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("state lock acquisition exceeded its bounded wait")
	}
	if time.Since(started) < settingsV3StateLockWait*9/10 {
		t.Fatal("contended writer did not use its bounded lock wait")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("contended update changed the existing state: %v", err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	// The same ready-to-run evidence is accepted after the lock is released,
	// and its successful write suppresses the next attempt.
	if !automationQualityOptimizationDue(now, 3) {
		t.Fatal("valid due evidence was not admitted after the lock was released")
	}
	for _, delta := range []time.Duration{time.Second, 2 * time.Second} {
		if automationQualityOptimizationDue(now.Add(delta), 3) {
			t.Fatal("the persisted cooldown admitted a second optimization")
		}
	}
	if got := v3ParseState(path)["QUALITY_OPTIMIZATION_LAST"]; got != now.Format(time.RFC3339) {
		t.Fatalf("cooldown reservation=%q", got)
	}
}

func TestV3StateReadOrLockFailureDoesNotRunPlanner(t *testing.T) {
	for _, which := range []string{"state_directory", "state_symlink", "lock_directory", "lock_symlink", "parent_file"} {
		t.Run(which, func(t *testing.T) {
			dir := t.TempDir()
			path, target := filepath.Join(dir, "settings.state"), filepath.Join(dir, "sentinel")
			original := []byte("QUALITY_OPTIMIZATION_LAST=" + v3StateReservationTestStamp + "\n")
			if err := os.WriteFile(target, original, 0600); err != nil {
				t.Fatal(err)
			}
			switch which {
			case "state_directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "state_symlink":
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "lock_directory":
				if err := os.Mkdir(path+".lock", 0700); err != nil {
					t.Fatal(err)
				}
			case "lock_symlink":
				if err := os.Symlink(target, path+".lock"); err != nil {
					t.Fatal(err)
				}
			case "parent_file":
				path = filepath.Join(target, "settings.state")
			}
			t.Setenv("FREENET_SETTINGS_V3_STATE", path)
			called := false
			err := v3UpdateState(func(map[string]string) map[string]string {
				called = true
				return map[string]string{"QUALITY_OPTIMIZATION_LAST": "changed"}
			})
			if err == nil || called {
				t.Fatalf("unsafe/unreadable state admitted a planner: called=%v err=%v", called, err)
			}
			after, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(original, after) {
				t.Fatalf("failed state update changed the sentinel: %v", err)
			}
		})
	}
}

func TestV3StateWriteFailureIsReturnedAndLockRemainsUsable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.state")
	t.Setenv("FREENET_SETTINGS_V3_STATE", path)
	var injectedErr error
	called := false
	err := v3UpdateState(func(map[string]string) map[string]string {
		called = true
		// Fail the atomic rename after a valid read and completed plan. This
		// uses only a private test directory and also fails when CI runs as root.
		injectedErr = os.Mkdir(path, 0700)
		return map[string]string{"QUALITY_OPTIMIZATION_LAST": v3StateReservationTestStamp}
	})
	if !called || injectedErr != nil || err == nil {
		t.Fatalf("post-plan write failure was hidden: called=%v injected=%v write=%v", called, injectedErr, err)
	}
	lockBefore, err := os.Lstat(path + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, ".freenet-ui-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("failed write left a temporary state file: %v %v", leftovers, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := v3WriteState(map[string]string{"HEALTH_LAST": "2026-10-22T01:00:02Z"}); err != nil {
		t.Fatalf("failed write left its lock held: %v", err)
	}
	lockAfter, err := os.Lstat(path + ".lock")
	if err != nil || !os.SameFile(lockBefore, lockAfter) {
		t.Fatalf("retry replaced the permanent lock inode: %v", err)
	}
	state := v3ParseState(path)
	if state["QUALITY_OPTIMIZATION_LAST"] != "" || state["HEALTH_LAST"] != "2026-10-22T01:00:02Z" {
		t.Fatalf("failed reservation leaked into a later successful write: %#v", state)
	}
}
