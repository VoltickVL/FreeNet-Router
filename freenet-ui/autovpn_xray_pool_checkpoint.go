package main

import (
    "encoding/json"
    "errors"
    "os"
    "path/filepath"
    "strings"
    "time"
)

const (
    freeNetAutoPoolStateDefault="/opt/var/lib/freenet/autovpn-pool"
    freeNetAutoPoolReceiptSchema=1
)
type freeNetAutoPoolReceipt struct {
    Schema int `json:"schema"`
    CreatedAt string `json:"created_at"`
    BeforeOutSHA string `json:"before_out_sha"`
    BeforeRoutingSHA string `json:"before_routing_sha"`
    AfterOutSHA string `json:"after_out_sha"`
    AfterRoutingSHA string `json:"after_routing_sha"`
    APIAddress string `json:"api_address"`
    ProbeAddress string `json:"probe_address"`
    // Only profile IDs and outbound tags; never log or expose the original
    // VLESS URI, UUID, keys, subscription source or private configs.
    ProfileTags map[string]string `json:"profile_tags"`
}

func freeNetAutoPoolStateDir()string{
    if p:=strings.TrimSpace(os.Getenv("FREENET_AUTOVPN_POOL_STATE_DIR"));p!=""{
        return p
    }
    return freeNetAutoPoolStateDefault
}
func freeNetAutoPoolPending()string{return filepath.Join(freeNetAutoPoolStateDir(),"pending")}
func freeNetAutoPoolActive()string{return filepath.Join(freeNetAutoPoolStateDir(),"active")}
func freeNetAutoPoolPathAbsent(path string)bool{
    _,err:=os.Lstat(path)
    return os.IsNotExist(err)
}
func freeNetAutoPoolReceiptValid(r freeNetAutoPoolReceipt)bool{
    if r.Schema!=freeNetAutoPoolReceiptSchema || !freeNetAutoAPIAddressAllowed(r.APIAddress) ||
        !freeNetAutoAPIAddressAllowed(r.ProbeAddress) || r.APIAddress==r.ProbeAddress ||
        len(r.ProfileTags)==0 || len(r.ProfileTags)>=freeNetAutoPoolLimit {return false}
    for _,s:=range []string{r.BeforeOutSHA,r.BeforeRoutingSHA,r.AfterOutSHA,r.AfterRoutingSHA}{
        if !isHexLowerSHA(s){return false}
    }
    found:=map[string]bool{}
    for id,tag:=range r.ProfileTags{
        if !validProfileID(id) || !freeNetAutoTagAllowed(tag) || tag=="vless-reality"||found[tag]{return false}
        found[tag]=true
    }
    return true
}
func freeNetAutoPoolCreateCheckpoint(beforeOut,beforeRouting []byte,
    candidate freeNetAutoPoolCandidate,profileTags map[string]string)(freeNetAutoPoolReceipt,error){
    r:=freeNetAutoPoolReceipt{}
    if len(beforeOut)==0||len(beforeRouting)==0||len(candidate.Outbounds)==0||
      len(candidate.Routing)==0 {return r,errors.New("AutoVPN checkpoint content unavailable")}
    r=freeNetAutoPoolReceipt{
        Schema:freeNetAutoPoolReceiptSchema,
        CreatedAt:time.Now().UTC().Format(time.RFC3339),
        BeforeOutSHA:sha256Hex(beforeOut),
        BeforeRoutingSHA:sha256Hex(beforeRouting),
        AfterOutSHA:sha256Hex(candidate.Outbounds),
        AfterRoutingSHA:sha256Hex(candidate.Routing),
        APIAddress:candidate.APIAddress,
        ProbeAddress:candidate.ProbeAddress,
        ProfileTags:profileTags,
    }
    if !freeNetAutoPoolReceiptValid(r){return freeNetAutoPoolReceipt{},errors.New("AutoVPN checkpoint metadata invalid")}
    root:=freeNetAutoPoolStateDir()
    if err:=os.MkdirAll(root,0700);err!=nil{return freeNetAutoPoolReceipt{},errors.New("AutoVPN state directory unavailable")}
    info,err:=os.Lstat(root)
    if err!=nil||!info.IsDir()||info.Mode().Perm()&0077!=0{
        return freeNetAutoPoolReceipt{},errors.New("AutoVPN state directory unsafe")
    }
    if !freeNetAutoPoolPathAbsent(freeNetAutoPoolPending()) ||
       !freeNetAutoPoolPathAbsent(freeNetAutoPoolActive()){
        return freeNetAutoPoolReceipt{},errors.New("AutoVPN pool checkpoint or active state already exists")
    }
    if err:=os.Mkdir(freeNetAutoPoolPending(),0700);err!=nil{
        return freeNetAutoPoolReceipt{},errors.New("AutoVPN checkpoint writer busy")
    }
    // A partially written directory is left as a STOP, never silently
    // discarded, even if the active Xray files have not yet changed.
    raw,_:=json.Marshal(r)
    for _,entry:=range []struct{name string;content []byte}{
        {"before-outbounds.json",beforeOut},
        {"before-routing.json",beforeRouting},
        {"receipt.json",raw},
    }{
        if visionQUICSyncFile(filepath.Join(freeNetAutoPoolPending(),entry.name),entry.content)!=nil{
            return freeNetAutoPoolReceipt{},errors.New("AutoVPN checkpoint persistence failed")
        }
    }
    if visionQUICSyncDir(freeNetAutoPoolPending())!=nil ||
       visionQUICSyncDir(root)!=nil ||
       visionQUICSyncDir(filepath.Dir(root))!=nil {
        return freeNetAutoPoolReceipt{},errors.New("AutoVPN checkpoint durability unverified")
    }
    return r,nil
}
func freeNetAutoPoolReadCheckpoint(dir string)(freeNetAutoPoolReceipt,[]byte,[]byte,error){
    var receipt freeNetAutoPoolReceipt
    info,err:=os.Lstat(dir)
    if err!=nil||!info.IsDir()||info.Mode().Perm()&0077!=0 {
        return receipt,nil,nil,errors.New("AutoVPN state folder unavailable")
    }
    files:=[][]byte{}
    for _,name:=range []string{"receipt.json","before-outbounds.json","before-routing.json"}{
        path:=filepath.Join(dir,name)
        st,e:=os.Lstat(path)
        if e!=nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077!=0 ||
           st.Size()==0||st.Size()>visionQUICMaxConfigBytes{
            return receipt,nil,nil,errors.New("AutoVPN checkpoint file unsafe")
        }
        data,e:=os.ReadFile(path)
        if e!=nil||int64(len(data))!=st.Size(){
            return receipt,nil,nil,errors.New("AutoVPN checkpoint unreadable")
        }
        files=append(files,data)
    }
    if json.Unmarshal(files[0],&receipt)!=nil||!freeNetAutoPoolReceiptValid(receipt)||
       receipt.BeforeOutSHA!=sha256Hex(files[1]) ||
       receipt.BeforeRoutingSHA!=sha256Hex(files[2]){
        return freeNetAutoPoolReceipt{},nil,nil,errors.New("AutoVPN checkpoint identity invalid")
    }
    return receipt,files[1],files[2],nil
}
func freeNetAutoPoolPromoteCheckpoint()error{
    if !freeNetAutoPoolPathAbsent(freeNetAutoPoolActive()){
        return errors.New("active AutoVPN pool already exists")
    }
    if _,_,_,err:=freeNetAutoPoolReadCheckpoint(freeNetAutoPoolPending());err!=nil{return err}
    if err:=os.Rename(freeNetAutoPoolPending(),freeNetAutoPoolActive());err!=nil{
        return errors.New("AutoVPN checkpoint activation failed")
    }
    return visionQUICSyncDir(freeNetAutoPoolStateDir())
}
func freeNetAutoPoolClearPending()error{
    pending:=freeNetAutoPoolPending()
    if freeNetAutoPoolPathAbsent(pending){return nil}
    if _,_,_,err:=freeNetAutoPoolReadCheckpoint(pending);err!=nil{return err}
    if err:=os.RemoveAll(pending);err!=nil{return errors.New("AutoVPN checkpoint cleanup failed")}
    return visionQUICSyncDir(freeNetAutoPoolStateDir())
}
