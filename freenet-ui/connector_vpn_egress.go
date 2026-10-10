package main

import (
 "bufio"
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "syscall"
 "time"
)

const connectorVPNProxyAddr = "127.0.0.1:12032"
const connectorVPNProxyURL = "http://" + connectorVPNProxyAddr
const connectorVPNSourceFile = "04_outbounds.json"
const connectorVPNMaxSource = 512 << 10

// This is a separate, loopback-only Xray process. It uses exactly one existing
// VLESS outbound. No direct/freedom/blackhole alternate outbound is present,
// and the live Xray config, routing tables and firewall are never modified.
type connectorVPNOnlyState struct {
 Configured bool `json:"configured"`
 Active bool `json:"active"`
 Route string `json:"route"`
 ErrorCode string `json:"error_code,omitempty"`
}

func (a *app) connectorVPNProxyConfigPath() string {
 return filepath.Join(a.connectorTunnelDir(),"vpn-egress.json")
}

func connectorStripJSONC(raw []byte) ([]byte,error) {
 if len(raw)==0 || len(raw)>connectorVPNMaxSource{return nil,errors.New("unsupported source size")}
 out:=make([]byte,0,len(raw))
 quoted,escaped,line,block:=false,false,false,false
 for i:=0;i<len(raw);i++{
  c:=raw[i]
  if line{
   if c=='\n' {line=false;out=append(out,c)} else {out=append(out,' ')}
   continue
  }
  if block{
   if c=='*' && i+1<len(raw) && raw[i+1]=='/' {out=append(out,' ',' ');i++;block=false;continue}
   if c=='\n' {out=append(out,c)} else {out=append(out,' ')}
   continue
  }
  if quoted {
   out=append(out,c)
   if escaped {escaped=false} else if c=='\\' {escaped=true} else if c=='"' {quoted=false}
   continue
  }
  if c=='"' {quoted=true;out=append(out,c);continue}
  if c=='/' && i+1<len(raw) && raw[i+1]=='/' {line=true;out=append(out,' ',' ');i++;continue}
  if c=='/' && i+1<len(raw) && raw[i+1]=='*' {block=true;out=append(out,' ',' ');i++;continue}
  out=append(out,c)
 }
 if quoted||block{return nil,errors.New("invalid comments")}
 // Xray supports trailing commas in JSONC. Remove commas preceding closing
 // brackets while respecting strings; a strict JSON parser validates the rest.
 result:=make([]byte,0,len(out));quoted=false;escaped=false
 for i:=0;i<len(out);i++{
  c:=out[i]
  if quoted{
   result=append(result,c)
   if escaped {escaped=false} else if c=='\\' {escaped=true} else if c=='"' {quoted=false}
   continue
  }
  if c=='"' {quoted=true;result=append(result,c);continue}
  if c==','{
   j:=i+1
   for j<len(out) && (out[j]==' '||out[j]=='\r'||out[j]=='\n'||out[j]=='\t'){j++}
   if j<len(out) && (out[j]=='}'||out[j]==']'){result=append(result,' ');continue}
  }
  result=append(result,c)
 }
 return result,nil
}

func connectorReadVPNSource(path string)([]byte,error){
 st,err:=os.Lstat(path)
 if err!=nil||!st.Mode().IsRegular()||st.Size()<1||st.Size()>connectorVPNMaxSource {
  return nil,errors.New("VPN_SOURCE_UNKNOWN")
 }
 file,err:=os.Open(path);if err!=nil{return nil,errors.New("VPN_SOURCE_UNREADABLE")};defer file.Close()
 fst,err:=file.Stat()
 if err!=nil||!fst.Mode().IsRegular()||fst.Size()!=st.Size(){return nil,errors.New("VPN_SOURCE_UNSAFE")}
 data,err:=io.ReadAll(io.LimitReader(file,connectorVPNMaxSource+1))
 if err!=nil||len(data)==0||len(data)>connectorVPNMaxSource{return nil,errors.New("VPN_SOURCE_UNREADABLE")}
 return data,nil
}

// Only a *single* configured VLESS Reality outbound can be used. Never use
// an arbitrary server, user-supplied proxy, or router default DIRECT route.
func connectorVPNOnlyConfig(source []byte)([]byte,error){
 sanitized,err:=connectorStripJSONC(source)
 if err!=nil{return nil,errors.New("VPN_SOURCE_INVALID")}
 var parsed struct{Outbounds []json.RawMessage `json:"outbounds"`}
 if err=json.Unmarshal(sanitized,&parsed);err!=nil||parsed.Outbounds==nil{
  return nil,errors.New("VPN_SOURCE_INVALID")
 }
 var chosen map[string]json.RawMessage
 count:=0
 for _,raw:=range parsed.Outbounds{
  var basics struct{Tag string `json:"tag"`; Protocol string `json:"protocol"`}
  if json.Unmarshal(raw,&basics)!=nil{return nil,errors.New("VPN_OUTBOUND_UNKNOWN")}
  if basics.Tag!="vless-reality"{continue}
  count++
  if basics.Protocol!="vless"{return nil,errors.New("VPN_OUTBOUND_UNSUPPORTED")}
  if json.Unmarshal(raw,&chosen)!=nil{return nil,errors.New("VPN_OUTBOUND_INVALID")}
 }
 if count!=1||chosen==nil{return nil,errors.New("VPN_OUTBOUND_UNKNOWN")}
 var settings struct{Vnext []struct{Address string `json:"address"`;Port int `json:"port"`;Users []json.RawMessage `json:"users"`} `json:"vnext"`}
 if json.Unmarshal(chosen["settings"],&settings)!=nil||len(settings.Vnext)!=1||settings.Vnext[0].Address==""||
   settings.Vnext[0].Port<1||settings.Vnext[0].Port>65535||len(settings.Vnext[0].Users)!=1{
  return nil,errors.New("VPN_PROFILE_UNVERIFIED")
 }
 var stream map[string]json.RawMessage
 if json.Unmarshal(chosen["streamSettings"],&stream)!=nil||stream==nil{return nil,errors.New("VPN_TRANSPORT_UNVERIFIED")}
 var security string
 if json.Unmarshal(stream["security"],&security)!=nil||security!="reality" {return nil,errors.New("VPN_REALITY_REQUIRED")}
 // Prevent the Xray sidecar's connection *to the VPN server* from being
 // intercepted recursively by XKeen OUTPUT rules. This does not allow a
 // direct OpenAI route; only the VPN VLESS outbound exists.
 var sockopt map[string]json.RawMessage
 if len(stream["sockopt"])>0 && json.Unmarshal(stream["sockopt"],&sockopt)!=nil{return nil,errors.New("VPN_SOCKOPT_UNKNOWN")}
 if sockopt==nil{sockopt=make(map[string]json.RawMessage)}
 sockopt["mark"]=json.RawMessage("255")
 stream["sockopt"],err=json.Marshal(sockopt);if err!=nil{return nil,errors.New("VPN_SOCKOPT_INVALID")}
 chosen["streamSettings"],err=json.Marshal(stream);if err!=nil{return nil,errors.New("VPN_CONFIG_INVALID")}
 // Deliberately no "direct" outbound and no fallback for failed VPN.
 config:=map[string]any{
  "log":map[string]any{"loglevel":"warning"},
  "inbounds":[]any{map[string]any{
    "tag":"freenet-openai-vpn","listen":"127.0.0.1","port":12032,
    "protocol":"http","settings":map[string]any{"timeout":10},
  }},
  "outbounds":[]any{chosen},
  "routing":map[string]any{"domainStrategy":"AsIs","rules":[]any{
    map[string]any{"type":"field","inboundTag":[]string{"freenet-openai-vpn"},"outboundTag":"vless-reality"},
  }},
 }
 return json.Marshal(config)
}

func connectorVPNProbe(ctx context.Context, addr string) error {
 d:=net.Dialer{Timeout:2*time.Second}
 conn,err:=d.DialContext(ctx,"tcp",addr)
 if err!=nil{return errors.New("VPN_PROXY_UNREACHABLE")}
 defer conn.Close()
 _=conn.SetDeadline(time.Now().Add(8*time.Second))
 // CONNECT succeeds only after Xray opens the upstream socket through its
 // sole VLESS Reality outbound. No API key or identifying token is sent.
 if _,err=io.WriteString(conn,"CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\nProxy-Connection: close\r\n\r\n");err!=nil{
  return errors.New("VPN_PROXY_CONNECT_FAILED")
 }
 resp,err:=http.ReadResponse(bufio.NewReader(conn),&http.Request{Method:http.MethodConnect})
 if err!=nil{return errors.New("VPN_PROXY_CONNECT_FAILED")}
 defer resp.Body.Close()
 if resp.StatusCode!=http.StatusOK{return errors.New("VPN_PROXY_CONNECT_REJECTED")}
 return nil
}

func (a *app) connectorVPNStopLocked() {
 if a.connectorVPNProxyCmd!=nil{
  cmd:=a.connectorVPNProxyCmd
  _=cmd.Process.Kill()
  _=cmd.Wait()
  a.connectorVPNProxyCmd=nil
 }
 // The generated config contains a copy of the active VPN Reality credentials.
 // Remove it on stop; a following start re-reads the current profile.
 _=os.Remove(a.connectorVPNProxyConfigPath())
}

func (a *app) connectorVPNStartLocked() error {
 if a.connectorVPNProxyCmd!=nil{return errors.New("VPN_PROXY_ALREADY_RUNNING")}
 if !a.liveXrayRunning(){return errors.New("VPN_XRAY_OFFLINE")}
 raw,err:=connectorReadVPNSource(filepath.Join(a.routingConfigDir(),connectorVPNSourceFile))
 if err!=nil{return err}
 cfg,err:=connectorVPNOnlyConfig(raw)
 if err!=nil{return err}
 if err:=a.connectorPrivateDir();err!=nil{return errors.New("VPN_PRIVATE_STORE_UNAVAILABLE")}
 target:=a.connectorVPNProxyConfigPath()
 if st,err:=os.Lstat(target);err==nil{
  if !st.Mode().IsRegular()||st.Mode().Perm()!=0600{return errors.New("VPN_CONFIG_UNSAFE")}
 }else if !os.IsNotExist(err){return errors.New("VPN_CONFIG_UNKNOWN")}
 if err:=atomicWrite(target,cfg,0600);err!=nil{return errors.New("VPN_CONFIG_WRITE_FAILED")}
 cleanup:=func(){_ =os.Remove(target)}
 ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
 defer cancel()
 valid:=exec.CommandContext(ctx,a.routingXrayBin(),"run","-test","-c",target)
 valid.Stdout=io.Discard;valid.Stderr=io.Discard
 if err:=valid.Run();err!=nil {cleanup();return errors.New("VPN_XRAY_CONFIG_INVALID")}
 // Refuse an occupied listener. Reusing an unknown service would bypass our
 // guarantee that the tunnel uses only the selected VLESS profile.
 ln,err:=net.Listen("tcp",connectorVPNProxyAddr)
 if err!=nil{cleanup();return errors.New("VPN_PROXY_PORT_OCCUPIED")}
 _=ln.Close()
 cmd:=exec.Command(a.routingXrayBin(),"run","-c",target)
 cmd.Stdin=nil;cmd.Stdout=io.Discard;cmd.Stderr=io.Discard
 cmd.SysProcAttr=&syscall.SysProcAttr{Pdeathsig:syscall.SIGTERM}
 if err:=cmd.Start();err!=nil{cleanup();return errors.New("VPN_PROXY_START_FAILED")}
 a.connectorVPNProxyCmd=cmd
 for attempt:=0;attempt<4;attempt++{
  probeCtx,done:=context.WithTimeout(context.Background(),9*time.Second)
  err=connectorVPNProbe(probeCtx,connectorVPNProxyAddr)
  done()
  if err==nil{return nil}
  if cmd.ProcessState!=nil{break}
  time.Sleep(400*time.Millisecond)
 }
 a.connectorVPNStopLocked()
 return errors.New("VPN_CONNECT_NOT_VERIFIED")
}

func connectorVPNYAMLProxyLine() string {
 return "http_proxy: "+connectorVPNProxyURL+"\n"
}
