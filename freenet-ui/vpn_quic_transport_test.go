package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const visionQUICFixture = `{
 "outbounds":[
  {"tag":"vless-reality","protocol":"vless",
   "settings":{"vnext":[{"address":"sample.invalid","port":443,"users":[{"id":"test-nonsecret-identity","flow":"xtls-rprx-vision","encryption":"none"}]}]},
   "streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverName":"example.invalid","publicKey":"test-nonsecret","shortId":"00"}}},
  {"tag":"direct","protocol":"freedom","streamSettings":{"sockopt":{"mark":255}}},
  {"tag":"block","protocol":"blackhole"}
 ],"custom":{"keep":"untouched"}
}`

func visionQUICDocument(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {t.Fatal(err)}
	return v
}

func TestVisionQUICCandidateChangesOnlyFlowAndReverses(t *testing.T) {
	original := []byte(visionQUICFixture)
	changed, before, err := visionQUICCandidate(original, visionQUICAllowed)
	if err != nil || before != visionQUICStandard {t.Fatalf("candidate: before=%q err=%v", before, err)}
	if bytes.Equal(original,changed) {t.Fatal("candidate was not changed")}
	orig := visionQUICDocument(t,original)
	out := visionQUICDocument(t,changed)
	outbound := out["outbounds"].([]any)[0].(map[string]any)
	user := outbound["settings"].(map[string]any)["vnext"].([]any)[0].(map[string]any)["users"].([]any)[0].(map[string]any)
	if got:=user["flow"];got!=visionQUICAllowed{t.Fatalf("flow=%v",got)}
	user["flow"] = visionQUICStandard
	if !reflect.DeepEqual(orig,out) {t.Fatal("candidate changed endpoint, credentials, routing or unrelated outbounds")}
	reversed, previously, err := visionQUICCandidate(changed, visionQUICStandard)
	if err != nil || previously != visionQUICAllowed {t.Fatalf("reverse: %q %v",previously,err)}
	if !reflect.DeepEqual(visionQUICDocument(t,original),visionQUICDocument(t,reversed)) {t.Fatal("reverse did not recover exact logical profile")}
	if len(visionQUICPlanToken(original,visionQUICAllowed))!=64 {t.Fatal("missing opaque plan token")}
	if visionQUICPlanToken(original,visionQUICAllowed)==visionQUICPlanToken(changed,visionQUICAllowed) ||
		visionQUICPlanToken(original,visionQUICAllowed)==visionQUICPlanToken(original,visionQUICStandard) {
		t.Fatal("plan does not bind configuration bytes and requested mode")
	}
	for _,secret := range []string{"test-nonsecret-identity","sample.invalid","test-nonsecret"} {
		if strings.Contains(visionQUICPlanToken(original,visionQUICAllowed),secret){t.Fatal("plan leaks original config content")}
	}
}

func TestVisionQUICCandidateRejectsUnsafeAndAmbiguousProfiles(t *testing.T) {
	for _,tc:=range []struct{name,raw,target string}{
		{"unknown-target",visionQUICFixture,"random"},
		{"no-change",visionQUICFixture,visionQUICStandard},
		{"not-json","not json",visionQUICAllowed},
		{"trailing-json",visionQUICFixture+" {}",visionQUICAllowed},
		{"root-duplicate-key",`{"outbounds":[],"outbounds":[]}`,visionQUICAllowed},
		{"nested-duplicate-key",strings.Replace(visionQUICFixture,`"id":"test-nonsecret-identity"`,`"id":"first","id":"second"`,1),visionQUICAllowed},
		{"no-target",strings.Replace(visionQUICFixture,`"vless-reality"`,`"other"`,1),visionQUICAllowed},
		{"duplicate-target",strings.Replace(visionQUICFixture,`"tag":"direct"`,`"tag":"vless-reality"`,1),visionQUICAllowed},
		{"wrong-protocol",strings.Replace(visionQUICFixture,`"protocol":"vless"`,`"protocol":"freedom"`,1),visionQUICAllowed},
		{"ws",strings.Replace(visionQUICFixture,`"network":"tcp"`,`"network":"ws"`,1),visionQUICAllowed},
		{"tls",strings.Replace(visionQUICFixture,`"security":"reality"`,`"security":"tls"`,1),visionQUICAllowed},
		{"not-vision",strings.Replace(visionQUICFixture,visionQUICStandard,"random-flow",1),visionQUICAllowed},
		{"missing-user",strings.Replace(visionQUICFixture,`"users":[{"id":"test-nonsecret-identity","flow":"xtls-rprx-vision","encryption":"none"}]`,`"users":[]`,1),visionQUICAllowed},
		{"missing-credential",strings.Replace(visionQUICFixture,`"id":"test-nonsecret-identity",`,"",1),visionQUICAllowed},
		{"oversized",strings.Repeat("x",visionQUICMaxConfigBytes+1),visionQUICAllowed},
	}{
		t.Run(tc.name,func(t *testing.T){
			_,_,err:=visionQUICCandidate([]byte(tc.raw),tc.target)
			if err==nil {t.Fatal("unsafe candidate accepted")}
			if strings.Contains(err.Error(),"test-nonsecret") || strings.Contains(err.Error(),"sample.invalid") {
				t.Fatalf("credential data leaked: %v",err)
			}
		})
	}
}

func TestVisionQUICPreflightIsPrivateAndNeverLeaksCandidate(t *testing.T) {
	base:=[]byte(visionQUICFixture)
	changed,_,err:=visionQUICCandidate(base,visionQUICAllowed)
	if err!=nil{t.Fatal(err)}
	var path string
	err=visionQUICPreflightWith(context.Background(),changed,func(_ context.Context,p string)error{
		path=p
		info,e:=os.Stat(p);if e!=nil{return e}
		if info.Mode().Perm()!=0600{return errors.New("not private")}
		dir,e:=os.Stat(filepath.Dir(p));if e!=nil{return e}
		if dir.Mode().Perm()!=0700{return errors.New("directory not private")}
		staged,e:=os.ReadFile(p);if e!=nil{return e}
		if !bytes.Equal(staged,changed){return errors.New("candidate differs")}
		return nil
	})
	if err!=nil{t.Fatal(err)}
	if _,err:=os.Stat(path);!os.IsNotExist(err){t.Fatal("secret-bearing temporary config not removed")}
	err=visionQUICPreflightWith(context.Background(),changed,func(context.Context,string)error{
		return errors.New("test-nonsecret-identity should never reach API")
	})
	if err==nil || strings.Contains(err.Error(),"test-nonsecret"){t.Fatalf("raw Xray failure exposed: %v",err)}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	ran:=false
	err=visionQUICPreflightWith(ctx,changed,func(context.Context,string)error{ran=true;return nil})
	if ran || err==nil{t.Fatal("cancelled probe was executed")}
}
