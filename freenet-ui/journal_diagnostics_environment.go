package main

import (
 "crypto/sha256"
 "encoding/hex"
 "io"
 "os"
 "runtime"
 "strconv"
 "strings"
 "time"
)

type journalDiagnosticFileEvidence struct {
 Exists bool `json:"exists"`
 SizeBytes int64 `json:"size_bytes,omitempty"`
 ModifiedAt string `json:"modified_at,omitempty"`
 SHA256Prefix string `json:"sha256_prefix,omitempty"`
 HashSkipped string `json:"hash_skipped,omitempty"`
}

type journalDiagnosticEnvironment struct {
 OS string `json:"os"`
 Architecture string `json:"architecture"`
 GoRuntime string `json:"go_runtime"`
 ProcessPID int `json:"process_pid"`
 SystemUptimeSeconds int64 `json:"system_uptime_seconds,omitempty"`
 MemoryAvailableKB int64 `json:"memory_available_kb,omitempty"`
 FileEvidence map[string]journalDiagnosticFileEvidence `json:"file_evidence"`
}

const journalDiagnosticMaxHashBytes int64 = 2*1024*1024

func journalDiagnosticFileState(path string, withHash bool) journalDiagnosticFileEvidence {
 info,err:=os.Stat(path)
 if err!=nil || !info.Mode().IsRegular(){return journalDiagnosticFileEvidence{}}
 state:=journalDiagnosticFileEvidence{
  Exists:true,SizeBytes:info.Size(),ModifiedAt:info.ModTime().UTC().Format(time.RFC3339),
 }
 if !withHash{return state}
 if info.Size()>journalDiagnosticMaxHashBytes {state.HashSkipped="file exceeds 2 MiB";return state}
 f,err:=os.Open(path)
 if err!=nil{return state}
 defer f.Close()
 hash:=sha256.New()
 if _,err=io.Copy(hash,io.LimitReader(f,journalDiagnosticMaxHashBytes+1));err==nil{
  state.SHA256Prefix=hex.EncodeToString(hash.Sum(nil)[:12])
 }
 return state
}

func journalDiagnosticProcMetrics() (int64,int64) {
 uptime:=int64(0)
 available:=int64(0)
 if data,err:=os.ReadFile("/proc/uptime");err==nil{
  value:=strings.Fields(string(data))
  if len(value)>0 { if f,e:=strconv.ParseFloat(value[0],64);e==nil && f>=0{uptime=int64(f)} }
 }
 if data,err:=os.ReadFile("/proc/meminfo");err==nil{
  for _,line:=range strings.Split(string(data),"\n"){
   if strings.HasPrefix(line,"MemAvailable:"){
    fields:=strings.Fields(strings.TrimPrefix(line,"MemAvailable:"))
    if len(fields)>0 { available,_=strconv.ParseInt(fields[0],10,64) }
    break
   }
  }
 }
 return uptime,available
}

func (a *app) journalDiagnosticEnvironment() journalDiagnosticEnvironment {
 uptime,available:=journalDiagnosticProcMetrics()
 return journalDiagnosticEnvironment{
  OS:runtime.GOOS,Architecture:runtime.GOARCH,GoRuntime:runtime.Version(),ProcessPID:os.Getpid(),
  SystemUptimeSeconds:uptime,MemoryAvailableKB:available,
  FileEvidence:map[string]journalDiagnosticFileEvidence{
   "freenet_config":journalDiagnosticFileState(a.cfg.ConfigPath,true),
   "xray_outbounds":journalDiagnosticFileState(a.cfg.OutPath,true),
   "vpn_profile_filter":journalDiagnosticFileState(a.cfg.FilterPath,true),
   "xkeen_binary":journalDiagnosticFileState(a.cfg.XKeenPath,false),
   "automation_state":journalDiagnosticFileState(automationStatePath(),false),
   "watchdog_state":journalDiagnosticFileState(settingsV3StatePath(),false),
   "automation_journal":journalDiagnosticFileState(automationHistoryPath(),false),
   "settings_journal":journalDiagnosticFileState(settingsV3HistoryPath(),false),
   "technical_journal":journalDiagnosticFileState(technicalJournalPath(),false),
  },
 }
}
