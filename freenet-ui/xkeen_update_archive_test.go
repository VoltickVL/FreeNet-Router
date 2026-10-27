package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func xkeenFixtureArchive(t *testing.T, members map[string]string, special string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz:=gzip.NewWriter(&buf)
	tw:=tar.NewWriter(gz)
	for name, content := range members {
		kind:=byte(tar.TypeReg)
		if name==special {kind=tar.TypeSymlink}
		h:=&tar.Header{Name:name,Mode:0755,Size:int64(len(content)),Typeflag:kind}
		if kind==tar.TypeSymlink {h.Size=0;h.Linkname="../secret"}
		if err:=tw.WriteHeader(h);err!=nil {t.Fatal(err)}
		if kind!=tar.TypeSymlink {if _,err:=tw.Write([]byte(content));err!=nil{t.Fatal(err)}}
	}
	if err:=tw.Close();err!=nil{t.Fatal(err)}
	if err:=gz.Close();err!=nil{t.Fatal(err)}
	return buf.Bytes()
}

func xkeenFixtureTree(t *testing.T, root, version string, candidate bool) {
	t.Helper()
	script:="xkeen"
	modules:=".xkeen"
	if candidate {modules="_xkeen"}
	if err:=os.MkdirAll(filepath.Join(root,modules,"01_info"),0700);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(root,script),[]byte("#!/bin/sh\nexit 0\n"),0700);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(root,modules,"import.sh"),[]byte("#! /bin/sh\n"),0700);err!=nil{t.Fatal(err)}
	meta:="xkeen_current_version=\""+version+"\"\nxkeen_build=\"Stable\"\n"
	if err:=os.WriteFile(filepath.Join(root,modules,"01_info","01_info_variable.sh"),[]byte(meta),0600);err!=nil{t.Fatal(err)}
}

func TestXKeenArchiveValidatorRejectsUntrustedMemberPathsAndTypes(t *testing.T) {
	good :=map[string]string{"xkeen":"#!/bin/sh\nexit 0\n","_xkeen/import.sh":"#!/bin/sh\n","_xkeen/01_info/01_info_variable.sh":"xkeen_current_version=\"2.1\"\nxkeen_build=\"Stable\"\n"}
	for _, tc:=range []struct{name string;badPath string;link string;wrongVersion bool;ok bool}{
		{"valid-release","","",false,true},
		{"path-traversal","_xkeen/../../etc/passwd","",false,false},
		{"absolute-path","/etc/passwd","",false,false},
		{"unrelated-config","etc/xray/04_outbounds.json","",false,false},
		{"symlink","","_xkeen/import.sh",false,false},
		{"wrong-version","","",true,false},
	}{
		t.Run(tc.name,func(t *testing.T){
			content:=map[string]string{}
			for k,v:=range good{content[k]=v}
			if tc.badPath!=""{content[tc.badPath]="never overwrite\n"}
			if tc.wrongVersion{content["_xkeen/01_info/01_info_variable.sh"]="xkeen_current_version=\"2.0\"\nxkeen_build=\"Stable\"\n"}
			data:=xkeenFixtureArchive(t,content,tc.link)
			archive:=filepath.Join(t.TempDir(),"xkeen.tar.gz")
			if err:=os.WriteFile(archive,data,0600);err!=nil{t.Fatal(err)}
			stage:=t.TempDir()
			err:=extractVerifiedXKeenArchive(archive,stage,"2.1")
			if (err==nil)!=tc.ok{t.Fatalf("bad archive accepted=%v err=%v",tc.ok,err)}
			if tc.badPath!="" && strings.HasPrefix(tc.badPath,"_xkeen/.."){
				outside:=filepath.Join(stage,"..","etc","passwd")
				if _,err:=os.Stat(outside);!os.IsNotExist(err){t.Fatal("archive path escaped stage")}
			}
		})
	}
}

func TestXKeenCatalogUpdateAssetRequiresExactGithubReleaseAndDigest(t *testing.T) {
	sha:="sha256:"+strings.Repeat("a",64)
	build:=func(tag,url,hash string,size int64,pre bool) []byte{
		body,_:=json.Marshal(map[string]any{"tag_name":tag,"draft":false,"prerelease":pre,
			"assets":[]map[string]any{{"name":"xkeen.tar.gz","browser_download_url":url,"size":size,"digest":hash}}})
		return body
	}
	legit:=xkeenReleaseAssetURL("2.1")
	for _,tc:=range []struct{name,tag,url,hash string;size int64;pre,ok bool}{
		{"valid","2.1",legit,sha,129691,false,true},
		{"evil-url","2.1","https://evil.invalid/tar.gz",sha,129691,false,false},
		{"http-url","2.1","http://github.com/jameszeroX/XKeen/releases/download/2.1/xkeen.tar.gz",sha,129691,false,false},
		{"tag-drift","2.1","https://github.com/jameszeroX/XKeen/releases/download/2.0/xkeen.tar.gz",sha,129691,false,false},
		{"missing-digest","2.1",legit,"",129691,false,false},
		{"oversize","2.1",legit,sha,99<<20,false,false},
		{"beta","2.1_Beta",xkeenReleaseAssetURL("2.1_Beta"),sha,129691,true,false},
	}{
		t.Run(tc.name,func(t *testing.T){
			got,err:=parseXKeenUpdateAsset(build(tc.tag,tc.url,tc.hash,tc.size,tc.pre))
			if (err==nil)!=tc.ok {t.Fatalf("untrusted asset accepted: %+v %v",got,err)}
		})
	}
}

type xkeenFixtureTransport struct{data []byte;status int}
func (f xkeenFixtureTransport) RoundTrip(req *http.Request)(*http.Response,error){
	if req.Method!="GET" || req.URL.Host!="github.com" {return nil,errors.New("untrusted request")}
	return &http.Response{StatusCode:f.status,Body:io.NopCloser(bytes.NewReader(f.data)),Header:make(http.Header)},nil
}

func TestXKeenArchiveDownloadMatchesOfficialLengthAndSHA(t *testing.T){
	content:=[]byte("not a tar but the exact fixture digest matches")
	hash:=sha256.Sum256(content)
	asset:=xkeenUpdateAsset{Version:"2.1",URL:xkeenReleaseAssetURL("2.1"),Digest:hex.EncodeToString(hash[:]),Size:int64(len(content))}
	client:=&http.Client{Transport:xkeenFixtureTransport{data:content,status:200}}
	got,err:=downloadVerifiedXKeenArchive(context.Background(),client,asset,t.TempDir())
	if err!=nil{t.Fatal(err)}
	defer os.Remove(got)
	copied,err:=os.ReadFile(got)
	if err!=nil||!bytes.Equal(copied,content){t.Fatal("verified download not identical")}
	client=&http.Client{Transport:xkeenFixtureTransport{data:append(content,0),status:200}}
	if _,err:=downloadVerifiedXKeenArchive(context.Background(),client,asset,t.TempDir());err==nil{t.Fatal("extra download bytes accepted")}
}

func TestXKeenSwapAndRollbackNeverTouchVPNConfiguration(t *testing.T) {
	for _, failingStep:=range []string{"","backup-modules","install-modules","install-script"}{
		t.Run("failure-"+failingStep,func(t *testing.T){
			home:=t.TempDir()
			binary:=filepath.Join(home,"xkeen")
			xkeenFixtureTree(t,home,"2.0",false)
			stage,err:=os.MkdirTemp(home,".freenet-xkeen-stage-*")
			if err!=nil{t.Fatal(err)}
			xkeenFixtureTree(t,stage,"2.1",true)
			snapshot,err:=os.MkdirTemp(home,".freenet-xkeen-snapshot-*")
			if err!=nil{t.Fatal(err)}
			config:=filepath.Join(home,"user-xray.json")
			if err:=os.WriteFile(config,[]byte("private config untouched"),0600);err!=nil{t.Fatal(err)}
			xkeenSwapTestHook=func(step string)error{
				if failingStep==step{return errors.New("injected failure")}
				return nil
			}
			t.Cleanup(func(){xkeenSwapTestHook=func(string)error{return nil}})
			err=swapXKeenVersionedFiles(binary,stage,snapshot)
			if failingStep==""{
				if err!=nil{t.Fatal(err)}
				newVersion,channel,metaErr:=readXKeenInstalledMetadata(binary)
				if metaErr!=nil||newVersion!="2.1"||channel!="stable"{t.Fatal("candidate postcheck failed")}
			}else if err==nil{t.Fatal("fault-injection unexpectedly passed")}
			if err:=rollbackXKeenFiles(binary,snapshot,"2.0");err!=nil{t.Fatal(err)}
			version,_,metaErr:=readXKeenInstalledMetadata(binary)
			if metaErr!=nil||version!="2.0"{t.Fatal("rollback version incorrect")}
			data,_:=os.ReadFile(config)
			if string(data)!="private config untouched"{t.Fatal("Xray user config mutated")}
		})
	}
}

func TestXKeenOperationReceiptNeverExposesInternalSnapshotPaths(t *testing.T) {
	home:=t.TempDir()
	binary:=filepath.Join(home,"xkeen")
	snapshot,err:=os.MkdirTemp(home,".freenet-xkeen-snapshot-*")
	if err!=nil{t.Fatal(err)}
	record:=xkeenUpdateDiskState{State:"RUNNING",Rollback:"PENDING",Previous:"2.0",Target:"2.1",SnapshotDir:snapshot}
	if err:=writeXKeenUpdateDiskState(binary,record);err!=nil{t.Fatal(err)}
	s:=publicXKeenUpdateState(binary)
	if s.State!="STOP" || s.Rollback!="UNKNOWN" {t.Fatalf("interrupted operation not fail-closed: %+v",s)}
	jsonBody,_:=json.Marshal(s)
	if strings.Contains(string(jsonBody),snapshot)||strings.Contains(string(jsonBody),home){
		t.Fatal("private XKeen update snapshot path disclosed in API")
	}
}
