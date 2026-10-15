package main

import (
  "context"
  "encoding/json"
  "errors"
  "net/url"
  "os"
  "path/filepath"
  "strings"
  "testing"
)

func TestBestCatalogUsesFreshDirectThenActiveVPNThenSourceBoundLKG(t *testing.T) {
  dir := t.TempDir()
  cache := filepath.Join(dir, "protected.lkg")
  t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_CACHE", cache)
  t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_SOURCE", cache+".source")
  sub := filepath.Join(dir, "subscription.url")
  source := "https://provider.example.invalid/private-token"
  if err:=os.WriteFile(sub, []byte(source+"\n"),0600); err!=nil {t.Fatal(err)}
  a:=testNetworkApp(t,"DNS_MODE=firmware\n")
  a.cfg.SubPath=sub

  originalDirect, originalVPN := directSubscriptionBodyFetch, activeVPNSubscriptionBodyFetch
  t.Cleanup(func(){directSubscriptionBodyFetch=originalDirect;activeVPNSubscriptionBodyFetch=originalVPN})
  directCalls, vpnCalls:=0,0
  directSubscriptionBodyFetch=func(_ context.Context,_ *url.URL)([]byte,error){directCalls++;return nil,errors.New("private-token failed")}
  activeVPNSubscriptionBodyFetch=func(_ *app,_ context.Context,_ *url.URL)([]byte,error){vpnCalls++;return nil,errors.New("private-token failed")}
  if _,_,_,_,err:=a.discoverBestServerCandidatesWithSource(context.Background());err==nil||strings.Contains(err.Error(),"private-token"){
    t.Fatalf("missing source-bound LKG did not STOP safely: %v",err)
  }

  directSubscriptionBodyFetch=func(_ context.Context,_ *url.URL)([]byte,error){directCalls++;return []byte(testSubscriptionPlain),nil}
  all,total,truncated,origin,err:=a.discoverBestServerCandidatesWithSource(context.Background())
  if err!=nil||total!=2||len(all)!=2||truncated||!origin.Fresh||origin.Source!="direct"||origin.UpdatedAt==""{
    t.Fatalf("fresh direct discovery failed: total=%d count=%d origin=%+v err=%v",total,len(all),origin,err)
  }
  if vpnCalls!=1 {t.Fatalf("direct success unexpectedly called VPN fallback: %d",vpnCalls)}

  rotated:=strings.Replace(testSubscriptionPlain,"203.0.113.10:443","203.0.113.77:8443",1)
  directSubscriptionBodyFetch=func(_ context.Context,_ *url.URL)([]byte,error){directCalls++;return nil,errors.New("direct unavailable")}
  activeVPNSubscriptionBodyFetch=func(_ *app,_ context.Context,_ *url.URL)([]byte,error){vpnCalls++;return []byte(rotated),nil}
  all,total,_,origin,err=a.discoverBestServerCandidatesWithSource(context.Background())
  if err!=nil||total!=2||!origin.Fresh||origin.Source!="active_vpn"||all[0].Profile.Address!="203.0.113.77"||all[0].Profile.Port!=8443{
    t.Fatalf("active VPN fallback did not refresh rotated servers: count=%d origin=%+v err=%v",total,origin,err)
  }
  local,err:=os.ReadFile(cache)
  if err!=nil||!strings.Contains(string(local),"203.0.113.77:8443") {t.Fatal("fresh active-VPN catalog did not replace protected LKG")}

  activeVPNSubscriptionBodyFetch=func(_ *app,_ context.Context,_ *url.URL)([]byte,error){vpnCalls++;return nil,errors.New("VPN unavailable")}
  all,total,_,origin,err=a.discoverBestServerCandidatesWithSource(context.Background())
  if err!=nil||total!=2||origin.Fresh||origin.Source!="protected_cache"||origin.UpdatedAt==""||all[0].Profile.Address!="203.0.113.77"{
    t.Fatalf("source-bound fallback should be usable but stale: total=%d origin=%+v err=%v",total,origin,err)
  }
  public,err:=json.Marshal(bestServerQualityResponse{Success:true,CatalogFresh:origin.Fresh,CatalogSource:origin.Source,CatalogUpdatedAt:origin.UpdatedAt})
  if err!=nil||strings.Contains(string(public),"private-token")||strings.Contains(string(public),"TEST-PBK")||strings.Contains(string(public),"vless://"){t.Fatalf("catalog public provenance leaked secrets: %s err=%v",public,err)}

  if err:=os.Chmod(cache,0644);err!=nil{t.Fatal(err)}
  if _,_,_,_,err:=a.discoverBestServerCandidatesWithSource(context.Background());err==nil{t.Fatal("world-readable protected cache must STOP")}
  if err:=os.Chmod(cache,0600);err!=nil{t.Fatal(err)}
  if err:=os.WriteFile(sub,[]byte("https://other.example.invalid/private-token\n"),0600);err!=nil{t.Fatal(err)}
  if _,_,_,_,err:=a.discoverBestServerCandidatesWithSource(context.Background());err==nil{t.Fatal("subscription rotation must never reuse another source's protected LKG")}
}

func TestBestCatalogCancellationDoesNotSilentlyFallBack(t *testing.T) {
  ctx,cancel:=context.WithCancel(context.Background())
  cancel()
  a:=testNetworkApp(t,"DNS_MODE=firmware\n")
  if _,_,_,_,err:=a.discoverBestServerCandidatesWithSource(ctx);!errors.Is(err,context.Canceled){t.Fatalf("canceled scan must not serve cached catalog or probe anything: %v",err)}
}
