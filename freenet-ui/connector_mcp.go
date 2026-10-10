package main

import (
    "bytes"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "mime"
    "net/http"
    "net/http/httptest"
    "net/url"
    "strconv"
    "strings"
)

const connectorMCPVersion = "2026-07-28"
const connectorMCPLegacyVersion = "2025-06-18"
const connectorMCPMaxBody = 8192

type connectorMCPRequest struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      json.RawMessage `json:"id"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

func connectorMCPReply(w http.ResponseWriter, code int, id json.RawMessage, result any, rpcErr any) {
    w.Header().Set("Cache-Control", "no-store")
    w.Header().Set("Content-Type", "application/json")
    if len(id) == 0 { id = json.RawMessage("null") }
    reply := map[string]any{"jsonrpc":"2.0", "id":id}
    if rpcErr != nil { reply["error"] = rpcErr } else { reply["result"] = result }
    w.WriteHeader(code)
    _ = json.NewEncoder(w).Encode(reply)
}

func connectorMCPError(w http.ResponseWriter, code int, id json.RawMessage, number int, message string) {
    connectorMCPReply(w, code, id, nil, map[string]any{"code":number, "message":message})
}

func connectorMCPVersionAllowed(v string) bool {
    return v == connectorMCPVersion || v == connectorMCPLegacyVersion
}

func connectorMCPTools() []map[string]any {
    empty := map[string]any{"type":"object", "properties":map[string]any{}, "additionalProperties":false}
    return []map[string]any{
        {
            "name":"get_status",
            "title":"Состояние FreeNet",
            "description":"Только чтение: версия, процессы и базовые признаки работоспособности. Не доказывает маршрут LAN.",
            "inputSchema":empty,
            "annotations":map[string]any{"readOnlyHint":true, "destructiveHint":false, "openWorldHint":false},
        },
        {
            "name":"get_component_health",
            "title":"Состояние компонентов",
            "description":"Только чтение: FreeNet, Xray, XKeen UI и DNS. Не запускает или перезапускает компоненты.",
            "inputSchema":empty,
            "annotations":map[string]any{"readOnlyHint":true, "destructiveHint":false, "openWorldHint":false},
        },
        {
            "name":"test_route",
            "title":"Расчёт правила маршрутизации",
            "description":"Только по сохранённому Xray конфигу. Возвращает CONFIG_ONLY или UNKNOWN, фактического LAN-трафика не наблюдает.",
            "inputSchema":map[string]any{
                "type":"object",
                "properties":map[string]any{
                    "host":map[string]any{"type":"string", "description":"FQDN либо IPv4/IPv6 назначения", "maxLength":253},
                    "client":map[string]any{"type":"string", "description":"Необязательный частный IP LAN-клиента", "maxLength":45},
                    "port":map[string]any{"type":"integer", "minimum":1, "maximum":65535},
                    "network":map[string]any{"type":"string", "enum":[]string{"tcp","udp"}},
                },
                "required":[]string{"host"},
                "additionalProperties":false,
            },
            "annotations":map[string]any{"readOnlyHint":true, "destructiveHint":false, "openWorldHint":false},
        },
        {
            "name":"get_recent_journal",
            "title":"Доступность безопасного журнала",
            "description":"Не выдаёт сырой журнал или секреты; возвращает available=false, пока очищенная проекция не готова.",
            "inputSchema":empty,
            "annotations":map[string]any{"readOnlyHint":true, "destructiveHint":false, "openWorldHint":false},
        },
    }
}

func connectorMCPArguments(name string, raw json.RawMessage) (url.Values, error) {
    args := map[string]json.RawMessage{}
    if len(raw) != 0 && string(raw) != "null" {
        if err := json.Unmarshal(raw, &args); err != nil || args == nil { return nil, errors.New("invalid tool arguments") }
    }
    values := url.Values{"tool":[]string{name}}
    if name != "test_route" && len(args) != 0 { return nil, errors.New("unexpected tool arguments") }
    for key, data := range args {
        switch key {
        case "host", "client", "network":
            var value string
            if err := json.Unmarshal(data, &value); err != nil { return nil, errors.New("invalid string tool argument") }
            limit := 253
            if key == "client" {limit = 45} else if key == "network" {limit = 3}
            if len(value) > limit {return nil, errors.New("tool argument is too long")}
            values.Set(key,value)
        case "port":
            var port int
            if err := json.Unmarshal(data, &port); err != nil || port < 1 || port > 65535 {
                return nil, errors.New("invalid port")
            }
            values.Set("port",strconv.Itoa(port))
        default:
            return nil, errors.New("unknown tool argument")
        }
    }
    if name == "test_route" && values.Get("host") == "" { return nil, errors.New("host is required") }
    return values, nil
}

func (a *app) connectorMCPCall(name string, args json.RawMessage) (any, error) {
    switch name {
    case "get_status","get_component_health","get_recent_journal","test_route":
    default:
        return nil, errors.New("unknown tool")
    }
    values, err := connectorMCPArguments(name,args)
    if err != nil { return nil, err }
    request := httptest.NewRequest(http.MethodGet,"/api/admin/connector/diagnostics?"+values.Encode(),nil)
    recorder := httptest.NewRecorder()
    a.handleConnectorDiagnostics(recorder, request)
    if recorder.Code != http.StatusOK {
        return nil, errors.New("diagnostic unavailable or invalid input")
    }
    // The underlying projection is an explicit whitelist. Never return
    // raw Xray configs, HTTP cookies, subscription URLs, or journal events.
    body := recorder.Body.Bytes()
    if len(body) > connectorMCPMaxBody { return nil, errors.New("diagnostic result exceeds limit") }
    var result map[string]any
    if err := json.Unmarshal(body, &result); err != nil || result["mutation"] != "NONE" {
        return nil, errors.New("read-only diagnostic contract violated")
    }
    return map[string]any{
        "content":[]map[string]any{{"type":"text","text":string(body)}},
        "structuredContent":result,
        "isError":false,
    }, nil
}

func (a *app) handleConnectorMCP(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Cache-Control", "no-store")
    if !a.authorizeConnectorMachine(w,r) { return }
    if r.Method != http.MethodPost {
        w.Header().Set("Allow","POST")
        connectorMCPError(w,http.StatusMethodNotAllowed,nil,-32600,"POST required")
        return
    }
    mediaType,_,err := mime.ParseMediaType(r.Header.Get("Content-Type"))
    if err != nil || mediaType != "application/json" {
        connectorMCPError(w,http.StatusUnsupportedMediaType,nil,-32600,"application/json required")
        return
    }
    accept := r.Header.Get("Accept")
    if !strings.Contains(accept,"application/json") || !strings.Contains(accept,"text/event-stream") {
        connectorMCPError(w,http.StatusNotAcceptable,nil,-32600,"MCP requires JSON and SSE accept types")
        return
    }
    hdrVersion := r.Header.Get("MCP-Protocol-Version")
    if hdrVersion != "" && !connectorMCPVersionAllowed(hdrVersion) {
        connectorMCPError(w,http.StatusBadRequest,nil,-32602,"unsupported MCP protocol version")
        return
    }
    raw,err := io.ReadAll(io.LimitReader(r.Body,connectorMCPMaxBody+1))
    if err != nil || len(raw)>connectorMCPMaxBody {
        connectorMCPError(w,http.StatusRequestEntityTooLarge,nil,-32600,"MCP request too large")
        return
    }
    decoder := json.NewDecoder(bytes.NewReader(raw))
    decoder.DisallowUnknownFields()
    var request connectorMCPRequest
    if err := decoder.Decode(&request); err != nil {
        connectorMCPError(w,http.StatusBadRequest,nil,-32700,"invalid JSON-RPC request")
        return
    }
    var extra any
    if err := decoder.Decode(&extra); err != io.EOF {
        connectorMCPError(w,http.StatusBadRequest,nil,-32700,"trailing JSON-RPC content")
        return
    }
    if request.JSONRPC != "2.0" || request.Method == "" || len(request.Method)>80 {
        connectorMCPError(w,http.StatusBadRequest,request.ID,-32600,"invalid JSON-RPC envelope")
        return
    }
    if headerMethod:=r.Header.Get("Mcp-Method"); headerMethod!="" && headerMethod!=request.Method {
        connectorMCPError(w,http.StatusBadRequest,request.ID,-32600,"MCP method header mismatch")
        return
    }
    if len(request.ID)==0 {
        if request.Method == "notifications/initialized" {
            w.WriteHeader(http.StatusAccepted)
            return
        }
        connectorMCPError(w,http.StatusBadRequest,nil,-32600,"request id required")
        return
    }
    if len(request.ID)>64 || (request.ID[0]!='"' && request.ID[0]!='-' && (request.ID[0]<'0' || request.ID[0]>'9')) {
        connectorMCPError(w,http.StatusBadRequest,nil,-32600,"invalid request id")
        return
    }
    if hdrVersion=="" && request.Method!="initialize" {
        connectorMCPError(w,http.StatusBadRequest,request.ID,-32602,"MCP protocol version header required")
        return
    }
    switch request.Method {
    case "initialize":
        var params struct{ ProtocolVersion string `json:"protocolVersion"` }
        if err:=json.Unmarshal(request.Params,&params);err!=nil || !connectorMCPVersionAllowed(params.ProtocolVersion) {
            connectorMCPError(w,http.StatusBadRequest,request.ID,-32602,"unsupported initialization protocol version")
            return
        }
        if hdrVersion!="" && hdrVersion!=params.ProtocolVersion {
            connectorMCPError(w,http.StatusBadRequest,request.ID,-32602,"protocol version mismatch")
            return
        }
        connectorMCPReply(w,http.StatusOK,request.ID,map[string]any{
            "protocolVersion":params.ProtocolVersion,
            "capabilities":map[string]any{"tools":map[string]any{"listChanged":false}},
            "serverInfo":map[string]any{"name":"freenet-router-readonly","version":"0.1.0"},
        },nil)
    case "tools/list":
        connectorMCPReply(w,http.StatusOK,request.ID,map[string]any{"tools":connectorMCPTools()},nil)
    case "tools/call":
        var params struct {
            Name string `json:"name"`
            Arguments json.RawMessage `json:"arguments"`
        }
        if err:=json.Unmarshal(request.Params,&params);err!=nil || len(params.Name)>64 {
            connectorMCPError(w,http.StatusBadRequest,request.ID,-32602,"invalid tool call")
            return
        }
        result,err:=a.connectorMCPCall(params.Name,params.Arguments)
        if err!=nil {
            // Fail closed; never reflect arbitrary values or paths.
            connectorMCPError(w,http.StatusBadRequest,request.ID,-32602,fmt.Sprintf("tool unavailable: %s",err.Error()))
            return
        }
        connectorMCPReply(w,http.StatusOK,request.ID,result,nil)
    default:
        connectorMCPError(w,http.StatusNotFound,request.ID,-32601,"MCP method not found")
    }
}
