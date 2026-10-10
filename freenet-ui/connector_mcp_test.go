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

func mcpTestRequest(t *testing.T, token string, body []byte) *http.Request {
    t.Helper()
    req:=httptest.NewRequest(http.MethodPost,"http://localhost:1001/mcp",bytes.NewReader(body))
    req.RemoteAddr="127.0.0.1:33030"
    req.Header.Set("Authorization","Bearer "+token)
    req.Header.Set("Content-Type","application/json")
    req.Header.Set("Accept","application/json, text/event-stream")
    req.Header.Set("MCP-Protocol-Version",connectorMCPVersion)
    return req
}

func mcpTestRPC(t *testing.T,a *app,token,method string,params any) (int,map[string]any,string) {
    t.Helper()
    body,err:=json.Marshal(map[string]any{"jsonrpc":"2.0","id":7,"method":method,"params":params})
    if err!=nil{t.Fatal(err)}
    w:=httptest.NewRecorder()
    a.handleConnectorMCP(w,mcpTestRequest(t,token,body))
    var decoded map[string]any
    if err:=json.Unmarshal(w.Body.Bytes(),&decoded);err!=nil{t.Fatalf("bad MCP JSON: %d %v (%s)",w.Code,err,w.Body.String())}
    return w.Code,decoded,w.Body.String()
}

func TestMCPReadOnlyDiscoveryAndRouteNoMutation(t *testing.T) {
    a,_:=connectorFixture(t)
    dir:=t.TempDir()
    t.Setenv("FREENET_ROUTING_CONFIG_DIR",dir)
    routing:=[]byte(`{"routing":{"rules":[{"type":"field","domain":["full:sg.api.io.mi.com"],"outboundTag":"direct"}]}}`)
    outbound:=[]byte(`{"outbounds":[{"tag":"direct","protocol":"freedom","settings":{"private":"LEAK-MCP-PRIVATE-SECRET"}}]}`)
    if err:=os.WriteFile(filepath.Join(dir,"05_routing.json"),routing,0600);err!=nil{t.Fatal(err)}
    if err:=os.WriteFile(filepath.Join(dir,"04_outbounds.json"),outbound,0600);err!=nil{t.Fatal(err)}
    token:=connectorPair(t,a)
    code,data,raw:=mcpTestRPC(t,a,token,"initialize",map[string]any{
        "protocolVersion":connectorMCPVersion,"clientInfo":map[string]any{"name":"test","version":"1"},
        "capabilities":map[string]any{},
    })
    if code!=200 || data["jsonrpc"]!="2.0" {t.Fatalf("initialize failed: %d %s",code,raw)}
    result:=data["result"].(map[string]any)
    if result["protocolVersion"]!=connectorMCPVersion {t.Fatal("wrong MCP version negotiation")}
    code,data,raw=mcpTestRPC(t,a,token,"tools/list",map[string]any{})
    if code!=200{t.Fatalf("tools/list failed: %d %s",code,raw)}
    list:=data["result"].(map[string]any)["tools"].([]any)
    if len(list)!=6{t.Fatalf("expected six allowlisted tools including access request: %d",len(list))}
    for _,item:=range list {
        tool:=item.(map[string]any)
        if tool["inputSchema"]==nil {t.Fatal("tool has no input schema")}
        if tool["name"]=="request_access" {
            if tool["annotations"].(map[string]any)["readOnlyHint"]!=false {t.Fatal("approval request falsely labeled read-only")}
        } else if tool["annotations"].(map[string]any)["readOnlyHint"]!=true {t.Fatal("diagnostic/status tool not labeled read-only")}
    }
    code,data,raw=mcpTestRPC(t,a,token,"tools/call",map[string]any{
        "name":"test_route",
        "arguments":map[string]any{"host":"sg.api.io.mi.com","client":"192.168.50.144","port":443,"network":"tcp"},
    })
    if code!=200 || strings.Contains(raw,"LEAK-MCP-PRIVATE-SECRET") || strings.Contains(raw,"vless://") {
        t.Fatalf("unsafe tool result: %d %s",code,raw)
    }
    toolResult:=data["result"].(map[string]any)
    structured:=toolResult["structuredContent"].(map[string]any)
    if structured["expected_action"]!="DIRECT" || structured["observed_route"]!="NOT_OBSERVED" ||
        structured["mutation"]!="NONE" || structured["runtime_route_observed"]!=false {
        t.Fatalf("MCP claimed live routing or mutation: %+v",structured)
    }
    code,data,_=mcpTestRPC(t,a,token,"tools/call",map[string]any{"name":"get_recent_journal","arguments":map[string]any{}})
    if code!=200 || data["result"].(map[string]any)["structuredContent"].(map[string]any)["available"]!=false {
        t.Fatal("MCP must not expose raw journal")
    }
    after,_:=os.ReadFile(filepath.Join(dir,"05_routing.json"))
    if !bytes.Equal(after,routing){t.Fatal("MCP mutated Xray routing config")}
}

func TestMCPUnknownMethodsAndToolsFailClosed(t *testing.T){
    a,_:=connectorFixture(t);token:=connectorPair(t,a)
    for _,method:=range []string{"system/exec","tools/update","resources/write","prompts/get"}{
        code,_,_:=mcpTestRPC(t,a,token,method,map[string]any{})
        if code!=http.StatusNotFound{t.Fatalf("dangerous MCP method %s permitted: %d",method,code)}
    }
    for _,tc:=range []struct{tool string;args map[string]any}{
        {"exec",map[string]any{}},
        {"test_route",map[string]any{"host":"sg.api.io.mi.com","command":"rm -rf /"}},
        {"test_route",map[string]any{"host":"sg.api.io.mi.com","port":99999}},
        {"test_route",map[string]any{"host":"sg.api.io.mi.com","port":"443"}},
        {"test_route",map[string]any{}},
        {"get_status",map[string]any{"host":"example.com"}},
    }{
        code,_,body:=mcpTestRPC(t,a,token,"tools/call",map[string]any{"name":tc.tool,"arguments":tc.args})
        if code!=400{t.Fatalf("MCP allowed unsafe args %s: %d %s",tc.tool,code,body)}
        if strings.Contains(body,"rm -rf") {t.Fatal("raw tool arguments disclosed")}
    }
}

func TestMCPAuthRevocationAndRequestValidation(t *testing.T){
    a,mux:=connectorFixture(t)
    _=mux
    unauth:=mcpTestRequest(t,"",[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
    w:=httptest.NewRecorder()
    a.handleConnectorMCP(w,unauth)
    if w.Code!=401{t.Fatalf("MCP accessible before pairing: %d",w.Code)}
    token:=connectorPair(t,a)
    req:=mcpTestRequest(t,token,[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
    req.RemoteAddr="192.0.2.3:50000"
    w=httptest.NewRecorder();a.handleConnectorMCP(w,req)
    if w.Code!=401{t.Fatal("non-loopback MCP was authorized")}
    req=mcpTestRequest(t,token,[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
    req.Header.Set("Cookie","freenet_session=bad")
    w=httptest.NewRecorder();a.handleConnectorMCP(w,req)
    if w.Code!=401{t.Fatal("cookie-bearing MCP request accepted")}
    req=mcpTestRequest(t,token,[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
    req.Header.Del("MCP-Protocol-Version")
    w=httptest.NewRecorder();a.handleConnectorMCP(w,req)
    if w.Code!=400{t.Fatal("MCP version header omission accepted")}
    req=mcpTestRequest(t,token,[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
    req.Header.Set("MCP-Protocol-Version","2099-01-01")
    w=httptest.NewRecorder();a.handleConnectorMCP(w,req)
    if w.Code!=400{t.Fatal("unsupported MCP protocol version accepted")}
    req=mcpTestRequest(t,token,[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
    req.Header.Set("Mcp-Method","system/exec")
    w=httptest.NewRecorder();a.handleConnectorMCP(w,req)
    if w.Code!=400{t.Fatal("mismatched MCP method accepted")}
    req=mcpTestRequest(t,token,[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
    req.Header.Set("Content-Type","text/plain")
    w=httptest.NewRecorder();a.handleConnectorMCP(w,req)
    if w.Code!=415{t.Fatal("MCP bad content-type accepted")}
    req=mcpTestRequest(t,token,bytes.Repeat([]byte("x"),connectorMCPMaxBody+1))
    w=httptest.NewRecorder();a.handleConnectorMCP(w,req)
    if w.Code!=413{t.Fatal("oversized MCP request accepted")}
    req=mcpTestRequest(t,token,[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"} {}`))
    w=httptest.NewRecorder();a.handleConnectorMCP(w,req)
    if w.Code!=400{t.Fatal("multiple JSON-RPC bodies accepted")}
    revoke:=httptest.NewRecorder()
    a.handleConnectorRevoke(revoke,connectorRequest("POST","/api/admin/connector/revoke",`{"confirm":true}`))
    if revoke.Code!=200{t.Fatal("revoke failed")}
    code,_,_:=mcpTestRPC(t,a,token,"tools/list",map[string]any{})
    if code!=401{t.Fatal("revoked token still accessed MCP")}
    token=connectorPair(t,a)
    grant,ok,err:=a.readConnectorGrant()
    if !ok||err!=nil{t.Fatal("grant state missing")}
    grant.IssuedAt=time.Now().Add(-16*time.Minute)
    grant.ExpiresAt=grant.IssuedAt.Add(connectorGrantTTL)
    if err:=a.saveConnectorGrant(grant);err!=nil{t.Fatal(err)}
    code,_,_=mcpTestRPC(t,a,token,"tools/list",map[string]any{})
    if code!=401{t.Fatal("expired token still accessed MCP")}
}

func TestMCPHTTPRouteIsProtected(t *testing.T){
    _,mux:=connectorFixture(t)
    w:=httptest.NewRecorder()
    mux.ServeHTTP(w,mcpTestRequest(t,"",[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
    if w.Code!=401{t.Fatalf("POST /mcp route is not guarded: %d",w.Code)}
    // No server-sent event listener or public GET endpoint exists.
    w=httptest.NewRecorder()
    mux.ServeHTTP(w,connectorRequest("GET","/mcp",""))
    if w.Code==200{t.Fatal("public SSE/GET path exposed")}
}
