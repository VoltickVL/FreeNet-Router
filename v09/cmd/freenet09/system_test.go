package main

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestNativeSystemUsesOnlyOfficialEvidence(t *testing.T) {
    root:=t.TempDir()
    proc:=t.TempDir()
    writeFixture(t,root,"sbin/xray","NATIVE-XRAY")
    writeFixture(t,root,"sbin/xkeen","NATIVE-XKEEN")
    writeFixture(t,root,"etc/init.d/S05xkeen","# native init")
    writeFixture(t,root,"etc/xkeen/xkeen.json",`{"credential":"SECRET-XKEEN-DO-NOT-SHOW"}`)
    writeFixture(t,root,"etc/xray/configs/03_dns.json",`{
        // Native Xray DNS
        "dns":{"servers":["https://DNS-SECRET-DO-NOT-SHOW/dns-query",{"address":"1.1.1.1"}],"queryStrategy":"UseIPv4"}
    }`)
    writeFixture(t,root,"etc/xray/configs/05_routing.json",`{"routing":{"rules":[{"domain":["private.example"]},{}],"domainStrategy":"AsIs"}}`)
    writeFixture(t,root,"etc/xray/dat/geosite.dat","GEO-SENSITIVE-CONTENT")
    writeFixture(t,root,"etc/xray/dat/geoip.dat","GEO-SENSITIVE-COUNTRIES")
    writeFixture(t,root,"var/spool/cron/crontabs/root",
        "# /opt/sbin/xkeen -ug\n"+
        "*/5 * * * * /opt/sbin/xkeen -sbt\n"+
        "20 3 * * 1 /opt/sbin/xkeen -ug\n"+
        "* * * * * /opt/bin/legacy --token=SECRET-CRON-DO-NOT-SHOW\n")
    writeFixture(t,proc,"123/comm","xray\n")
    state:=nativeSystemStatus(root,proc)
    if state.Evidence!="READ_ONLY" || state.VPN!="NOT_VERIFIED" ||
        state.XrayProcess!="PROCESS_DETECTED" ||
        state.XKeenInit!="PRESENT" || state.XKeenConfig!="PRESENT" {
        t.Fatalf("native status not accurately scoped: %+v",state)
    }
    if state.DNS.State!="CONFIG_ONLY" || state.DNS.Count!=2 || state.DNS.Mode!="UseIPv4" {
        t.Fatalf("official dns summary invalid: %+v",state.DNS)
    }
    if state.Routing.State!="CONFIG_ONLY" || state.Routing.Count!=2 || state.Routing.Mode!="AsIs" {
        t.Fatalf("official routing summary invalid: %+v",state.Routing)
    }
    if state.GeoData.State!="PRESENT" || state.GeoData.Files!=2 ||
        state.XKeenGeoCron!="CONFIGURED" || state.XKeenBalancerCron!="CONFIGURED" {
        t.Fatalf("native GeoData/cron presence not represented correctly: %+v",state)
    }
    h:=handlersWithProc(root,proc)
    rec:=httptest.NewRecorder()
    h.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/api/native/system",nil))
    if rec.Code!=http.StatusOK {t.Fatalf("GET native system returned %d",rec.Code)}
    var parsed nativeSystem
    if err:=json.Unmarshal(rec.Body.Bytes(),&parsed);err!=nil{t.Fatal(err)}
    if parsed.VPN!="NOT_VERIFIED"{t.Fatal("false green VPN runtime status")}
    for _,secret:=range []string{"SECRET-XKEEN-DO-NOT-SHOW","DNS-SECRET","SECRET-CRON","GEO-SENSITIVE","private.example"} {
        if bytes.Contains(rec.Body.Bytes(),[]byte(secret)){t.Fatalf("native system API leaked %q",secret)}
    }
}

func TestNativeSystemUnknownAndFailClosed(t *testing.T) {
    root:=t.TempDir()
    proc:=filepath.Join(t.TempDir(),"missing")
    state:=nativeSystemStatus(root,proc)
    if state.XrayProcess!="UNKNOWN" || state.VPN!="NOT_VERIFIED" ||
        state.DNS.State!="MISSING" || state.Routing.State!="MISSING" ||
        state.GeoData.State!="MISSING" || state.XKeenGeoCron!="NOT_CONFIGURED" {
        t.Fatalf("missing native stack must not appear healthy: %+v",state)
    }
    writeFixture(t,root,"etc/xray/configs/03_dns.json",`{"dns":{"servers":[],"queryStrategy":"password=secret"}}`)
    if got:=dnsSummary(root);got.Mode!="not-specified" {t.Fatalf("unknown DNS strategy leaked: %+v",got)}
    writeFixture(t,root,"etc/xray/configs/03_dns.json",`{"dns":"bad-structure"}`)
    if got:=dnsSummary(root);got.State!="UNKNOWN" {t.Fatalf("bad native DNS should be unknown: %+v",got)}
    writeFixture(t,root,"var/spool/cron/crontabs/root","# /opt/sbin/xkeen -ug\n* * * * * /opt/bin/something-else\n")
    if a,b:=nativeCron(root);a!="NOT_CONFIGURED"||b!="NOT_CONFIGURED" {t.Fatalf("comments counted as active cron: %s %s",a,b)}
}

func TestGeoDataSymlinkDoesNotMasqueradeAsVerified(t *testing.T) {
    root:=t.TempDir()
    outside:=filepath.Join(t.TempDir(),"external.dat")
    if err:=os.WriteFile(outside,[]byte("secret"),0600);err!=nil {t.Fatal(err)}
    if err:=os.MkdirAll(filepath.Join(root,"etc/xray/dat"),0700);err!=nil {t.Fatal(err)}
    if err:=os.Symlink(outside,filepath.Join(root,"etc/xray/dat/geosite.dat"));err!=nil{t.Skip(err)}
    if got:=geodataSummary(root);got.State!="UNKNOWN"||got.Files!=0 {
        t.Fatalf("symlink geodata should not be trusted: %+v",got)
    }
    if got:=xrayProcessEvidence(t.TempDir());got!="NOT_DETECTED" {t.Fatalf("unexpected Xray process %q",got)}
    if got:=xrayProcessEvidence(filepath.Join(t.TempDir(),"absent"));got!="UNKNOWN" {t.Fatalf("absent proc must be UNKNOWN: %q",got)}
}

func TestNativeSystemHTTPRejectsMutations(t *testing.T) {
    h:=handlersWithProc(t.TempDir(),t.TempDir())
    for _,method:=range []string{"POST","PUT","DELETE","PATCH"}{
        rec:=httptest.NewRecorder()
        h.ServeHTTP(rec,httptest.NewRequest(method,"/api/native/system",strings.NewReader("{}")))
        if rec.Code==http.StatusOK {
            t.Fatalf("%s unexpectedly accepted on native read-only API",method)
        }
    }
}
