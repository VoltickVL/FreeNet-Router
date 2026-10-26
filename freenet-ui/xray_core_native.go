package main

import (
    "context"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "time"
)

// The native XKeen UI already owns the running XKeen/Xray process lifecycle.
// FreeNet must not substitute the xkeen CLI -restart for its softRestart:
// the UI starts Xray with GID=11111, which the existing router's routing
// rules require. Keep the endpoint on loopback, never on a remote host.
const xrayCoreNativeControlURL = "http://127.0.0.1:1000/api/control"

var xrayCoreNativeControlEndpoint = xrayCoreNativeControlURL

var xrayCoreNativeClient = &http.Client{
    Timeout: 8 * time.Second,
    Transport: &http.Transport{Proxy: nil},
}

// Injectable read-only runtime proof permits fault-injection tests without
// restarting any production process. Production requires the actual running
// Xray process to execute the binary we just installed under GID 11111.
var xrayCoreNativeRuntimeProof = xrayCoreNativeProcessMatches

type xrayCoreNativeControlResponse struct {
    Success bool   `json:"success"`
    Running bool   `json:"running"`
    CurrentCore string `json:"currentCore"`
}

func xrayCoreNativeRequest(ctx context.Context, action string) (xrayCoreNativeControlResponse, error) {
    var result xrayCoreNativeControlResponse
    if ctx.Err() != nil { return result, ctx.Err() }
    method := http.MethodGet
    var body io.Reader
    if action != "" {
        if action != "softRestart" { return result, errors.New("unsupported native Xray control action") }
        method = http.MethodPost
        body = strings.NewReader(`{"action":"softRestart"}`)
    }
    req, err := http.NewRequestWithContext(ctx, method, xrayCoreNativeControlEndpoint, body)
    if err != nil { return result, errors.New("native XKeen UI request unavailable") }
    if body != nil { req.Header.Set("Content-Type", "application/json") }
    resp, err := xrayCoreNativeClient.Do(req)
    if err != nil { return result, errors.New("native XKeen UI unavailable") }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK { return result, errors.New("native XKeen UI rejected the request") }
    if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&result); err != nil || !result.Success {
        return result, errors.New("native XKeen UI did not acknowledge Xray control")
    }
    return result, nil
}

func xrayCoreNativePreflight(ctx context.Context, binary string) error {
    state, err := xrayCoreNativeRequest(ctx, "")
    if err != nil { return err }
    if !state.Running || state.CurrentCore != "xray" || !xrayCoreNativeRuntimeProof(binary) {
        return errors.New("native XKeen/Xray runtime not verified; no binary changed")
    }
    return nil
}

// This proof does not trust a version string on disk or a green status badge:
// require exactly one production Xray, the target executable inode, and its
// actual kernel GID. Ignore unrelated process names including fn-xray-probe.
func xrayCoreNativeProcessMatches(binary string) bool {
    return xrayCoreNativeProcessMatchesInProc("/proc", binary)
}

func xrayCoreNativeProcessMatchesInProc(procRoot, binary string) bool {
    live, err := os.Stat(binary)
    if err != nil || !live.Mode().IsRegular() { return false }
    processes, err := os.ReadDir(procRoot)
    if err != nil { return false }
    matches := 0
    for _, process := range processes {
        if !process.IsDir() { continue }
        if _, err := strconv.Atoi(process.Name()); err != nil { continue }
        dir := filepath.Join(procRoot, process.Name())
        comm, err := os.ReadFile(filepath.Join(dir, "comm"))
        if err != nil || strings.TrimSpace(string(comm)) != "xray" { continue }
        matches++
        if matches > 1 { return false }
        status, err := os.ReadFile(filepath.Join(dir, "status"))
        if err != nil { return false }
        validGID := false
        for _, row := range strings.Split(string(status), "\n") {
            fields := strings.Fields(row)
            if len(fields) >= 5 && fields[0] == "Gid:" &&
                fields[1] == "11111" && fields[2] == "11111" &&
                fields[3] == "11111" && fields[4] == "11111" {
                validGID = true
                break
            }
        }
        if !validGID { return false }
        executable, err := os.Stat(filepath.Join(dir, "exe"))
        if err != nil || !os.SameFile(live, executable) { return false }
    }
    return matches == 1
}

// Native XKeen UI handles the process restart. A short, bounded post-check
// proves the new running executable, instead of executing the 30+-second
// 'xray run -test -confdir' AGAIN after a successful restart. Rollback uses
// the exact same native path and receives a separate recovery deadline.
func (a *app) restartXrayCoreUpdateOwned(ctx context.Context, binary, version string) error {
    if ctx.Err() != nil { return errors.New("native Xray restart deadline expired") }
    if _, err := xrayCoreNativeRequest(ctx, "softRestart"); err != nil { return err }
    ticker := time.NewTicker(350 * time.Millisecond)
    defer ticker.Stop()
    consecutive := 0
    for {
        if ctx.Err() != nil { return errors.New("native Xray start did not become healthy before deadline") }
        state, err := xrayCoreNativeRequest(ctx, "")
        if err == nil && state.Running && state.CurrentCore == "xray" && xrayCoreNativeRuntimeProof(binary) {
            checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
            current, versionErr := xrayCoreVersionAt(checkCtx, binary)
            cancel()
            if versionErr == nil && current == version {
                consecutive++
                if consecutive >= 3 { return nil }
            } else {
                consecutive = 0
            }
        } else {
            consecutive = 0
        }
        select {
        case <-ctx.Done(): return errors.New("native Xray restart readiness timeout")
        case <-ticker.C:
        }
    }
}
