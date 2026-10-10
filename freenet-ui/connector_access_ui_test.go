package main

import (
 "strings"
 "testing"
)

func TestConnectorAccessBrowserSecurityContract(t *testing.T){
 data,err:=webFS.ReadFile("web/index.html")
 if err!=nil{t.Fatal(err)}
 ui:=string(data)
 for _,required:=range []string{
  `id="connectorGrantState"`,
  `id="connectorStatusBtn"`,
  `id="connectorPairBtn"`,
  `id="connectorRevokeBtn"`,
  `id="connectorGrantSecret" hidden`,
  "function clearConnectorSecret()",
  "setTimeout(clearConnectorSecret,60000)",
  "if(page!=='admin')clearConnectorSecret()",
  "if(!(s&&s.authenticated))clearConnectorSecret()",
  "/api/admin/connector/grant",
  "/api/admin/connector/",
  "HTTP LAN выдача токена запрещена",
  "ChatGPT не подключён",
 }{
  if !strings.Contains(ui,required){t.Fatalf("missing connector UI safety contract %q",required)}
 }
 if strings.Contains(ui,"localStorage.setItem('connector")||strings.Contains(ui,"sessionStorage.setItem('connector"){
  t.Fatal("connector token must never persist in browser storage")
 }
}
