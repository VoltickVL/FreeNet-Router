package main

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "testing"
    "time"
)

func progressiveXrayFixture(t *testing.T, command string) string {
    t.Helper()
    binary := filepath.Join(t.TempDir(), "xray")
    if err := os.WriteFile(binary, []byte("#!/bin/sh\n"+command+"\n"), 0700); err != nil { t.Fatal(err) }
    return binary
}

func TestXrayCoreProgressiveFastPass(t *testing.T) {
    binary := progressiveXrayFixture(t, "exit 0")
    err := runXrayCoreProgressiveSemanticTest(context.Background(), binary, t.TempDir(), os.Environ(),
        80*time.Millisecond, 220*time.Millisecond, 30*time.Millisecond,
        func(int)(uint64,bool){return 1,true})
    if err != nil { t.Fatalf("fast semantic PASS rejected: %v", err) }
}

func TestXrayCoreProgressiveIdleFailsClosedWithoutExtension(t *testing.T) {
    binary := progressiveXrayFixture(t, "sleep 1")
    start := time.Now()
    err := runXrayCoreProgressiveSemanticTest(context.Background(), binary, t.TempDir(), os.Environ(),
        70*time.Millisecond, 300*time.Millisecond, 30*time.Millisecond,
        func(int)(uint64,bool){return 0,true})
    if !errors.Is(err, errXrayCoreSemanticIdle) { t.Fatalf("idle probe should STOP, got %v", err) }
    if time.Since(start) > 500*time.Millisecond { t.Fatal("idle probe incorrectly consumed extended deadline") }
}

func TestXrayCoreProgressiveObservedCPUPass(t *testing.T) {
    binary := progressiveXrayFixture(t, "sleep 0.13; exit 0")
    calls := 0
    err := runXrayCoreProgressiveSemanticTest(context.Background(), binary, t.TempDir(), os.Environ(),
        80*time.Millisecond, 350*time.Millisecond, 35*time.Millisecond,
        func(int)(uint64,bool){calls++;return uint64(calls),true})
    if err != nil { t.Fatalf("CPU-advancing slow semantic PASS rejected: %v", err) }
    if calls < 2 { t.Fatal("semantic extension was not tied to observed process progress") }
}

func TestXrayCoreProgressiveActiveButNeverPassesFailsClosed(t *testing.T) {
    binary := progressiveXrayFixture(t, "sleep 1")
    calls := 0
    err := runXrayCoreProgressiveSemanticTest(context.Background(), binary, t.TempDir(), os.Environ(),
        70*time.Millisecond, 155*time.Millisecond, 30*time.Millisecond,
        func(int)(uint64,bool){calls++;return uint64(calls),true})
    if !errors.Is(err, errXrayCoreSemanticProgressDeadline) {
        t.Fatalf("progress may extend observation but never bypass semantic PASS: %v", err)
    }
}

func TestXrayCoreProgressiveBadConfigFailsBeforeExtension(t *testing.T) {
    binary := progressiveXrayFixture(t, "exit 31")
    err := runXrayCoreProgressiveSemanticTest(context.Background(), binary, t.TempDir(), os.Environ(),
        70*time.Millisecond, 155*time.Millisecond, 30*time.Millisecond,
        func(int)(uint64,bool){return 2,true})
    if err == nil { t.Fatal("nonzero semantic exit must not pass") }
    if errors.Is(err, errXrayCoreSemanticIdle) || errors.Is(err, errXrayCoreSemanticProgressDeadline) {
        t.Fatalf("nonzero exit must not be reported as timeout: %v", err)
    }
}

func TestXrayCoreCPUJiffiesReadsSelf(t *testing.T) {
    if _, ok := readXrayCoreCPUJiffies(os.Getpid()); !ok {
        t.Skip("/proc process stats are not available")
    }
}
