package main

import (
    "bytes"
    "context"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func testFreeNetPoolFolder(t *testing.T) string {
    t.Helper()
    dir:=t.TempDir()
    for name,value:=range map[string]string{
        "01_log.json":`{"log":{"loglevel":"warning"}}`,
        "04_outbounds.json":freeNetAutoPoolCurrent,
        "05_routing.json":freeNetAutoPoolRouting,
    }{
        if err:=os.WriteFile(filepath.Join(dir,name),[]byte(value),0600);err!=nil{t.Fatal(err)}
    }
    return dir
}
func testFreeNetPoolCandidate(t *testing.T) freeNetAutoPoolCandidate {
    t.Helper()
    p,err:=buildFreeNetAutoPoolCandidate([]byte(freeNetAutoPoolCurrent),[]byte(freeNetAutoPoolRouting),
        []map[string]any{freeNetAutoPoolAlternative(t)},"127.0.0.1:10085","127.0.0.1:10086")
    if err!=nil{t.Fatal(err)}
    return p
}

func TestFreeNetPoolPrivateFullCandidateStaging(t *testing.T) {
    dir:=testFreeNetPoolFolder(t)
    candidate:=testFreeNetPoolCandidate(t)
    var staged string
    err:=validateFreeNetAutoPoolCandidateWith(context.Background(),dir,candidate,
        func(_ context.Context,priv string)error {
            staged=priv
            mode,e:=os.Stat(priv)
            if e!=nil || mode.Mode().Perm()!=0700 {t.Fatal("candidate staging not private")}
            entries,e:=os.ReadDir(priv)
            if e!=nil || len(entries)!=3{t.Fatal("full configuration not staged")}
            outbound,e:=os.ReadFile(filepath.Join(priv,"04_outbounds.json"))
            if e!=nil||!bytes.Equal(outbound,candidate.Outbounds){t.Fatal("candidate outbound missing")}
            routing,e:=os.ReadFile(filepath.Join(priv,"05_routing.json"))
            if e!=nil||!bytes.Equal(routing,candidate.Routing){t.Fatal("candidate routing missing")}
            return nil
        })
    if err!=nil{t.Fatal(err)}
    if _,err:=os.Stat(staged);!os.IsNotExist(err){t.Fatal("temporary credentials not deleted")}
    disk,_:=os.ReadFile(filepath.Join(dir,"04_outbounds.json"))
    if string(disk)!=freeNetAutoPoolCurrent{t.Fatal("read-only preflight rewrote live VPN")}
}

func TestFreeNetPoolPreflightFailsClosedWhenSourcesChange(t *testing.T) {
    candidate:=testFreeNetPoolCandidate(t)
    for _,tc:=range []struct{
        name string
        setup func(string)error
        validate func(context.Context,string)error
    }{
        {"validator-failed",func(string)error{return nil},func(context.Context,string)error{return errors.New("private sensitive output")}},
        {"symlink",func(dir string)error{
            if e:=os.Remove(filepath.Join(dir,"01_log.json"));e!=nil{return e}
            return os.Symlink(filepath.Join(dir,"04_outbounds.json"),filepath.Join(dir,"01_log.json"))
         },func(context.Context,string)error{return nil}},
        {"source-changed",func(string)error{return nil},nil},
    } {
        t.Run(tc.name,func(t *testing.T){
            dir:=testFreeNetPoolFolder(t)
            if err:=tc.setup(dir);err!=nil{t.Fatal(err)}
            validator:=tc.validate
            if validator==nil{
                validator=func(_ context.Context,priv string)error{
                    return os.WriteFile(filepath.Join(dir,"01_log.json"),[]byte(`{"log":{"loglevel":"debug"}}`),0600)
                }
            }
            err:=validateFreeNetAutoPoolCandidateWith(context.Background(),dir,candidate,validator)
            if err==nil{t.Fatal("unsafe candidate passed")}
            if strings.Contains(err.Error(),"private sensitive") {t.Fatal("secret-bearing validator errors leaked")}
        })
    }
}
