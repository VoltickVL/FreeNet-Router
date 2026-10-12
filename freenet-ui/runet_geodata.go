package main

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "errors"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "time"
)

// The canonical published assets live on runetfreedom's release branches,
// not the GitHub Releases "latest" endpoint or XKeen's stock geo catalogs.
const (
    runetGeoSiteBase = "https://raw.githubusercontent.com/runetfreedom/russia-blocked-geosite/release/"
    runetGeoIPBase   = "https://raw.githubusercontent.com/runetfreedom/russia-blocked-geoip/release/"
    runetGeoMaxBytes = int64(96 << 20) // bounded streaming: 512 MB routers
    runetGeoPending  = ".freenet-runet-activation.pending"
)

type runetGeoSource struct {
    name string
    base string
    kind GeoDataKind
}
var runetGeoSources = []runetGeoSource{
    {name:"geosite.dat", base:runetGeoSiteBase, kind:GeoDataSite},
    {name:"geoip.dat", base:runetGeoIPBase, kind:GeoDataIP},
}
type runetGeoCandidate struct {
    name string
    path string
    changed bool
}

func runetGeoHTTPClient() *http.Client {
    return &http.Client{
        Timeout: 3*time.Minute,
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            if len(via) > 3 || req.URL.Scheme != "https" {
                return errors.New("unsafe geodata redirect")
            }
            return nil
        },
    }
}

// SHA256SUM format is exactly "<sha256>  filename" (or "*filename").
func runetGeoChecksum(raw []byte, name string) (string,error) {
    if len(raw)==0 || len(raw)>256 || strings.Count(strings.TrimSpace(string(raw)),"\n")!=0 {
        return "",errors.New("invalid geodata checksum metadata")
    }
    parts:=strings.Fields(string(raw))
    if len(parts)!=2 || strings.TrimPrefix(parts[1],"*")!=name || len(parts[0])!=64 {
        return "",errors.New("invalid geodata checksum metadata")
    }
    if _,err:=hex.DecodeString(parts[0]);err!=nil {
        return "",errors.New("invalid geodata checksum metadata")
    }
    return strings.ToLower(parts[0]),nil
}

func runetGeoGet(ctx context.Context,client *http.Client,url string)(io.ReadCloser,error){
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,url,nil)
    if err!=nil{return nil,err}
    resp,err:=client.Do(req)
    if err!=nil{return nil,errors.New("geodata source is unavailable")}
    if resp.StatusCode!=http.StatusOK {
        resp.Body.Close()
        return nil,errors.New("geodata source returned non-200")
    }
    return resp.Body,nil
}

func runetGeoFileHash(path string)(string,error){
    f,err:=os.Open(path)
    if err!=nil{return "",err}
    defer f.Close()
    h:=sha256.New()
    if _,err=io.Copy(h,f);err!=nil{return "",err}
    return hex.EncodeToString(h.Sum(nil)),nil
}

func runetGeoStage(ctx context.Context,client *http.Client,dir string,src runetGeoSource,validate func(context.Context,string,GeoDataKind)error)(candidate runetGeoCandidate,err error){
    candidate.name=src.name
    checksumBody,err:=runetGeoGet(ctx,client,src.base+src.name+".sha256sum")
    if err!=nil{return candidate,err}
    checksum,readErr:=io.ReadAll(io.LimitReader(checksumBody,257))
    checksumBody.Close()
    if readErr!=nil{return candidate,errors.New("geodata checksum cannot be read")}
    expected,err:=runetGeoChecksum(checksum,src.name)
    if err!=nil{return candidate,err}

    target:=filepath.Join(dir,src.name)
    fi,err:=os.Lstat(target)
    if err!=nil || !fi.Mode().IsRegular() || fi.Size()==0 {
        return candidate,fmt.Errorf("%s: installed asset is missing or unsafe",src.name)
    }
    body,err:=runetGeoGet(ctx,client,src.base+src.name)
    if err!=nil{return candidate,err}
    defer body.Close()
    f,err:=os.CreateTemp(dir,".freenet-runet-*.dat")
    if err!=nil{return candidate,errors.New("cannot stage geodata on destination filesystem")}
    candidate.path=f.Name()
    defer func(){if err!=nil{os.Remove(candidate.path)}}()
    h:=sha256.New()
    count,copyErr:=io.Copy(io.MultiWriter(f,h),io.LimitReader(body,runetGeoMaxBytes+1))
    if copyErr!=nil || count<1024 || count>runetGeoMaxBytes || ctx.Err()!=nil{
        f.Close()
        return candidate,errors.New("geodata download incomplete or outside safe limits")
    }
    if hex.EncodeToString(h.Sum(nil))!=expected{
        f.Close()
        return candidate,errors.New("geodata sha256 does not match published checksum")
    }
    if err=f.Chmod(fi.Mode().Perm());err!=nil{
        f.Close()
        return candidate,errors.New("cannot preserve geodata permissions")
    }
    if err=f.Sync();err!=nil{
        f.Close()
        return candidate,errors.New("cannot sync geodata candidate")
    }
    if err=f.Close();err!=nil{
        return candidate,errors.New("cannot finish geodata candidate")
    }
    if validate!=nil{
        if err=validate(ctx,candidate.path,src.kind);err!=nil{
            return candidate,fmt.Errorf("%s: protobuf candidate validation failed",src.name)
        }
    }
    oldHash,hashErr:=runetGeoFileHash(target)
    if hashErr!=nil{return candidate,errors.New("cannot verify installed geodata hash")}
    candidate.changed=oldHash!=expected
    return candidate,nil
}

// Preserve original inodes using same-filesystem hard links. If either rename
// fails, restore *both* old files. A failed rollback fences future mutations.
func runetGeoCommit(dir string,staged []runetGeoCandidate) error{
    backups:=map[string]string{}
    changed:=[]runetGeoCandidate{}
    for _,item:=range staged{if item.changed{changed=append(changed,item)}}
    for _,item:=range changed{
        target:=filepath.Join(dir,item.name)
        f,err:=os.CreateTemp(dir,".freenet-runet-backup-*.dat")
        if err!=nil{
            for _,b:=range backups{os.Remove(b)}
            return errors.New("cannot reserve geodata rollback")
        }
        backup:=f.Name()
        f.Close()
        os.Remove(backup)
        if err=os.Link(target,backup);err!=nil{
            for _,b:=range backups{os.Remove(b)}
            return errors.New("cannot snapshot geodata for rollback")
        }
        backups[item.name]=backup
    }
    applied:=[]runetGeoCandidate{}
    var failed error
    for _,item:=range changed{
        if err:=os.Rename(item.path,filepath.Join(dir,item.name));err!=nil{
            failed=errors.New("cannot atomically replace geodata")
            break
        }
        applied=append(applied,item)
    }
    if failed!=nil{
        for i:=len(applied)-1;i>=0;i--{
            item:=applied[i]
            if err:=os.Rename(backups[item.name],filepath.Join(dir,item.name));err!=nil{
                return errors.New("GEODATA ROLLBACK FAILED/UNKNOWN: STOP")
            }
        }
    }
    for _,b:=range backups{os.Remove(b)}
    return failed
}

// Memory-bounded protobuf validation; no complete .dat is loaded in RAM.
func validateRunetGeoDataCandidate(ctx context.Context,path string,kind GeoDataKind)error{
    query:="example.org"
    if kind==GeoDataIP{query="8.8.8.8"}
    result,err:=searchGeoDataFileStream(ctx,path,kind,query,256)
    if err!=nil || !result.Observed || result.Truncated{
        return errors.New("geodata protobuf validation failed or incomplete")
    }
    return nil
}

func updateRunetGeoData(ctx context.Context,dir string,client *http.Client,sources []runetGeoSource,validate func(context.Context,string,GeoDataKind)error)(changed bool,err error){
    if client==nil{client=runetGeoHTTPClient()}
    if len(sources)!=2{return false,errors.New("both runetfreedom geodata sources are required")}
    info,err:=os.Stat(dir)
    if err!=nil || !info.IsDir(){return false,errors.New("geodata directory unavailable")}
    lock:=filepath.Join(dir,".freenet-runet-update.lock")
    if err=os.Mkdir(lock,0700);err!=nil{return false,errors.New("geodata busy or previous state unknown: STOP")}
    keepLock:=false
    defer func(){if !keepLock{os.Remove(lock)}}()
    staged:=make([]runetGeoCandidate,0,2)
    defer func(){for _,item:=range staged{if item.path!=""{os.Remove(item.path)}}}()
    for _,src:=range sources{
        item,stageErr:=runetGeoStage(ctx,client,dir,src,validate)
        if stageErr!=nil{return false,stageErr}
        staged=append(staged,item)
        changed=changed||item.changed
    }
    if ctx.Err()!=nil{return false,ctx.Err()}
    if changed{
        // Durable pending marker keeps the UI honest until controlled Xray
        // activation is implemented/confirmed; do not restart live Xray here.
        if err=os.WriteFile(filepath.Join(dir,runetGeoPending),[]byte("pending: verified geodata on disk; Xray reload not confirmed\n"),0600);err!=nil{
            return false,errors.New("cannot persist geodata activation checkpoint")
        }
    }
    if err=runetGeoCommit(dir,staged);err!=nil{
        if strings.Contains(err.Error(),"ROLLBACK FAILED/UNKNOWN"){keepLock=true}
        // Do not erase an earlier pending marker on a failed attempt.
        return false,err
    }
    return changed,nil
}
