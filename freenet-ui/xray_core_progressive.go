package main

import (
    "context"
    "errors"
    "os"
    "os/exec"
    "strconv"
    "strings"
    "time"
)

var (
    errXrayCoreSemanticIdle = errors.New("xray semantic child made no CPU progress")
    errXrayCoreSemanticProgressDeadline = errors.New("xray semantic child exceeded progressive deadline")
)

// /proc/[pid]/stat is read in-process and never returned in HTTP, issues or
// logs. The jiffies counter is used only to verify that a MIPS child is still
// doing work before extending the ORIGINAL 35-second bound.
func readXrayCoreCPUJiffies(pid int) (uint64, bool) {
    if pid <= 0 { return 0, false }
    b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
    if err != nil { return 0, false }
    // comm is parenthesized and can contain spaces; count fields after it.
    tail := strings.LastIndex(string(b), ") ")
    if tail < 0 { return 0, false }
    fields := strings.Fields(string(b)[tail+2:])
    if len(fields) < 13 { return 0, false }
    user, err1 := strconv.ParseUint(fields[11], 10, 64)
    system, err2 := strconv.ParseUint(fields[12], 10, 64)
    if err1 != nil || err2 != nil { return 0, false }
    return user + system, true
}

// The first budget is unchanged. Only observed CPU advancement *near its
// expiry* allows the same child to continue, still within a hard parent
// deadline. No semantic PASS can be inferred from CPU activity: a zero exit
// from xray run -test is mandatory, for BOTH old and candidate binaries.
//
// The injectable cpu reader and scaled budgets permit deterministic tests;
// production always uses /proc CPU jiffies, not a guessed completion time.
func runXrayCoreProgressiveSemanticTest(
    parent context.Context, binary, configDir string, env []string,
    soft, hard, progressWindow time.Duration, cpu func(int) (uint64, bool),
) error {
    if parent == nil || cpu == nil || soft <= progressWindow || progressWindow <= 0 || hard < soft {
        return errors.New("invalid Xray semantic test budget")
    }
    ctx, cancel := context.WithCancel(parent)
    defer cancel()
    // A real /dev/null file avoids Go's io.Discard copy pipes. Child
    // descendants inheriting a pipe must never extend cmd.Wait after STOP.
    sink, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
    if err != nil { return errors.New("cannot prepare Xray semantic output sink") }
    defer sink.Close()
    cmd := exec.CommandContext(ctx, binary, "run", "-test", "-confdir", configDir)
    cmd.Env = env
    cmd.Stdout, cmd.Stderr = sink, sink
    cmd.WaitDelay = 2 * time.Second
    if err := cmd.Start(); err != nil { return err }
    childStart := time.Now()
    done := make(chan error, 1)
    go func(){ done <- cmd.Wait() }()
    beforeTimer := time.NewTimer(soft - progressWindow)
    deadlineTimer := time.NewTimer(soft)
    defer beforeTimer.Stop()
    defer deadlineTimer.Stop()
    var before uint64
    var beforeOK bool
    waitDone := func() {
        cancel()
        <-done
    }
    for {
        select {
        case err := <-done:
            if parent.Err() != nil { return parent.Err() }
            return err
        case <-parent.Done():
            waitDone()
            return parent.Err()
        case <-beforeTimer.C:
            before, beforeOK = cpu(cmd.Process.Pid)
        case <-deadlineTimer.C:
            // A completion arriving at the soft deadline is still a PASS
            // when the child actually exited successfully.
            select {
            case err := <-done:
                if parent.Err() != nil { return parent.Err() }
                return err
            default:
            }
            now, ok := cpu(cmd.Process.Pid)
            if !beforeOK || !ok || now <= before {
                waitDone()
                return errXrayCoreSemanticIdle
            }
            remaining := hard - time.Since(childStart)
            if remaining <= 0 {
                waitDone()
                return errXrayCoreSemanticProgressDeadline
            }
            hardTimer := time.NewTimer(remaining)
            defer hardTimer.Stop()
            select {
            case err := <-done:
                if parent.Err() != nil { return parent.Err() }
                return err
            case <-hardTimer.C:
                waitDone()
                return errXrayCoreSemanticProgressDeadline
            case <-parent.Done():
                waitDone()
                return parent.Err()
            }
        }
    }
}
