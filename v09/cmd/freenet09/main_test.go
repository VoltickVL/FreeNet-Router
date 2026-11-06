package main

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func writeFixture(t *testing.T, root, rel, contents string) {
    t.Helper()
    path := filepath.Join(root, rel)
    if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil { t.Fatal(err) }
    if err := os.WriteFile(path, []byte(contents), 0600); err != nil { t.Fatal(err) }
}

func TestNativeOverviewNeverReadsConfigOrDisclosesSecrets(t *testing.T) {
    root := t.TempDir()
    writeFixture(t, root, "sbin/xray", "xray")
    writeFixture(t, root, "sbin/xkeen", "xkeen")
    writeFixture(t, root, "sbin/freenet-ui", "legacy")
    writeFixture(t, root, "etc/init.d/S99freenet-ui", "legacy-init")
    writeFixture(t, root, "etc/xray/configs/04_outbounds.json", `{"secret":"CREDENTIAL-NEVER-EXPOSE","address":"private.example"}`)

    h := handlers(root)
    req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
    rec := httptest.NewRecorder()
    h.ServeHTTP(rec, req)
    if rec.Code != http.StatusOK { t.Fatalf("GET /api/overview: %d",rec.Code) }
    var overview nativeOverview
    if err := json.Unmarshal(rec.Body.Bytes(), &overview); err != nil { t.Fatal(err) }
    if overview.Mode != "READ_ONLY" || overview.Runtime != "NOT_VERIFIED" ||
        overview.Xray != "PRESENT" || overview.XKeen != "PRESENT" ||
        overview.XrayConfig != "PRESENT" || overview.LegacyFreeNet != "PRESENT" {
        t.Fatalf("unexpected inventory: %+v",overview)
    }
    for _, secret := range []string{"CREDENTIAL-NEVER-EXPOSE","private.example","04_outbounds.json"} {
        if strings.Contains(rec.Body.String(),secret) { t.Fatalf("secret/config exposed: %q",secret) }
    }
    if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "default-src 'none'") {
        t.Fatal("browser security headers missing")
    }

    for _, methodPath := range [][2]string{
        {"POST","/api/overview"}, {"POST","/api/update"},
        {"DELETE","/api/native/xray"}, {"GET","/api/update"},
    } {
        response := httptest.NewRecorder()
        h.ServeHTTP(response,httptest.NewRequest(methodPath[0], methodPath[1],nil))
        if response.Code == http.StatusOK { t.Fatalf("unsupported operation allowed: %v",methodPath) }
    }
    page := httptest.NewRecorder()
    h.ServeHTTP(page,httptest.NewRequest(http.MethodGet,"/",nil))
    if page.Code != http.StatusOK || !strings.Contains(page.Body.String(),"FreeNet 0.9 · Control Center") {
        t.Fatal("preview page not rendered")
    }
    if strings.Contains(page.Body.String(), "CREDENTIAL-NEVER-EXPOSE") {
        t.Fatal("HTML leaked native config")
    }
    actual, err := os.ReadFile(filepath.Join(root,"etc/xray/configs/04_outbounds.json"))
    if err != nil || !strings.Contains(string(actual),"CREDENTIAL-NEVER-EXPOSE") {
        t.Fatal("read-only app unexpectedly changed native config")
    }
}

func TestSymlinkCannotMasqueradeAsNativeComponent(t *testing.T) {
    root := t.TempDir()
    elsewhere := filepath.Join(t.TempDir(),"xray")
    if err := os.WriteFile(elsewhere,[]byte("x"),0600); err!=nil { t.Fatal(err) }
    if err := os.MkdirAll(filepath.Join(root,"sbin"),0700); err!=nil { t.Fatal(err) }
    if err := os.Symlink(elsewhere,filepath.Join(root,"sbin/xray")); err!=nil { t.Skip(err) }
    if state:=observedPath(root,"sbin/xray",false);state!="UNKNOWN" {
        t.Fatalf("symlink must not be trusted: %s",state)
    }
    if state:=observedPath(root,"etc/xray/configs",true);state!="MISSING" {
        t.Fatalf("missing directory: %s",state)
    }
    if state:=observedPath(root,"../etc/shadow",false);state!="UNKNOWN" {
        t.Fatalf("path escape: %s",state)
    }
}

func TestPreviewListenOnlyLocal(t *testing.T) {
    good:=[]string{"127.0.0.1:1009","[::1]:1009"}
    bad:=[]string{":1009","0.0.0.0:1009","[::]:1009","192.168.50.1:1009","localhost:1009","example.org:443","random"}
    for _,addr:=range good { if !loopbackListen(addr) { t.Fatalf("loopback rejected %q",addr) } }
    for _,addr:=range bad { if loopbackListen(addr) { t.Fatalf("unsafe listener accepted %q",addr) } }
    if err:=selfTest(); err!=nil { t.Fatal(err) }
}
