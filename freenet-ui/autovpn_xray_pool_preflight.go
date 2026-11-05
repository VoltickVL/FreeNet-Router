package main

import (
    "bytes"
    "context"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "time"
)

// The candidate contains private credentials. It is staged in an isolated
// 0700 directory, validated as a complete Xray configuration, and destroyed.
// Never print the candidate or validator stderr into logs, API or GitHub.
func validateFreeNetAutoPoolCandidateWith(
    ctx context.Context, liveDir string, candidate freeNetAutoPoolCandidate,
    validate func(context.Context,string) error,
) error {
    if ctx==nil || validate==nil || len(candidate.Outbounds)==0 ||
        len(candidate.Routing)==0 || len(candidate.Tags)<2 ||
        len(candidate.Tags)>freeNetAutoPoolLimit {
        return errors.New("AutoVPN candidate unavailable")
    }
    if rejectDuplicateJSONKeys(candidate.Outbounds)!=nil ||
        rejectDuplicateJSONKeys(candidate.Routing)!=nil {
        return errors.New("AutoVPN candidate JSON invalid")
    }
    entries,err:=os.ReadDir(liveDir)
    if err!=nil || len(entries)==0 || len(entries)>64 {
        return errors.New("Xray config inventory unavailable")
    }
    tempDir,err:=os.MkdirTemp("","freenet-autovpn-pool-*")
    if err!=nil{return errors.New("private pool staging unavailable")}
    defer os.RemoveAll(tempDir)
    if os.Chmod(tempDir,0700)!=nil{return errors.New("private pool staging unavailable")}
    seenOutbound:=false;seenRouting:=false
    for _,entry:=range entries {
        name:=entry.Name()
        if !strings.HasSuffix(name,".json") && !strings.HasSuffix(name,".jsonc"){continue}
        path:=filepath.Join(liveDir,name)
        info,statErr:=os.Lstat(path)
        if statErr!=nil || !info.Mode().IsRegular() || info.Size()<=0 ||
            info.Size()>(1024<<10) {
            return errors.New("Xray config fragment unsafe")
        }
        original,readErr:=os.ReadFile(path)
        if readErr!=nil || len(original)!=int(info.Size()){
            return errors.New("Xray config fragment unavailable")
        }
        // Neither xray test nor staged credentials may see a stale or
        // substituted source after a concurrent editor changes it.
        again,statErr:=os.Lstat(path)
        if statErr!=nil || !again.Mode().IsRegular() ||
            !os.SameFile(info,again) || again.Size()!=info.Size() ||
            !again.ModTime().Equal(info.ModTime()) {
            return errors.New("Xray source changed during candidate staging")
        }
        if name=="04_outbounds.json" {original=candidate.Outbounds;seenOutbound=true}
        if name=="05_routing.json" {original=candidate.Routing;seenRouting=true}
        if len(original)==0 || os.WriteFile(filepath.Join(tempDir,name),original,0600)!=nil {
            return errors.New("Xray config candidate staging failed")
        }
    }
    if !seenOutbound || !seenRouting {
        return errors.New("Xray current source layout unsupported")
    }
    testCtx,cancel:=context.WithTimeout(ctx,25*time.Second)
    defer cancel()
    if err:=validate(testCtx,tempDir);err!=nil || testCtx.Err()!=nil {
        return errors.New("full Xray candidate validation failed")
    }
    // Verify the originally read live fragments remain exactly identical.
    // A read-only plan is not a mutation, but an unstable source is not READY.
    for _,name:=range []string{"04_outbounds.json","05_routing.json"} {
        live,err:=os.ReadFile(filepath.Join(liveDir,name))
        stage,stageErr:=os.ReadFile(filepath.Join(tempDir,name))
        if err!=nil||stageErr!=nil||bytes.Equal(live,stage){
            return errors.New("AutoVPN candidate source verification failed")
        }
    }
    return nil
}

func (a *app) validateFreeNetAutoPoolCandidate(ctx context.Context, candidate freeNetAutoPoolCandidate) error {
    if a==nil || strings.TrimSpace(a.cfg.OutPath)==""{return errors.New("AutoVPN configuration unavailable")}
    return validateFreeNetAutoPoolCandidateWith(ctx,a.routingConfigDir(),candidate,
        func(c context.Context,dir string) error {
            return runReadOnlyXrayValidation(c,a.routingXrayBin(),a.routingAssetDir(),dir)
        })
}
