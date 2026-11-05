package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testVisionQUICCanaryFiles(t *testing.T) (string, []byte, []byte) {
	t.Helper()
	dir:=t.TempDir()
	t.Setenv("FREENET_VISION_QUIC_PENDING",filepath.Join(dir,"pending"))
	out:=filepath.Join(dir,"04_outbounds.json")
	before:=[]byte(visionQUICFixture)
	candidate,flow,err:=visionQUICCandidate(before,visionQUICAllowed)
	if err!=nil || flow!=visionQUICStandard {t.Fatal(err)}
	if err:=os.WriteFile(out,before,0600);err!=nil{t.Fatal(err)}
	return out,before,candidate
}

func TestVisionQUICCheckpointSurvivesRestartAndRestoresExactBytes(t *testing.T) {
	out,before,candidate:=testVisionQUICCanaryFiles(t)
	if err:=visionQUICSaveCheckpoint(before,candidate,visionQUICStandard,visionQUICAllowed);err!=nil {
		t.Fatal(err)
	}
	privatePath:=visionQUICPendingPath()
	info,err:=os.Stat(privatePath);if err!=nil || info.Mode().Perm()!=0700{t.Fatal("checkpoint directory not private")}
	saved,receipt,err:=visionQUICReadCheckpoint()
	if err!=nil || !bytes.Equal(saved,before) || receipt.CandidateSHA!=visionQUICDigest(candidate) {
		t.Fatalf("checkpoint mismatch: %v",err)
	}
	if err:=os.WriteFile(out,candidate,0600);err!=nil{t.Fatal(err)}
	a:=&app{cfg:config{OutPath:out,LockPath:filepath.Join(t.TempDir(),"lock")}}
	count:=0
	err=a.visionQUICRollbackWith(func(context.Context)error {count++;return nil},func()bool{return true})
	if err!=nil {t.Fatal(err)}
	restored,err:=os.ReadFile(out)
	if err!=nil || !bytes.Equal(restored,before){t.Fatal("original flow/credentials not restored")}
	if count!=1 {t.Fatalf("restart calls=%d",count)}
	if _,err:=os.Lstat(privatePath);!os.IsNotExist(err){t.Fatal("pending backup not cleaned after verified restore")}
}

func TestVisionQUICRollbackHardStopsOnForeignChanges(t *testing.T){
	out,before,candidate:=testVisionQUICCanaryFiles(t)
	if err:=visionQUICSaveCheckpoint(before,candidate,visionQUICStandard,visionQUICAllowed);err!=nil {t.Fatal(err)}
	foreign:=[]byte(visionQUICFixture+"\n")
	if err:=os.WriteFile(out,foreign,0600);err!=nil{t.Fatal(err)}
	a:=&app{cfg:config{OutPath:out}}
	ran:=false
	err:=a.visionQUICRollbackWith(func(context.Context)error{ran=true;return nil},func()bool{return true})
	if err==nil || ran {t.Fatal("unknown concurrent mutation incorrectly accepted")}
	after,_:=os.ReadFile(out)
	if !bytes.Equal(after,foreign){t.Fatal("foreign bytes overwritten")}
	if _,err:=os.Stat(visionQUICPendingPath());err!=nil{t.Fatal("unknown rollback checkpoint was deleted")}
}

func TestVisionQUICRollbackHardStopsWhenCoreNotVerified(t *testing.T) {
	out,before,candidate:=testVisionQUICCanaryFiles(t)
	if err:=visionQUICSaveCheckpoint(before,candidate,visionQUICStandard,visionQUICAllowed);err!=nil {t.Fatal(err)}
	if err:=os.WriteFile(out,candidate,0600);err!=nil {t.Fatal(err)}
	a:=&app{cfg:config{OutPath:out}}
	err:=a.visionQUICRollbackWith(func(context.Context)error{return errors.New("core failed")},func()bool{return true})
	if err==nil {t.Fatal("restart failure was accepted")}
	restored,_:=os.ReadFile(out)
	if !bytes.Equal(restored,before) {t.Fatal("rollback did not restore on-disk original")}
	if _,err:=os.Stat(visionQUICPendingPath());err!=nil {t.Fatal("unverified runtime checkpoint disappeared")}
}

func TestVisionQUICCheckpointIntegrityAndIncompleteSTOP(t *testing.T) {
	_,before,candidate:=testVisionQUICCanaryFiles(t)
	if err:=visionQUICSaveCheckpoint(before,candidate,visionQUICStandard,visionQUICAllowed);err!=nil {t.Fatal(err)}
	if err:=visionQUICSaveCheckpoint(before,candidate,visionQUICStandard,visionQUICAllowed);err==nil {
		t.Fatal("second concurrent checkpoint allowed")
	}
	path:=filepath.Join(visionQUICPendingPath(),"receipt.json")
	b,err:=os.ReadFile(path);if err!=nil{t.Fatal(err)}
	var receipt visionQUICReceipt
	if json.Unmarshal(b,&receipt)!=nil{t.Fatal("receipt invalid")}
	receipt.CandidateSHA="invalid"
	b,_=json.Marshal(receipt)
	if err:=os.WriteFile(path,b,0600);err!=nil{t.Fatal(err)}
	if _,_,err:=visionQUICReadCheckpoint();err==nil {t.Fatal("corrupt checkpoint accepted")}
}

func TestVisionQUICPlanBoundToExactBytesTargetAndFreshness(t *testing.T){
	before:=[]byte(visionQUICFixture)
	now:=time.Now().UTC()
	issued:=now.Unix()
	token:=visionQUICPlanBind(before,visionQUICAllowed,issued)
	for _,tc:=range []struct{name string;data []byte;target,token string;issued int64;at time.Time;want bool}{
		{"fresh",before,visionQUICAllowed,token,issued,now,true},
		{"expired",before,visionQUICAllowed,token,issued,now.Add(3*time.Minute),false},
		{"future",before,visionQUICAllowed,token,issued,now.Add(-time.Minute),false},
		{"changed-file",append(append([]byte{},before...),byte(' ')),visionQUICAllowed,token,issued,now,false},
		{"different-target",before,visionQUICStandard,token,issued,now,false},
		{"random-token",before,visionQUICAllowed,"abc",issued,now,false},
	}{
		t.Run(tc.name,func(t *testing.T){
			got:=visionQUICValidatePlan(tc.data,tc.target,tc.token,tc.issued,tc.at)
			if got!=tc.want {t.Fatalf("accept=%v want=%v",got,tc.want)}
		})
	}
}

func TestVisionQUICFullConfigPreflightUsesPrivateConfdir(t *testing.T){
	base:=t.TempDir()
	live:=filepath.Join(base,"live")
	if err:=os.Mkdir(live,0700);err!=nil{t.Fatal(err)}
	before:=[]byte(visionQUICFixture)
	candidate,_,err:=visionQUICCandidate(before,visionQUICAllowed)
	if err!=nil{t.Fatal(err)}
	for name,raw:=range map[string][]byte{
		"04_outbounds.json":before,
		"05_routing.json":[]byte(`{"routing":{"rules":[]}}`),
	}{
		if err:=os.WriteFile(filepath.Join(live,name),raw,0600);err!=nil{t.Fatal(err)}
	}
	fake:=filepath.Join(base,"fake-xray")
	script:="#!/bin/sh\n[ \"$1\" = run ] && [ \"$2\" = -test ] && [ \"$3\" = -confdir ] || exit 1\n" +
		"[ -f \"$4/04_outbounds.json\" ] && [ -f \"$4/05_routing.json\" ] || exit 2\n" +
		"grep -q xtls-rprx-vision-udp443 \"$4/04_outbounds.json\" || exit 3\n" +
		"grep -q 'routing' \"$4/05_routing.json\" || exit 4\n" +
		"exit 0\n"
	if err:=os.WriteFile(fake,[]byte(script),0700);err!=nil{t.Fatal(err)}
	if err:=visionQUICPreflight(context.Background(),fake,base,live,"04_outbounds.json",candidate);err!=nil {
		t.Fatalf("whole configuration preflight failed: %v",err)
	}
	if err:=visionQUICPreflight(context.Background(),fake,base,live,"outside.json",candidate);err==nil{
		t.Fatal("arbitrary candidate filename accepted")
	}
	// A bad Xray outcome never exposes credential-bearing config or command output.
	bad:=filepath.Join(base,"bad-xray")
	if err:=os.WriteFile(bad,[]byte("#!/bin/sh\necho 'test-nonsecret-identity' >&2\nexit 1\n"),0700);err!=nil{t.Fatal(err)}
	if err:=visionQUICPreflight(context.Background(),bad,base,live,"04_outbounds.json",candidate);err==nil ||
		strings.Contains(err.Error(),"test-nonsecret-identity") {
		t.Fatalf("failed preflight exposed private data: %v",err)
	}
}
