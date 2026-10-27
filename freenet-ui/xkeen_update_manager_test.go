package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func xkeenGuardedFixtureArchive(t *testing.T) string {
	t.Helper()
	var out bytes.Buffer
	gz:=gzip.NewWriter(&out)
	tw:=tar.NewWriter(gz)
	files:=map[string]string{
		"xkeen":"#!/bin/sh\nexit 0\n",
		"_xkeen/import.sh":"#!/bin/sh\n",
		"_xkeen/01_info/01_info_variable.sh":"xkeen_current_version=\"2.1\"\nxkeen_build=\"Stable\"\n",
	}
	for _,name:=range []string{"xkeen","_xkeen/import.sh","_xkeen/01_info/01_info_variable.sh"}{
		b:=[]byte(files[name])
		h:=&tar.Header{Name:name,Mode:0755,Size:int64(len(b)),Typeflag:tar.TypeReg}
		if err:=tw.WriteHeader(h);err!=nil{t.Fatal(err)}
		if _,err:=tw.Write(b);err!=nil{t.Fatal(err)}
	}
	if err:=tw.Close();err!=nil{t.Fatal(err)}
	if err:=gz.Close();err!=nil{t.Fatal(err)}
	name:=filepath.Join(t.TempDir(),"fixture.tar.gz")
	if err:=os.WriteFile(name,out.Bytes(),0600);err!=nil{t.Fatal(err)}
	return name
}

func TestXKeenUpdaterPlanStopsOnAmbiguousXrayWithoutMutation(t *testing.T) {
	dir:=t.TempDir()
	xkeenFixtureTree(t,dir,"2.0",false)
	binary:=filepath.Join(dir,"xkeen")
	app:=&app{cfg:config{XKeenPath:binary},sem:make(chan struct{},1)}
	oldAsset:=xkeenUpdateGetAsset
	oldProof:=xkeenUpdateProductionReady
	defer func(){xkeenUpdateGetAsset=oldAsset;xkeenUpdateProductionReady=oldProof}()
	xkeenUpdateGetAsset=func(context.Context)(xkeenUpdateAsset,error){
		return xkeenUpdateAsset{Version:"2.1",URL:xkeenReleaseAssetURL("2.1"),
			Digest:strings.Repeat("a",64),Size:129691},nil
	}
	xkeenUpdateProductionReady=func(*app)bool{return false}
	plan,_:=app.xkeenUpdatePrepare(context.Background())
	if plan.Ready || plan.State!="blocked" || plan.Reason!="xray-production-identity-unverified" ||
		plan.Mutation!="NONE" || plan.RuntimeChange!="NONE" {
		t.Fatalf("unknown Xray must block update even with valid release: %+v",plan)
	}
	installed,_,err:=readXKeenInstalledMetadata(binary)
	if err!=nil||installed!="2.0" {t.Fatal("read-only plan changed installed XKeen")}
}

func TestXKeenScriptTransactionSuccessAndAutomaticRollback(t *testing.T) {
	fixture:=xkeenGuardedFixtureArchive(t)
	asset:=xkeenUpdateAsset{Version:"2.1",URL:xkeenReleaseAssetURL("2.1"),
		Digest:strings.Repeat("a",64),Size:129691}
	oldArchive:=xkeenUpdateGetArchive
	oldProof:=xkeenUpdateProductionReady
	defer func(){xkeenUpdateGetArchive=oldArchive;xkeenUpdateProductionReady=oldProof;
		xkeenSwapTestHook=func(string)error{return nil}}()
	xkeenUpdateProductionReady=func(*app)bool{return true}
	xkeenUpdateGetArchive=func(_ context.Context,_ xkeenUpdateAsset,parent string)(string,error){
		content,err:=os.ReadFile(fixture)
		if err!=nil{return "",err}
		path:=filepath.Join(parent,"verified-xkeen-fixture.tar.gz")
		return path,os.WriteFile(path,content,0600)
	}
	for _,tc:=range []struct{name,fail string;wantState,wantVersion,wantRollback string}{
		{"success","","SUCCESS","2.1","NOT_NEEDED"},
		{"failure-after-backup","install-modules","ROLLED_BACK","2.0","SUCCESS"},
	}{
		t.Run(tc.name,func(t *testing.T){
			dir:=t.TempDir()
			xkeenFixtureTree(t,dir,"2.0",false)
			conf:=filepath.Join(dir,"private-xray-config.json")
			if err:=os.WriteFile(conf,[]byte("private VPN config"),0600);err!=nil{t.Fatal(err)}
			binary:=filepath.Join(dir,"xkeen")
			app:=&app{cfg:config{XKeenPath:binary},sem:make(chan struct{},1)}
			xkeenSwapTestHook=func(step string)error{
				if step==tc.fail{return os.ErrPermission}
				return nil
			}
			before:=xkeenUpdateDiskState{State:"RUNNING",Previous:"2.0",Target:"2.1",Rollback:"PENDING"}
			if err:=writeXKeenUpdateDiskState(binary,before);err!=nil{t.Fatal(err)}
			setXKeenUpdateLocalActive(true)
			result:=app.runXKeenGuardedUpdate(asset,"2.0")
			setXKeenUpdateLocalActive(false)
			if result.State!=tc.wantState||result.Rollback!=tc.wantRollback {
				t.Fatalf("update state mismatch %+v",result)
			}
			got,_,err:=readXKeenInstalledMetadata(binary)
			if err!=nil||got!=tc.wantVersion{
				t.Fatalf("XKeen installed version after update got=%s err=%v want=%s",got,err,tc.wantVersion)
			}
			v,err:=os.ReadFile(conf)
			if err!=nil||string(v)!="private VPN config"{t.Fatal("Xray user config was changed")}
			after,err:=readXKeenUpdateDiskState(binary)
			if err!=nil||after.State!=tc.wantState{
				t.Fatalf("terminal receipt not durable: %+v err=%v",after,err)
			}
		})
	}
}
