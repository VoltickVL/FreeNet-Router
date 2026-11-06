package main

// The only writer owned by FreeNet 0.9 is a dedicated Xray *outbound fragment*.
// Original XKeen, Xray, other configs, routing, DNS, ipset and cron remain
// untouched. There is no backup/recovery subsystem: a failed single-file
// transaction restores only its previous managed fragment.
import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "errors"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "sort"
    "strings"
    "time"
)

// Official Xray confdir merge PREPENDS new outbound arrays unless the file
// name contains "tail". Never hijack the existing default outbound or change
// DIRECT/VPN routing simply because the user imported a subscription.
const subscriptionFragmentName = "04_zz_freenet09_subscription_tail.json"
const subscriptionStateName = "subscription-state.json"
const maxNativeConfigs = 100
const maxNativeConfigAggregate = 16 << 20

type nativeSubscriptionState struct {
    URL string `json:"url"`
    SHA256 string `json:"sha256"`
    Balancer string `json:"balancer"`
    Selector string `json:"selector"`
}
type nativeApplyResult struct {
    State string `json:"state"`
    Runtime string `json:"runtime"`
    Count int `json:"count,omitempty"`
    Balancer string `json:"balancer,omitempty"`
    Applied bool `json:"applied"`
}

func applyStopped() nativeApplyResult {
    return nativeApplyResult{State:"STOP",Runtime:"NOT_VERIFIED",Applied:false}
}
func hashConfig(b []byte) string {
    hash:=sha256.Sum256(b)
    return hex.EncodeToString(hash[:])
}

func nativePrivateDir(root string)(string,error){
    dir:=filepath.Join(root,"etc/freenet09")
    if observedPath(root,"etc/freenet09",true)!="PRESENT"{
        return "",errors.New("private freeNet directory missing")
    }
    info,err:=os.Lstat(dir)
    if err!=nil || !info.IsDir() || info.Mode().Perm()!=0700 {
        return "",errors.New("private FreeNet directory unsafe")
    }
    return dir,nil
}

// read the complete Xray confdir as ordinary regular files. It also rejects
// stop-list names, which XKeen 2.1 would otherwise refuse to start.
func readNativeConfdir(root string)(map[string][]byte,error){
    if observedPath(root,"etc/xray/configs",true)!="PRESENT" {
        return nil,errors.New("native Xray config directory absent")
    }
    dir:=filepath.Join(root,"etc/xray/configs")
    entries,err:=os.ReadDir(dir)
    if err!=nil || len(entries)>maxNativeConfigs {return nil,errors.New("native config directory unavailable")}
    files:=make(map[string][]byte)
    total:=0
    for _,entry:=range entries {
        name:=entry.Name()
        // Pre-tail experimental fragments may have prepended their outbound
        // and altered Xray default selection. Never silently mix/migrate them.
        if name=="04_zz_freenet09_subscription.json" {
            return nil,errors.New("non-tail subscription fragment requires inspection")
        }
        ext:=strings.ToLower(filepath.Ext(name))
        if ext!=".json" && ext!=".jsonc" && ext!=".yaml" && ext!=".yml" && ext!=".toml" {continue}
        // Native XKeen forbids backup names with these words in config root.
        // Only write our known safe name, and STOP if the original root is partial.
        low:=strings.ToLower(name)
        if ext==".json" {
            for _,word:=range []string{"bak","old","copy","orig","save","temp","tmp","копия"}{
                if strings.Contains(low,word){return nil,errors.New("XKeen stoplist file in root")}
            }
            if strings.Contains(low,"(")&&strings.Contains(low,").json"){return nil,errors.New("XKeen parenthesized filename")}
        }
        rel:=filepath.Join("etc/xray/configs",name)
        if observedPath(root,rel,false)!="PRESENT" {return nil,errors.New("symlink/unknown native config")}
        info,err:=os.Lstat(filepath.Join(dir,name))
        if err!=nil || !info.Mode().IsRegular() || info.Size()>nativeConfigMaxBytes {
            return nil,errors.New("native config unsafe size/type")
        }
        raw,err:=os.ReadFile(filepath.Join(dir,name))
        if err!=nil || int64(len(raw))!=info.Size() {
            return nil,errors.New("native config read failed")
        }
        total+=len(raw)
        if total>maxNativeConfigAggregate{return nil,errors.New("native config aggregate too large")}
        files[name]=raw
    }
    if len(files)==0 {return nil,errors.New("native Xray confdir is empty")}
    return files,nil
}

func sameNativeConfdir(a,b map[string][]byte)bool{
    if len(a)!=len(b){return false}
    for k,v:=range a{
        other,ok:=b[k]
        if !ok || hashConfig(v)!=hashConfig(other){return false}
    }
    return true
}

func readNativeSubscriptionState(root string, files map[string][]byte)(nativeSubscriptionState,error){
    var state nativeSubscriptionState
    _,fragmentExists:=files[subscriptionFragmentName]
    rel:="etc/freenet09/"+subscriptionStateName
    status:=observedPath(root,rel,false)
    if status=="MISSING" && !fragmentExists{return state,nil}
    if status!="PRESENT" || !fragmentExists{return state,errors.New("unknown managed subscription ownership")}
    info,err:=os.Lstat(filepath.Join(root,rel))
    if err!=nil || !info.Mode().IsRegular() || info.Mode().Perm()!=0600 || info.Size()>8192 {
        return state,errors.New("managed subscription state permissions unknown")
    }
    raw,err:=os.ReadFile(filepath.Join(root,rel))
    if err!=nil || json.Unmarshal(raw,&state)!=nil ||
        state.URL=="" || state.Balancer=="" || state.Selector=="" ||
        state.SHA256!=hashConfig(files[subscriptionFragmentName]) {
        return nativeSubscriptionState{},errors.New("managed subscription content differs from receipt")
    }
    if _,err:=validateSubscriptionURL(state.URL);err!=nil {
        return nativeSubscriptionState{},errors.New("managed subscription URL invalid")
    }
    return state,nil
}

func atomicWritePrivate(path string,data []byte) error {
    dir:=filepath.Dir(path)
    tmp,err:=os.CreateTemp(dir,".freenet09-write-*")
    if err!=nil{return errors.New("cannot create temporary native file")}
    tmpPath:=tmp.Name()
    defer os.Remove(tmpPath)
    if err:=tmp.Chmod(0600);err!=nil {tmp.Close();return errors.New("cannot chmod native candidate")}
    if _,err:=tmp.Write(data);err!=nil{tmp.Close();return errors.New("cannot write native candidate")}
    if err:=tmp.Sync();err!=nil{tmp.Close();return errors.New("cannot sync native candidate")}
    if err:=tmp.Close();err!=nil{return errors.New("cannot close native candidate")}
    if err:=os.Rename(tmpPath,path);err!=nil{return errors.New("cannot commit native candidate")}
    fd,err:=os.Open(dir)
    if err==nil { _=fd.Sync();_ = fd.Close() }
    return nil
}

func nativeConfigTest(ctx context.Context,root,configDir string)bool{
    if observedPath(root,"sbin/xray",false)!="PRESENT" {return false}
    binary:=filepath.Join(root,"sbin/xray")
    info,err:=os.Lstat(binary)
    if err!=nil || !info.Mode().IsRegular() || info.Mode()&0111==0 {return false}
    c,cancel:=context.WithTimeout(ctx,20*time.Second)
    defer cancel()
    cmd:=exec.CommandContext(c,binary,"run","-test","-confdir",configDir)
    cmd.Stdout=io.Discard;cmd.Stderr=io.Discard;cmd.Stdin=nil
    cmd.WaitDelay=500*time.Millisecond
    if observedPath(root,"etc/xray/dat",true)=="PRESENT" {
        cmd.Env=append(os.Environ(),"XRAY_LOCATION_ASSET="+filepath.Join(root,"etc/xray/dat"))
    }
    return cmd.Run()==nil && c.Err()==nil
}

// A disposable candidate dir outside the live Xray confdir validates all
// existing configs + the generated fragment using the ORIGINAL Xray binary.
func stageNativeSubscription(root string,source map[string][]byte,candidate []byte) (bool,error){
    privateDir,err:=nativePrivateDir(root)
    if err!=nil{return false,err}
    tempDir,err:=os.MkdirTemp(privateDir,"candidate-")
    if err!=nil{return false,errors.New("cannot stage native config")}
    defer os.RemoveAll(tempDir)
    if err:=os.Chmod(tempDir,0700);err!=nil{return false,errors.New("stage perms unavailable")}
    names:=make([]string,0,len(source))
    for name:=range source {
        if name==subscriptionFragmentName{continue}
        names=append(names,name)
    }
    sort.Strings(names)
    for _,name:=range names {
        if err:=os.WriteFile(filepath.Join(tempDir,name),source[name],0600);err!=nil {
            return false,errors.New("stage native config failed")
        }
    }
    if err:=os.WriteFile(filepath.Join(tempDir,subscriptionFragmentName),candidate,0600);err!=nil {
        return false,errors.New("cannot stage subscription candidate")
    }
    return nativeConfigTest(context.Background(),root,tempDir),nil
}

func synchronousXKeen(ctx context.Context,root,operation string) bool {
    if operation!="-restart" && operation!="-status" {return false}
    // Official XKeen 2.1 "-status" enters package self-heal before reaching
    // S05xkeen. Call its ORIGINAL init "status" directly to avoid hidden
    // opkg/package changes during a mere post-apply read-only check.
    path,args:=filepath.Join(root,"sbin/xkeen"),[]string{"-restart"}
    if operation=="-status" {
        path=filepath.Join(root,"etc/init.d/S05xkeen")
        args=[]string{"status"}
        if observedPath(root,"etc/init.d/S05xkeen",false)!="PRESENT" {return false}
    } else if observedPath(root,"sbin/xkeen",false)!="PRESENT" {
        return false
    }
    info,err:=os.Lstat(path)
    if err!=nil || !info.Mode().IsRegular() || info.Mode()&0111==0 {return false}
    c,cancel:=context.WithTimeout(ctx,40*time.Second)
    defer cancel()
    cmd:=exec.CommandContext(c,path,args...)
    if operation=="-restart" {
        cmd.Env=append(os.Environ(),"XKEEN_FOREGROUND=1")
    }
    cmd.Stdout=io.Discard;cmd.Stderr=io.Discard;cmd.Stdin=nil
    cmd.WaitDelay=500*time.Millisecond
    return cmd.Run()==nil && c.Err()==nil
}

func nativeSubscriptionStatus(root string)nativeApplyResult{
    stopped:=applyStopped()
    files,err:=readNativeConfdir(root)
    if err!=nil{return stopped}
    state,err:=readNativeSubscriptionState(root,files)
    if err!=nil{return stopped}
    if _,ok:=files[subscriptionFragmentName];!ok{return nativeApplyResult{
        State:"NOT_CONFIGURED",Runtime:"NOT_VERIFIED",Applied:false,
    }}
    target,err:=chooseNativeRouteTarget(files,nativeRouteTarget{
        Balancer:state.Balancer,Selector:state.Selector,
    })
    if err!=nil{return stopped}
    var doc struct{Outbounds []struct{Tag string `json:"tag"`} `json:"outbounds"`}
    if json.Unmarshal(files[subscriptionFragmentName],&doc)!=nil ||
        len(doc.Outbounds)==0 || len(doc.Outbounds)>maxSubscriptionNodes{return stopped}
    for _,out:=range doc.Outbounds {
        if !strings.HasPrefix(out.Tag,target.Selector+"freenet-"){
            return stopped
        }
    }
    return nativeApplyResult{State:"CONFIG_APPLIED_NOT_EGRESS_VERIFIED",
        Runtime:"NOT_VERIFIED",Count:len(doc.Outbounds),Balancer:target.Balancer,Applied:true}
}
