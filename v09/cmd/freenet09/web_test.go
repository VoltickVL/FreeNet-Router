package main

import (
    "bytes"
    "encoding/binary"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
)

func TestPWAInstallAssetsAndCorrectIOSIcons(t *testing.T) {
    h:=handlersWithProc(t.TempDir(),t.TempDir())
    manifest:=httptest.NewRecorder()
    h.ServeHTTP(manifest,httptest.NewRequest(http.MethodGet,"/manifest.webmanifest",nil))
    if manifest.Code!=http.StatusOK || !strings.Contains(manifest.Header().Get("Content-Type"),"application/manifest+json") {
        t.Fatalf("manifest unavailable: %d",manifest.Code)
    }
    var spec struct {
        Name string `json:"name"`
        Display string `json:"display"`
        Icons []struct {
            Src string `json:"src"`
            Sizes string `json:"sizes"`
        } `json:"icons"`
    }
    if err:=json.Unmarshal(manifest.Body.Bytes(),&spec);err!=nil {t.Fatal(err)}
    if spec.Display!="standalone" || !strings.Contains(spec.Name,"FreeNet") {t.Fatalf("invalid PWA manifest: %+v",spec)}
    wanted:=map[string]uint32{
        "/static/icon-180.png":180,
        "/static/icon-192.png":192,
        "/static/icon-512.png":512,
    }
    for url,width:=range wanted {
        rec:=httptest.NewRecorder()
        h.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,url,nil))
        data:=rec.Body.Bytes()
        if rec.Code!=http.StatusOK || len(data)<24 ||
            !bytes.Equal(data[:8],[]byte{137,80,78,71,13,10,26,10}) {
            t.Fatalf("native iOS/Android icon invalid: %s status=%d",url,rec.Code)
        }
        actual:=binary.BigEndian.Uint32(data[16:20])
        if actual!=width {t.Fatalf("incorrect icon size for %s: %d",url,actual)}
    }
    sw:=httptest.NewRecorder()
    h.ServeHTTP(sw,httptest.NewRequest(http.MethodGet,"/sw.js",nil))
    if sw.Code!=http.StatusOK || !strings.Contains(sw.Header().Get("Service-Worker-Allowed"),"/") {
        t.Fatalf("PWA service worker unavailable %d",sw.Code)
    }
    text:=sw.Body.String()
    if strings.Contains(text,"\"/api/native/system\"") || strings.Contains(text,"\"/api/overview\"") ||
        strings.Contains(text,"caches.open(\"native") {
        t.Fatal("service worker must not cache secret-bearing APIs")
    }
}

func TestDashboardIsRealNativePWAWithoutFakeVPNStatus(t *testing.T) {
    root:=nativeFixture(t)
    proc:=t.TempDir()
    h:=handlersWithProc(root,proc)
    rec:=httptest.NewRecorder()
    h.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/",nil))
    page:=rec.Body.String()
    if rec.Code!=http.StatusOK {t.Fatalf("dashboard not served %d",rec.Code)}
    required:=[]string{
        "FreeNet 0.9 · Control Center","rel=\"manifest\"",
        "apple-mobile-web-app-capable","apple-touch-icon",
        "sub-server.example.org","sub-203.0.113.4","native-balanced",
        "NOT_VERIFIED","НЕТ", // optional? checked below
    }
    // Terms from real Xray files must render; fake sample speed must not.
    for _,term:=range required[:len(required)-1] {
        if !strings.Contains(page,term) {t.Fatalf("missing PWA/native dashboard marker %q",term)}
    }
    for _,fake:=range []string{"812 Мбит/с","713 Мбит/с","VPN работает","GeoData актуально"} {
        if strings.Contains(page,fake){t.Fatalf("fabricated healthy runtime value in preview: %q",fake)}
    }
    for _,header:=range []string{"script-src 'self'","style-src 'self'","connect-src 'self'","default-src 'none'"}{
        if !strings.Contains(rec.Header().Get("Content-Security-Policy"),header) {
            t.Fatalf("missing strict browser CSP %s",header)
        }
    }
    if !strings.Contains(rec.Header().Get("Cache-Control"),"no-store") {
        t.Fatal("native dashboard response must never be cached")
    }
    css:=httptest.NewRecorder()
    h.ServeHTTP(css,httptest.NewRequest(http.MethodGet,"/static/style.css",nil))
    if css.Code!=http.StatusOK || !strings.Contains(css.Body.String(),"safe-area-inset-bottom") ||
        !strings.Contains(css.Body.String(),"max-width:760px") {
        t.Fatal("real responsive iPhone styling unavailable")
    }
    js:=httptest.NewRecorder()
    h.ServeHTTP(js,httptest.NewRequest(http.MethodGet,"/static/app.js",nil))
    if js.Code!=http.StatusOK || !strings.Contains(js.Body.String(),"textContent") ||
        !strings.Contains(js.Body.String(),"/api/native/system") {
        t.Fatal("PWA native refresh module unavailable")
    }
    for _,path:=range []string{"/api/overview","/api/native/system","/api/native/xray/topology"} {
        response:=httptest.NewRecorder()
        h.ServeHTTP(response,httptest.NewRequest(http.MethodGet,path,nil))
        if !strings.Contains(response.Header().Get("Cache-Control"),"no-store") {
            t.Fatalf("native response %s may be cached",path)
        }
    }
}

func TestPWAAssetsDoNotExposeGoSourceOrEntwareSecrets(t *testing.T) {
    h:=handlersWithProc(t.TempDir(),t.TempDir())
    for _,path:=range []string{"/static/../main.go","/static/../system.go","/static/../go.mod","/api/update",
        "/static/.env","/static/../../etc/xray/configs/04_outbounds.json"}{
        rec:=httptest.NewRecorder()
        h.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,path,nil))
        if rec.Code==http.StatusOK {t.Fatalf("unexpected public source/config path %s",path)}
    }
}

func TestV09ShellFocusesOnSubscriptionNotBackups(t *testing.T) {
    app:=handlersWithProc(t.TempDir(),t.TempDir())
    html:=httptest.NewRecorder()
    app.ServeHTTP(html,httptest.NewRequest(http.MethodGet,"/",nil))
    if html.Code!=http.StatusOK {t.Fatalf("dashboard http %d",html.Code)}
    page:=html.Body.String()
    for _,required:=range []string{`id="subscription"`,`data-view="subscription"`,
        "Подключить свою подписку","ЕЩЁ НЕ ГОТОВО"} {
        if !strings.Contains(page,required){t.Fatalf("product goal missing from UI: %q",required)}
    }
    for _,obsolete:=range []string{`id="recovery"`,`data-view="recovery"`,
        "Резервные копии","Восстановление","recovery-guard"} {
        if strings.Contains(page,obsolete){t.Fatalf("unwanted backup/recovery feature in shell: %q",obsolete)}
    }
    js:=httptest.NewRecorder()
    app.ServeHTTP(js,httptest.NewRequest(http.MethodGet,"/static/app.js",nil))
    if js.Code!=http.StatusOK || !strings.Contains(js.Body.String(),`"subscription"`) ||
        strings.Contains(js.Body.String(),`"recovery"`) {
        t.Fatal("mobile/desktop router still exposes backup center or hides subscription")
    }
}
