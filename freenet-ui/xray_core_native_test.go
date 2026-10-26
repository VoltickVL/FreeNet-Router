package main

import (
    "context"
    "io"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

func TestXrayCoreNativeRuntimeIdentityRejectsWrongGroupAndProcess(t *testing.T) {
    dir := t.TempDir()
    binary := filepath.Join(dir, "xray")
    if err := os.WriteFile(binary, []byte("test"), 0755); err != nil { t.Fatal(err) }
    proc := filepath.Join(dir, "proc")
    add := func(pid, gid string) {
        t.Helper()
        dest := filepath.Join(proc, pid)
        if err := os.MkdirAll(dest, 0700); err != nil { t.Fatal(err) }
        if err := os.WriteFile(filepath.Join(dest, "comm"), []byte("xray\n"), 0600); err != nil { t.Fatal(err) }
        if err := os.WriteFile(filepath.Join(dest, "status"), []byte("Name:\txray\nGid:\t"+gid+"\n"), 0600); err != nil { t.Fatal(err) }
        if err := os.Symlink(binary, filepath.Join(dest, "exe")); err != nil { t.Fatal(err) }
    }
    add("1234", "11111\t11111\t11111\t11111")
    if !xrayCoreNativeProcessMatchesInProc(proc, binary) { t.Fatal("healthy native GID and binary were not accepted") }
    if err := os.WriteFile(filepath.Join(proc,"1234","status"), []byte("Gid:\t0\t0\t0\t0\n"), 0600); err != nil { t.Fatal(err) }
    if xrayCoreNativeProcessMatchesInProc(proc,binary) { t.Fatal("GID 0 native runtime must never be accepted") }
    if err := os.WriteFile(filepath.Join(proc,"1234","status"), []byte("Gid:\t11111\t11111\t11111\t11111\n"), 0600); err != nil { t.Fatal(err) }
    add("1235", "11111\t11111\t11111\t11111")
    if xrayCoreNativeProcessMatchesInProc(proc,binary) { t.Fatal("two production Xray processes are ambiguous") }
    if err := os.RemoveAll(filepath.Join(proc, "1235")); err != nil { t.Fatal(err) }
    if err := os.Remove(filepath.Join(proc,"1234","exe")); err != nil { t.Fatal(err) }
    other := filepath.Join(dir,"old-xray")
    if err := os.WriteFile(other, []byte("test"), 0755); err != nil { t.Fatal(err) }
    if err := os.Symlink(other, filepath.Join(proc,"1234","exe")); err != nil { t.Fatal(err) }
    if xrayCoreNativeProcessMatchesInProc(proc,binary) { t.Fatal("running outdated binary inode must not count as successful restart") }
}

func TestXrayCoreNativeXKeenControlContracts(t *testing.T) {
    var calls []string
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        calls = append(calls, r.Method+" "+r.URL.Path)
        if r.URL.Path != "/api/control" { http.NotFound(w, r); return }
        if r.Method == http.MethodPost {
            data, err := io.ReadAll(io.LimitReader(r.Body,512))
            if err != nil || strings.TrimSpace(string(data)) != `{"action":"softRestart"}` {
                t.Errorf("native XKeen restart must use exact safe action, got %q", string(data))
            }
            _, _ = io.WriteString(w, `{"success":true}`)
        } else if r.Method == http.MethodGet {
            _, _ = io.WriteString(w, `{"success":true,"running":true,"currentCore":"xray"}`)
        } else { http.Error(w, "method forbidden", http.StatusMethodNotAllowed) }
    }))
    defer server.Close()
    old := xrayCoreNativeControlEndpoint
    xrayCoreNativeControlEndpoint = server.URL+"/api/control"
    defer func(){ xrayCoreNativeControlEndpoint = old }()
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    info, err := xrayCoreNativeRequest(ctx, "")
    if err != nil || !info.Success || !info.Running || info.CurrentCore != "xray" { t.Fatalf("native state query failed: %+v %v",info,err) }
    if _, err := xrayCoreNativeRequest(ctx,"softRestart"); err != nil { t.Fatalf("native XKeen UI rejected restart: %v",err) }
    if len(calls)!=2 || calls[0]!="GET /api/control" || calls[1]!="POST /api/control" { t.Fatalf("wrong native calls: %v",calls) }
}

func TestXrayCoreNativeUnavailableFailsClosed(t *testing.T) {
    server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){http.Error(w,"unavailable",http.StatusServiceUnavailable)}))
    defer server.Close()
    oldURL:=xrayCoreNativeControlEndpoint
    xrayCoreNativeControlEndpoint=server.URL
    defer func(){xrayCoreNativeControlEndpoint=oldURL}()
    oldProof:=xrayCoreNativeRuntimeProof
    xrayCoreNativeRuntimeProof=func(string) bool{return true}
    defer func(){xrayCoreNativeRuntimeProof=oldProof}()
    if err:=xrayCoreNativePreflight(context.Background(), "/path/to/existing/xray");err==nil {
        t.Fatal("offline native UI must prevent all core mutations")
    }
}
