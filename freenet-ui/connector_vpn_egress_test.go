package main

import (
 "bytes"
 "context"
 "encoding/json"
 "net"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

const vpnFakeRealityProfile=`{
 // Existing Xray config may contain comments and trailing commas.
 "outbounds": [
  {"tag":"direct","protocol":"freedom"},
  {
   "tag":"vless-reality",
   "protocol":"vless",
   "settings":{"vnext":[{"address":"vpn.example.test","port":443,"users":[{"id":"fake-test-uuid","encryption":"none"}]}]},
   "streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverName":"www.example.org","publicKey":"fake-test-publickey","shortId":"abcd"}},
  },
  {"tag":"block","protocol":"blackhole"}
 ]
}`

func TestConnectorVPNOnlyXrayConfigNeverFallsBackToDirect(t *testing.T){
 out,err:=connectorVPNOnlyConfig([]byte(vpnFakeRealityProfile))
 if err!=nil{t.Fatal(err)}
 if bytes.Contains(out,[]byte("fake-test-publickey"))==false{t.Fatal("VLESS Reality settings not preserved")}
 var parsed struct{
  Inbounds []struct{Listen string `json:"listen"`; Port int `json:"port"`;Protocol string `json:"protocol"`} `json:"inbounds"`
  Outbounds []struct{Tag string `json:"tag"`;Protocol string `json:"protocol"`;StreamSettings struct{Sockopt struct{Mark int `json:"mark"`} `json:"sockopt"`} `json:"streamSettings"`} `json:"outbounds"`
  Routing struct{Rules []struct{OutboundTag string `json:"outboundTag"`} `json:"rules"`} `json:"routing"`
 }
 if err=json.Unmarshal(out,&parsed);err!=nil{t.Fatal(err)}
 if len(parsed.Inbounds)!=1||parsed.Inbounds[0].Listen!="127.0.0.1"||parsed.Inbounds[0].Port!=12032||parsed.Inbounds[0].Protocol!="http" {t.Fatalf("public or non-http inbound: %+v",parsed.Inbounds)}
 if len(parsed.Outbounds)!=1||parsed.Outbounds[0].Tag!="vless-reality"||parsed.Outbounds[0].Protocol!="vless"||
   parsed.Outbounds[0].StreamSettings.Sockopt.Mark!=255 {
  t.Fatalf("direct fallback or loop-prone Xray config: %+v",parsed.Outbounds)
 }
 if len(parsed.Routing.Rules)!=1||parsed.Routing.Rules[0].OutboundTag!="vless-reality"{t.Fatal("sidecar did not require VLESS route")}
 if bytes.Contains(out,[]byte(`"protocol":"freedom"`))||bytes.Contains(out,[]byte(`"protocol":"blackhole"`)){t.Fatal("sidecar contains an alternate outbound")}
}

func TestConnectorVPNOnlyRejectsUnknownOutboundAndWeakSources(t *testing.T){
 cases:=[]string{
  "", "not-json", `{"outbounds":[]}`,
  `{"outbounds":[{"tag":"vless-reality","protocol":"freedom"}]}`,
  `{"outbounds":[{"tag":"vless-reality","protocol":"vless"}]}`,
  `{"outbounds":[{"tag":"vless-reality","protocol":"vless"},{"tag":"vless-reality","protocol":"vless"}]}`,
  strings.ReplaceAll(vpnFakeRealityProfile,`"security":"reality"`,`"security":"tls"`),
  strings.ReplaceAll(vpnFakeRealityProfile,`"protocol":"vless"`,`"protocol":"socks"`),
  strings.ReplaceAll(vpnFakeRealityProfile,`"port":443`,`"port":0`),
 }
 for i,s:=range cases{
  if _,err:=connectorVPNOnlyConfig([]byte(s));err==nil{
   t.Fatalf("unsafe VPN route source accepted: case %d",i)
  }
 }
}

func TestConnectorVPNOnlyRejectsSourceSymlinkAndBoundedFiles(t *testing.T){
 dir:=t.TempDir()
 dest:=filepath.Join(dir,"outbounds.json")
 if err:=os.WriteFile(dest,[]byte(vpnFakeRealityProfile),0600);err!=nil{t.Fatal(err)}
 result,err:=connectorReadVPNSource(dest)
 if err!=nil||len(result)==0{t.Fatal("safe source unreadable",err)}
 symlink:=filepath.Join(dir,"linked.json")
 if err:=os.Symlink(dest,symlink);err!=nil{t.Fatal(err)}
 if _,err:=connectorReadVPNSource(symlink);err==nil{t.Fatal("symbolic link source accepted")}
 if err:=os.WriteFile(dest,[]byte(strings.Repeat("x",connectorVPNMaxSource+1)),0600);err!=nil{t.Fatal(err)}
 if _,err:=connectorReadVPNSource(dest);err==nil{t.Fatal("oversized source accepted")}
}

func TestConnectorVPNOnlySavedKeyMigrationLeavesSecretsUntouched(t *testing.T){
 a,_:=connectorFixture(t)
 a.cfg.Listen="192.168.50.1:1001"
 _=connectorTestTransport(t,a)
 profile,err:=a.readTunnelProfile();if err!=nil{t.Fatal(err)}
 old,err:=a.connectorTunnelLegacyYAML(profile.TunnelID);if err!=nil{t.Fatal(err)}
 expected,err:=a.connectorTunnelYAML(profile.TunnelID);if err!=nil{t.Fatal(err)}
 if !strings.Contains(expected,"http_proxy: "+connectorVPNProxyURL){t.Fatal("explicit OpenAI proxy absent")}
 if strings.Contains(old,"http_proxy:"){t.Fatal("legacy fixture already proxied")}
 if err:=atomicWrite(a.connectorTunnelConfigPath(),[]byte(old),0600);err!=nil{t.Fatal(err)}
 priorKey,err:=os.ReadFile(a.connectorTransportKeyPath());if err!=nil{t.Fatal(err)}
 priorMachine,err:=os.ReadFile(a.connectorTransportHeaderPath());if err!=nil{t.Fatal(err)}
 if !a.connectorTransportConfigured(){t.Fatal("legacy credentials not recognized for migration")}
 if err:=a.connectorVPNUpgradeYAMLLocked(profile.TunnelID);err!=nil{t.Fatal(err)}
 cfg,err:=os.ReadFile(a.connectorTunnelConfigPath());if err!=nil||string(cfg)!=expected{t.Fatal("VPN proxy migration failed")}
 if !a.connectorTransportConfigured(){t.Fatal("VPN credentials not configured after migration")}
 if err:=a.connectorVPNUpgradeYAMLLocked(profile.TunnelID);err!=nil{t.Fatal("idempotent migration failed",err)}
 key,_:=os.ReadFile(a.connectorTransportKeyPath())
 machine,_:=os.ReadFile(a.connectorTransportHeaderPath())
 if !bytes.Equal(key,priorKey)||!bytes.Equal(machine,priorMachine){t.Fatal("existing credentials unexpectedly rotated")}
 if err:=os.WriteFile(a.connectorTunnelConfigPath(),[]byte("UNTRUSTED"),0600);err!=nil{t.Fatal(err)}
 if err:=a.connectorVPNUpgradeYAMLLocked(profile.TunnelID);err==nil{t.Fatal("unknown config overwritten")}
 observed,_:=os.ReadFile(a.connectorTunnelConfigPath())
 if string(observed)!="UNTRUSTED"{t.Fatal("unknown config mutated")}
}

func TestConnectorVPNProbeOnlyTrustsLocalHTTPConnect200(t *testing.T){
 listener,err:=net.Listen("tcp","127.0.0.1:0");if err!=nil{t.Fatal(err)}
 defer listener.Close()
 finish:=make(chan string,1)
 go func(){
  conn,err:=listener.Accept();if err!=nil{return}
  defer conn.Close()
  buf:=make([]byte,512);n,_:=conn.Read(buf)
  finish<-string(buf[:n])
  _,_=conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
 }()
 ctx:=context.Background()
 if err:=connectorVPNProbe(ctx,listener.Addr().String());err!=nil{t.Fatalf("HTTP CONNECT should have succeeded: %v",err)}
 request:=<-finish
 if !strings.HasPrefix(request,"CONNECT api.openai.com:443 HTTP/1.1")||strings.Contains(request,"Bearer")||strings.Contains(request,"sk-"){t.Fatal("unsafe or wrong CONNECT request")}
 if err:=connectorVPNProbe(ctx,"127.0.0.1:0");err==nil{t.Fatal("unavailable proxy accepted")}
}

func TestConnectorVPNOnlyOverridesHostEnvironment(t *testing.T){
 source:=[]string{
  "PATH=/opt/bin:/usr/bin",
  "CONTROL_PLANE_HTTP_PROXY=http://untrusted.invalid:1000",
  "CONTROL_PLANE_BASE_URL=https://direct.invalid",
  "TUNNEL_CLIENT_HTTP_PROXY=",
  "HTTP_PROXY=http://direct.invalid",
  "HTTPS_PROXY=http://direct.invalid",
  "ALL_PROXY=socks5://direct.invalid",
  "NO_PROXY=api.openai.com",
  "MCP_HTTP_PROXY=http://untrusted.invalid",
  "OPENAI_ADMIN_KEY=never-inherit",
  "XRAY_LOCATION_CONFDIR=/opt/etc/xray/configs",
 }
 got:=connectorVPNClientEnv(source)
 if len(got)!=2 || got[0]!=source[0] || got[1]!=source[len(source)-1] {
  t.Fatalf("client inherited dangerous API/proxy environment: %v",got)
 }
 xray:=connectorVPNXrayEnv(source)
 if strings.Contains(strings.Join(xray,"|"),"XRAY_LOCATION_CONFDIR="){t.Fatal("sidecar inherited active Xray confdir")}
 if !strings.Contains(strings.Join(xray,"|"),isolatedXrayProbeFlag){t.Fatal("Xray sidecar not marked isolated")}
}

func TestConnectorVPNConfigScopedToControlPlaneOnly(t *testing.T){
 a,_:=connectorFixture(t)
 a.cfg.Listen="192.168.50.1:1001"
 yaml,err:=a.connectorTunnelYAML("tunnel_"+strings.Repeat("a",32))
 if err!=nil{t.Fatal(err)}
 if !strings.Contains(yaml,"control_plane:\n  http_proxy: http://127.0.0.1:12032\n"){t.Fatal("control-plane does not have a dedicated VPN proxy")}
 if strings.Contains(yaml,"\nhttp_proxy:")||strings.Contains(yaml,"mcp:\n  http_proxy:"){t.Fatal("global proxy would also redirect loopback MCP")}
 if !strings.Contains(yaml,"http://127.0.0.1:1001/mcp"){t.Fatal("MCP must remain router-local")}
}
