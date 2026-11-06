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
    "time"
)

func newTestAuth(t *testing.T)(string,string,*adminAuth) {
    t.Helper()
    root:=t.TempDir()
    if err:=os.Mkdir(filepath.Join(root,"etc"),0700);err!=nil{t.Fatal(err)}
    code,err:=initializeAdminCredential(root)
    if err!=nil{t.Fatal(err)}
    if len(code)!=43 {t.Fatal("admin bootstrap token lacks entropy")}
    file:=filepath.Join(root,adminHashRelative)
    data,err:=os.ReadFile(file)
    if err!=nil {t.Fatal(err)}
    if bytes.Contains(data,[]byte(code)){t.Fatal("plaintext admin code stored")}
    stat,err:=os.Lstat(file)
    if err!=nil || stat.Mode().Perm()!=0600 {t.Fatal("credential permissions unsafe")}
    parent,err:=os.Lstat(filepath.Dir(file))
    if err!=nil || parent.Mode().Perm()!=0700 {t.Fatal("credential directory permissions unsafe")}
    auth,err:=loadAdminAuth(root)
    if err!=nil{t.Fatal(err)}
    if _,err:=initializeAdminCredential(root);err==nil{
        t.Fatal("existing admin token silently overwritten")
    }
    return root,code,auth
}

func TestHTTPSAdminSessionProtectsNativeControlCenter(t *testing.T){
    root,code,auth:=newTestAuth(t)
    server:=httptest.NewTLSServer(secureControlCenter(root,t.TempDir(),auth))
    defer server.Close()
    client:=server.Client()

    req,err:=http.NewRequest(http.MethodGet,server.URL+"/api/overview",nil)
    if err!=nil{t.Fatal(err)}
    unauth,err:=client.Do(req)
    if err!=nil{t.Fatal(err)}
    unauth.Body.Close()
    if unauth.StatusCode!=http.StatusUnauthorized {t.Fatal("native API leaked before login")}
    loginPage,err:=client.Get(server.URL+"/")
    if err!=nil{t.Fatal(err)}
    buf:=new(bytes.Buffer)
    _,_=buf.ReadFrom(loginPage.Body)
    loginPage.Body.Close()
    if loginPage.StatusCode!=http.StatusOK ||
        !strings.Contains(buf.String(),"Код администратора") ||
        strings.Contains(buf.String(),code){
        t.Fatal("login page failed or credential was leaked")
    }

    payload,err:=json.Marshal(loginRequest{Code:code})
    if err!=nil{t.Fatal(err)}
    loginReq,err:=http.NewRequest(http.MethodPost,server.URL+"/api/auth/login",bytes.NewReader(payload))
    if err!=nil{t.Fatal(err)}
    loginReq.Header.Set("Origin",server.URL)
    loginReq.Header.Set("Content-Type","application/json")
    loginReq.Header.Set("X-FreeNet-Action","login")
    login,err:=client.Do(loginReq)
    if err!=nil{t.Fatal(err)}
    login.Body.Close()
    if login.StatusCode!=http.StatusNoContent {t.Fatalf("HTTPS admin login failed: %d",login.StatusCode)}
    cookies:=login.Cookies()
    if len(cookies)!=1 || cookies[0].Name!=adminCookieName ||
        !cookies[0].Secure || !cookies[0].HttpOnly ||
        cookies[0].SameSite!=http.SameSiteStrictMode ||
        cookies[0].Path!="/" {
        t.Fatal("session cookie missing Secure+HttpOnly+SameSite Strict")
    }
    if strings.Contains(login.Header.Get("Set-Cookie"),code){
        t.Fatal("bootstrap secret echoed in session cookie")
    }

    verified,err:=http.NewRequest(http.MethodGet,server.URL+"/api/overview",nil)
    if err!=nil{t.Fatal(err)}
    verified.AddCookie(cookies[0])
    got,err:=client.Do(verified)
    if err!=nil{t.Fatal(err)}
    got.Body.Close()
    if got.StatusCode!=http.StatusOK {
        t.Fatalf("valid session denied native API: %d",got.StatusCode)
    }
    statusReq,err:=http.NewRequest(http.MethodGet,server.URL+"/api/auth/status",nil)
    if err!=nil{t.Fatal(err)}
    statusReq.AddCookie(cookies[0])
    status,err:=client.Do(statusReq)
    if err!=nil{t.Fatal(err)}
    statusBody:=new(bytes.Buffer)
    _,_=statusBody.ReadFrom(status.Body)
    status.Body.Close()
    if status.StatusCode!=http.StatusOK || !strings.Contains(statusBody.String(),`"authenticated":true`){
        t.Fatal("session status failed")
    }

    // Existing subscription preview remains protected by the same HTTPS session.
    subReq,err:=http.NewRequest(http.MethodPost,server.URL+"/api/subscription/preview",
        strings.NewReader(`{"url":"https://127.0.0.1/sub"}`))
    if err!=nil{t.Fatal(err)}
    subReq.AddCookie(cookies[0])
    subReq.Header.Set("Origin",server.URL)
    subReq.Header.Set("Content-Type","application/json")
    subReq.Header.Set("X-FreeNet-Action","preview-subscription")
    sub,err:=client.Do(subReq)
    if err!=nil{t.Fatal(err)}
    sub.Body.Close()
    if sub.StatusCode!=http.StatusBadRequest {
        t.Fatalf("logged-in subscription route unexpectedly bypassed SSRF: %d",sub.StatusCode)
    }

    logoutReq,err:=http.NewRequest(http.MethodPost,server.URL+"/api/auth/logout",nil)
    if err!=nil{t.Fatal(err)}
    logoutReq.AddCookie(cookies[0])
    logoutReq.Header.Set("Origin",server.URL)
    logoutReq.Header.Set("X-FreeNet-Action","logout")
    logout,err:=client.Do(logoutReq)
    if err!=nil{t.Fatal(err)}
    logout.Body.Close()
    if logout.StatusCode!=http.StatusNoContent {t.Fatalf("logout failed: %d",logout.StatusCode)}
    oldSession,err:=http.NewRequest(http.MethodGet,server.URL+"/api/overview",nil)
    if err!=nil{t.Fatal(err)}
    oldSession.AddCookie(cookies[0])
    oldResp,err:=client.Do(oldSession)
    if err!=nil{t.Fatal(err)}
    oldResp.Body.Close()
    if oldResp.StatusCode!=http.StatusUnauthorized {t.Fatal("logout did not revoke session")}
}

func TestAuthFailsClosedForCrossOriginAndWithoutTLS(t *testing.T){
    root,code,auth:=newTestAuth(t)
    handler:=secureControlCenter(root,t.TempDir(),auth)
    plain:=httptest.NewRecorder()
    handler.ServeHTTP(plain,httptest.NewRequest(http.MethodGet,"http://127.0.0.1:1009/",nil))
    if plain.Code!=http.StatusForbidden {t.Fatal("HTTPS mode accepted plaintext HTTP")}
    server:=httptest.NewTLSServer(handler)
    defer server.Close()
    client:=server.Client()
    makeLogin:=func(origin,action string)*http.Response{
        req,err:=http.NewRequest(http.MethodPost,server.URL+"/api/auth/login",
            strings.NewReader(`{"code":"`+code+`"}`))
        if err!=nil{t.Fatal(err)}
        req.Header.Set("Origin",origin)
        req.Header.Set("X-FreeNet-Action",action)
        req.Header.Set("Content-Type","application/json")
        response,err:=client.Do(req)
        if err!=nil{t.Fatal(err)}
        response.Body.Close()
        return response
    }
    if res:=makeLogin("https://evil.example","login");res.StatusCode!=http.StatusForbidden {
        t.Fatal("cross-site login accepted")
    }
    if res:=makeLogin(server.URL,"");res.StatusCode!=http.StatusForbidden {
        t.Fatal("login without custom CSRF header accepted")
    }
    if res:=makeLogin(server.URL,"login");res.StatusCode!=http.StatusNoContent {
        t.Fatal("valid secure same-origin login failed")
    }
}

func TestAdminRateLimitAndUnsafeStorage(t *testing.T){
    root,code,auth:=newTestAuth(t)
    if !auth.validatePairingCode(code) || auth.validatePairingCode(strings.Repeat("x",43)){
        t.Fatal("pairing code comparison incorrect")
    }
    now:=time.Now()
    for j:=0;j<5;j++ {auth.failedLogin("192.168.50.100",now)}
    if auth.loginPermitted("192.168.50.100",now.Add(time.Second)){
        t.Fatal("failed login brute force not rate-limited")
    }
    if !auth.loginPermitted("192.168.50.101",now.Add(time.Second)){
        t.Fatal("unrelated admin IP erroneously blocked")
    }
    if !auth.loginPermitted("192.168.50.100",now.Add(61*time.Second)){
        t.Fatal("rate limit does not expire")
    }
    file:=filepath.Join(root,adminHashRelative)
    if err:=os.Chmod(file,0644);err!=nil{t.Fatal(err)}
    if _,err:=loadAdminAuth(root);err==nil{t.Fatal("unsafe 0644 auth file accepted")}
    if err:=os.Remove(file);err!=nil{t.Fatal(err)}
    external:=filepath.Join(t.TempDir(),"elsewhere")
    if err:=os.WriteFile(external,[]byte(strings.Repeat("0",64)),0600);err!=nil{t.Fatal(err)}
    if err:=os.Symlink(external,file);err!=nil{t.Skip(err)}
    if _,err:=loadAdminAuth(root);err==nil{t.Fatal("symlink credential file accepted")}
}

func TestSecureListenerNeverUsesWildcardOrHostname(t *testing.T){
    for _,good:=range []string{"192.168.50.1:10443","127.0.0.1:10443","[::1]:10443"} {
        if !secureListenAddress(good){t.Fatalf("safe HTTPS address rejected: %s",good)}
    }
    for _,bad:=range []string{":10443","0.0.0.0:10443","[::]:10443",
        "router.local:10443","192.168.50.1:0","192.168.50.1:abc","192.168.50.1:65536"}{
        if secureListenAddress(bad){t.Fatalf("unsafe HTTPS listener accepted: %s",bad)}
    }
}
