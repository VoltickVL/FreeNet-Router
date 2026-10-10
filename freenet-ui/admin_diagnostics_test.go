package main

import (
  "net/http"
  "net/http/httptest"
  "testing"
)

func TestAdminPublicDomainFailClosed(t *testing.T) {
  good:=[]string{"sg.api.io.mi.com","example.com","WWW.Example.COM","xn--e1afmkfd.xn--p1ai"}
  for _,v:=range good {if _,ok:=validateAdminPublicDomain(v);!ok {t.Errorf("valid public hostname rejected: %s",v)}}
  bad:=[]string{"","localhost","router","192.168.50.144","fe80::1","http://example.com","example.com:443","account.local","router.lan","service.internal","bad_domain.com","-invalid.com","evil..com","internal.arpa","device.keenetic","evil.test","example.com.","*.example.com","user@host.com"}
  for _,v:=range bad {if _,ok:=validateAdminPublicDomain(v);ok {t.Errorf("invalid hostname accepted: %s",v)}}
}

func TestAdminDNSRequiresAuthAndRefusesLocalHosts(t *testing.T) {
  a:=&app{sem:make(chan struct{},1)}
  mux:=http.NewServeMux()
  registerAdminDiagnostics(mux,a)
  unauth:=httptest.NewRecorder()
  mux.ServeHTTP(unauth,httptest.NewRequest("GET","http://router/api/admin/dns?host=example.com",nil))
  if unauth.Code==http.StatusNotFound || unauth.Code==http.StatusOK {t.Fatalf("missing authentication on Admin DNS: %d",unauth.Code)}
  invalid:=httptest.NewRecorder()
  a.handleAdminDNS(invalid,httptest.NewRequest("GET","http://router/api/admin/dns?host=127.0.0.1",nil))
  if invalid.Code!=http.StatusBadRequest {t.Fatalf("local DNS target was accepted: %d",invalid.Code)}
  if got:=invalid.Body.String(); got=="" || !strings.Contains(got,`"mutation":"NONE"`) {t.Fatalf("no safe error contract: %s",got)}
}
