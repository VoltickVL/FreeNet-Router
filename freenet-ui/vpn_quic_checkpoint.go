package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const visionQUICPendingDefault = "/opt/var/lib/freenet/vision-quic.pending"

type visionQUICReceipt struct {
	BeforeSHA string `json:"before_sha"`
	CandidateSHA string `json:"candidate_sha"`
	BeforeFlow string `json:"before_flow"`
	TargetFlow string `json:"target_flow"`
}

func visionQUICPendingPath() string {
	if v:=strings.TrimSpace(os.Getenv("FREENET_VISION_QUIC_PENDING")); v!="" {
		return v
	}
	return visionQUICPendingDefault
}

func visionQUICDigest(data []byte) string {
	h:=sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func visionQUICSyncFile(path string, data []byte) error {
	f,err:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
	if err!=nil {return err}
	_,writeErr:=f.Write(data)
	syncErr:=f.Sync()
	closeErr:=f.Close()
	if writeErr!=nil {return writeErr}
	if syncErr!=nil {return syncErr}
	return closeErr
}

func visionQUICSyncDir(path string) error {
	dir,err:=os.Open(path)
	if err!=nil{return errors.New("directory unavailable")}
	err=dir.Sync()
	_ = dir.Close()
	if err!=nil{return errors.New("directory sync failed")}
	return nil
}

func visionQUICSaveCheckpoint(before,candidate []byte,beforeFlow,target string) error {
	if len(before)==0||len(candidate)==0 || len(before)>visionQUICMaxConfigBytes||
		len(candidate)>visionQUICMaxConfigBytes || !visionQUICMode(beforeFlow)||
		!visionQUICMode(target) || beforeFlow==target{
		return errors.New("candidate checkpoint invalid")
	}
	path:=visionQUICPendingPath()
	if err:=os.MkdirAll(filepath.Dir(path),0700);err!=nil {
		return errors.New("checkpoint parent unavailable")
	}
	if err:=os.Mkdir(path,0700);err!=nil {
		return errors.New("another QUIC canary checkpoint exists")
	}
	receipt:=visionQUICReceipt{
		BeforeSHA:visionQUICDigest(before),CandidateSHA:visionQUICDigest(candidate),
		BeforeFlow:beforeFlow,TargetFlow:target,
	}
	raw,_:=json.Marshal(receipt)
	if visionQUICSyncFile(filepath.Join(path,"before.json"),before)!=nil ||
		visionQUICSyncFile(filepath.Join(path,"receipt.json"),raw)!=nil {
		// Keep even an incomplete checkpoint: unknown state is a hard STOP.
		return errors.New("checkpoint persist failed")
	}
	if visionQUICSyncDir(path)!=nil || visionQUICSyncDir(filepath.Dir(path))!=nil {
		return errors.New("checkpoint sync failed")
	}
	return nil
}

func visionQUICReadCheckpoint() ([]byte,visionQUICReceipt,error) {
	dir:=visionQUICPendingPath()
	info,err:=os.Lstat(dir)
	if err!=nil || !info.IsDir()||info.Mode().Perm()!=0700 {
		return nil,visionQUICReceipt{},errors.New("checkpoint not secure")
	}
	for _,name:=range []string{"before.json","receipt.json"}{
		info,err:=os.Lstat(filepath.Join(dir,name))
		if err!=nil || !info.Mode().IsRegular() || info.Mode().Perm()!=0600 ||
			info.Size()<=0 || info.Size()>visionQUICMaxConfigBytes {
			return nil,visionQUICReceipt{},errors.New("checkpoint file not secure")
		}
	}
	before,e:=os.ReadFile(filepath.Join(dir,"before.json"))
	if e!=nil {return nil,visionQUICReceipt{},errors.New("checkpoint unreadable")}
	receiptBytes,e:=os.ReadFile(filepath.Join(dir,"receipt.json"))
	if e!=nil {return nil,visionQUICReceipt{},errors.New("receipt unreadable")}
	var receipt visionQUICReceipt
	if json.Unmarshal(receiptBytes,&receipt)!=nil ||
		visionQUICDigest(before)!=receipt.BeforeSHA ||
		len(receipt.CandidateSHA)!=64 ||
		!visionQUICMode(receipt.BeforeFlow)||!visionQUICMode(receipt.TargetFlow) ||
		receipt.BeforeFlow==receipt.TargetFlow {
		return nil,visionQUICReceipt{},errors.New("checkpoint checksum invalid")
	}
	return before,receipt,nil
}

func visionQUICClearCheckpoint() error {
	return os.RemoveAll(visionQUICPendingPath())
}
