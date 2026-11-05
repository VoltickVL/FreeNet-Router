package main

import (
    "bytes"
    "os"
    "path/filepath"
    "testing"
)

func TestFreeNetAutoPoolCheckpointKeepsExactOriginalBytes(t *testing.T){
    t.Setenv("FREENET_AUTOVPN_POOL_STATE_DIR",filepath.Join(t.TempDir(),"autovpn"))
    c:=testFreeNetPoolCandidate(t)
    profiles:=map[string]string{"0123456789abcdef":"freenet-auto-node-1"}
    r,err:=freeNetAutoPoolCreateCheckpoint([]byte(freeNetAutoPoolCurrent),
        []byte(freeNetAutoPoolRouting),c,profiles)
    if err!=nil{t.Fatal(err)}
    if r.AfterOutSHA!=sha256Hex(c.Outbounds){t.Fatal("candidate hash changed")}
    saved,out,route,err:=freeNetAutoPoolReadCheckpoint(freeNetAutoPoolPending())
    if err!=nil||saved.APIAddress!=c.APIAddress||
       !bytes.Equal(out,[]byte(freeNetAutoPoolCurrent)) ||
       !bytes.Equal(route,[]byte(freeNetAutoPoolRouting)){
        t.Fatalf("checkpoint mismatch: %v",err)
    }
    if _,err:=freeNetAutoPoolCreateCheckpoint(out,route,c,profiles);err==nil{
        t.Fatal("existing pending receipt silently overwritten")
    }
    if err:=freeNetAutoPoolPromoteCheckpoint();err!=nil{t.Fatal(err)}
    if _,_,_,err:=freeNetAutoPoolReadCheckpoint(freeNetAutoPoolActive());err!=nil{
        t.Fatal("promoted receipt unavailable: ",err)
    }
    if !freeNetAutoPoolPathAbsent(freeNetAutoPoolPending()){t.Fatal("pending state left after activation")}
}
func TestFreeNetAutoPoolCheckpointFailsClosedWhenCorrupted(t *testing.T){
    t.Setenv("FREENET_AUTOVPN_POOL_STATE_DIR",filepath.Join(t.TempDir(),"autovpn"))
    c:=testFreeNetPoolCandidate(t)
    _,err:=freeNetAutoPoolCreateCheckpoint([]byte(freeNetAutoPoolCurrent),
        []byte(freeNetAutoPoolRouting),c,map[string]string{"0123456789abcdef":"freenet-auto-node-1"})
    if err!=nil{t.Fatal(err)}
    path:=filepath.Join(freeNetAutoPoolPending(),"before-outbounds.json")
    if err:=os.WriteFile(path,[]byte("modified"),0600);err!=nil{t.Fatal(err)}
    if _,_,_,err:=freeNetAutoPoolReadCheckpoint(freeNetAutoPoolPending());err==nil{
        t.Fatal("changed secret-bearing backup was accepted")
    }
    if err:=freeNetAutoPoolClearPending();err==nil{
        t.Fatal("unverified checkpoint silently deleted")
    }
}
