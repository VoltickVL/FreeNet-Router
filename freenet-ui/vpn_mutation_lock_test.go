package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestVPNMutationLockAcquireAndRelease(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "vpn.lock")
	t.Setenv("FREENET_LOCK_DIR", lock)

	release, err := acquireVPNMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(lock, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strconv.Itoa(os.Getpid())+"\n" {
		t.Fatalf("owner pid=%q", data)
	}
	release()
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("owned lock was not released: %v", err)
	}
}

func TestVPNMutationLockRejectsLiveOwner(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "vpn.lock")
	t.Setenv("FREENET_LOCK_DIR", lock)
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lock, "pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	release, err := acquireVPNMutationLock()
	if release != nil {
		t.Fatal("busy lock returned a release function")
	}
	if !errors.Is(err, errVPNMutationBusy) {
		t.Fatalf("error=%v want busy", err)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("live owner lock was removed: %v", err)
	}
}

func TestVPNMutationLockRecoversDeadPID(t *testing.T) {
	root := t.TempDir()
	lock := filepath.Join(root, "vpn.lock")
	proc := filepath.Join(root, "proc")
	if err := os.Mkdir(proc, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_LOCK_DIR", lock)
	t.Setenv("FREENET_PROC_ROOT", proc)
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lock, "pid"), []byte("99999999\n"), 0600); err != nil {
		t.Fatal(err)
	}

	release, err := acquireVPNMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(lock, "pid"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strconv.Itoa(os.Getpid())+"\n" {
		t.Fatalf("stale lock owner was not replaced: %q", data)
	}
	release()
}

func TestVPNMutationLockReleaseDoesNotRemoveChangedOwner(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "vpn.lock")
	t.Setenv("FREENET_LOCK_DIR", lock)
	release, err := acquireVPNMutationLock()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lock, "pid"), []byte("424242\n"), 0600); err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("release removed a lock whose owner changed: %v", err)
	}
}
