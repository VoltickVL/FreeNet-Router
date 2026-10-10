package main

import (
 "archive/zip"
 "context"
 "crypto/sha256"
 "crypto/subtle"
 "debug/elf"
 "encoding/hex"
 "errors"
 "fmt"
 "io"
 "net"
 "net/http"
 "os"
 "os/exec"
 "path"
 "path/filepath"
 "runtime"
 "strings"
 "syscall"
 "time"
)

const (
 tunnelPinnedRelease = "v0.0.16"
 tunnelReleaseBase = "https://github.com/openai/tunnel-client/releases/download/v0.0.16"
 tunnelMaxArchive = 56 << 20
 tunnelMaxBinary = 90 << 20
 tunnelMinFree = 190 << 20
)
var tunnelInstallSlot = make(chan struct{}, 1)

type tunnelInstallPlan struct {
 Success bool `json:"success"`
 State string `json:"state"`
 Version string `json:"version"`
 Architecture string `json:"architecture"`
 Destination string `json:"destination"`
 Ready bool `json:"ready"`
 ExpectedDelta string `json:"expected_delta"`
 NoDelta string `json:"expected_no_delta"`
 ExternalConnected bool `json:"external_connected"`
 Mutation string `json:"mutation"`
 Error string `json:"error,omitempty"`
}

// Override only by privileged service environment, never by HTTP parameters.
func tunnelInstallPath() string {
 if v:=os.Getenv("FREENET_TUNNEL_INSTALL_PATH"); v!="" { return v }
 return "/opt/bin/freenet-tunnel-client"
}

func tunnelArchiveName(goos, arch string) string {
 if goos!="linux" || (arch!="arm64" && arch!="amd64") {return ""}
 return "tunnel-client-"+tunnelPinnedRelease+"-linux-"+arch+".zip"
}

func tunnelPlanFor(goos,arch,destination string) tunnelInstallPlan {
 p:=tunnelInstallPlan{Success:true,State:"STOP",Version:tunnelPinnedRelease,Architecture:goos+"/"+arch,Destination:destination,Mutation:"NONE",ExternalConnected:false,NoDelta:"VPN, DNS, routing, Xray, подписки и активный профиль не изменяются"}
 if tunnelArchiveName(goos,arch)=="" {p.Error="Официального Linux tunnel-client для этой архитектуры нет";return p}
 if !filepath.IsAbs(destination)||destination=="/" {p.Error="Недопустимый путь установки";return p}
 info,err:=os.Lstat(destination)
 if err==nil {
  if !info.Mode().IsRegular(){p.Error="Неизвестное состояние файла (symlink/directory): STOP"}
  else{p.State="EXISTING_UNVERIFIED";p.Error="Файл уже существует. Без отдельной проверки и rollback перезапись запрещена"}
  return p
 }
 if !os.IsNotExist(err) {p.Error="Не удалось проверить место установки";return p}
 parent:=filepath.Dir(destination)
 dirInfo,err:=os.Stat(parent)
 if err!=nil||!dirInfo.IsDir(){p.Error="Каталог установки недоступен";return p}
 var space syscall.Statfs_t
 if err:=syscall.Statfs(parent,&space);err!=nil{p.Error="Не удалось определить свободное место";return p}
 if space.Bavail*uint64(space.Bsize)<tunnelMinFree{p.Error="Недостаточно свободного места для безопасной staging-установки (требуется 190 MiB)";return p}
 p.Ready=true
 p.State="READY_FOR_EXPLICIT_INSTALL"
 p.ExpectedDelta="Только новый проверенный /opt/bin/freenet-tunnel-client; ни один сетевой сервис автоматически не запускается"
 return p
}

func (a *app) handleTunnelInstallPlan(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Cache-Control","no-store")
 if len(r.URL.Query())!=0 {writeJSON(w,http.StatusBadRequest,map[string]any{"success":false,"mutation":"NONE","error":"Параметры запрещены"});return}
 writeJSON(w,http.StatusOK,tunnelPlanFor(runtime.GOOS,runtime.GOARCH,tunnelInstallPath()))
}

func tunnelHTTPClient() *http.Client {
 return &http.Client{
  Timeout:100*time.Second,
  CheckRedirect:func(r *http.Request,via []*http.Request)error{
   if len(via)>=5 || r.URL.Scheme!="https" {return errors.New("unsafe release redirect")}
   host:=strings.ToLower(r.URL.Hostname())
   if host!="github.com" && host!="release-assets.githubusercontent.com" && host!="objects.githubusercontent.com" {return errors.New("unsafe release host")}
   return nil
  },
 }
}
func tunnelReleaseDownload(ctx context.Context,client *http.Client,addr string,max int64)([]byte,error) {
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,addr,nil)
 if err!=nil{return nil,err}
 res,err:=client.Do(req)
 if err!=nil{return nil,err}
 defer res.Body.Close()
 if res.StatusCode!=200 || res.ContentLength>max {return nil,errors.New("release HTTP status/length rejected")}
 bytes,err:=io.ReadAll(io.LimitReader(res.Body,max+1))
 if err!=nil{return nil,err}
 if int64(len(bytes))>max{return nil,errors.New("oversized release asset")}
 return bytes,nil
}
func tunnelManifestDigest(manifest []byte,filename string)(string,error){
 var found string
 for _,line:=range strings.Split(string(manifest),"
"){
  parts:=strings.Fields(line)
  if len(parts)!=2 || strings.TrimPrefix(parts[1],"*")!=filename {continue}
  if found!=""{return "",errors.New("duplicate checksum entry")}
  raw,err:=hex.DecodeString(parts[0])
  if err!=nil||len(raw)!=32{return "",errors.New("invalid release checksum")}
  found=strings.ToLower(parts[0])
 }
 if found==""{return "",errors.New("checksum entry missing")}
 return found,nil
}
func tunnelSafeELF(filename,arch string) error {
 f,err:=elf.Open(filename)
 if err!=nil{return errors.New("candidate is not ELF")}
 defer f.Close()
 if f.Class!=elf.ELFCLASS64 || f.Type!=elf.ET_EXEC && f.Type!=elf.ET_DYN {
  return errors.New("wrong ELF class/type")
 }
 expected:=elf.EM_AARCH64
 if arch=="amd64"{expected=elf.EM_X86_64}
 if f.Machine!=expected{return errors.New("wrong ELF machine")}
 for _,program:=range f.Progs{
  if program.Type==elf.PT_INTERP {
   // Never allow an unverified libc loader path on an Entware router.
   return errors.New("dynamic loader requires a separate ABI review")
  }
 }
 return nil
}
func tunnelCandidateFromZip(zipName,arch,stage string)error{
 info,err:=os.Stat(zipName)
 if err!=nil{return err}
 reader,err:=zip.OpenReader(zipName)
 if err!=nil{return err}
 defer reader.Close()
 _=info
 count:=0
 for _,entry:=range reader.File{
  clean:=path.Clean(entry.Name)
  if strings.HasPrefix(clean,"../")||strings.HasPrefix(clean,"/")||strings.Contains(entry.Name,"\") {return errors.New("unsafe zip pathname")}
  if path.Base(clean)!="tunnel-client" {continue}
  count++
  if count!=1 || !entry.Mode().IsRegular() || entry.UncompressedSize64==0 || entry.UncompressedSize64>tunnelMaxBinary {return errors.New("unsafe tunnel executable entry")}
  src,err:=entry.Open()
  if err!=nil{return err}
  target,err:=os.OpenFile(stage,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0700)
  if err!=nil{src.Close();return err}
  size,copyErr:=io.Copy(target,io.LimitReader(src,tunnelMaxBinary+1))
  syncErr:=target.Sync()
  closeErr:=target.Close()
  src.Close()
  if copyErr!=nil||syncErr!=nil||closeErr!=nil||size<=0||size>tunnelMaxBinary||uint64(size)!=entry.UncompressedSize64{
   return errors.New("invalid tunnel binary extraction")
  }
 }
 if count!=1 {return errors.New("tunnel executable not unique")}
 return tunnelSafeELF(stage,arch)
}
func installTunnelCandidate(ctx context.Context,client *http.Client,base,arch,destination string) error{
 name:=tunnelArchiveName("linux",arch)
 if name==""{return errors.New("unsupported architecture")}
 dir:=filepath.Dir(destination)
 zipTemp,err:=os.CreateTemp(dir,".freenet-tunnel-archive-")
 if err!=nil{return err}
 zipPath:=zipTemp.Name()
 zipTemp.Close()
 defer os.Remove(zipPath)
 // Production accepts only the compiled-in fixed OpenAI release base.
 checksumBytes,err:=tunnelReleaseDownload(ctx,client,base+"/SHA256SUMS.txt",32<<10)
 if err!=nil{return errors.New("official checksum download failed")}
 digest,err:=tunnelManifestDigest(checksumBytes,name)
 if err!=nil{return err}
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,base+"/"+name,nil)
 if err!=nil{return err}
 resp,err:=client.Do(req)
 if err!=nil{return errors.New("official archive download failed")}
 defer resp.Body.Close()
 if resp.StatusCode!=200 || resp.ContentLength>tunnelMaxArchive {return errors.New("official archive status/size rejected")}
 file,err:=os.OpenFile(zipPath,os.O_WRONLY|os.O_TRUNC,0600)
 if err!=nil{return err}
 h:=sha256.New()
 size,copyErr:=io.Copy(io.MultiWriter(file,h),io.LimitReader(resp.Body,tunnelMaxArchive+1))
 syncErr:=file.Sync()
 closeErr:=file.Close()
 if copyErr!=nil||syncErr!=nil||closeErr!=nil||size<=0||size>tunnelMaxArchive{return errors.New("archive write failed")}
 actual:=hex.EncodeToString(h.Sum(nil))
 if subtle.ConstantTimeCompare([]byte(actual),[]byte(digest))!=1{return errors.New("official SHA256 mismatch")}
 stageFile,err:=os.CreateTemp(dir,".freenet-tunnel-candidate-")
 if err!=nil{return err}
 stage:=stageFile.Name()
 stageFile.Close()
 os.Remove(stage) // extraction creates the candidate O_EXCL
 defer os.Remove(stage)
 if err:=tunnelCandidateFromZip(zipPath,arch,stage);err!=nil{return err}
 cmdCtx,cancel:=context.WithTimeout(ctx,6*time.Second)
 defer cancel()
 out,err:=exec.CommandContext(cmdCtx,stage,"--version").Output()
 if err!=nil||!strings.Contains(strings.ToLower(string(out)),"tunnel")||!strings.Contains(string(out),strings.TrimPrefix(tunnelPinnedRelease,"v")){
  return errors.New("candidate execution/ABI/version check failed")
 }
 // Recheck immediately before commit; no overwrite or symlink following.
 if _,err:=os.Lstat(destination);!os.IsNotExist(err){return errors.New("destination changed during staging; STOP")}
 if err:=os.Chmod(stage,0700);err!=nil{return err}
 if err:=os.Rename(stage,destination);err!=nil{return err}
 if info,err:=os.Lstat(destination);err!=nil||!info.Mode().IsRegular()||info.Mode().Perm()!=0700 {
  if removeErr:=os.Remove(destination);removeErr!=nil{return errors.New("ROLLBACK UNKNOWN: installed candidate cannot be removed")}
  return errors.New("installed candidate validation failed; rollback success")
 }
 return nil
}
func (a *app) handleTunnelInstall(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("Cache-Control","no-store")
 if !connectorConfirm(w,r){return}
 if !connectorSecureProvisioning(r) {
  writeJSON(w,http.StatusForbidden,map[string]any{"success":false,"mutation":"NONE","error":"Установка только через подтверждённое HTTPS или loopback"})
  return
 }
 select{
 case tunnelInstallSlot<-struct{}{}:defer func(){<-tunnelInstallSlot}()
 default:
  writeJSON(w,http.StatusConflict,map[string]any{"success":false,"mutation":"NONE","error":"Операция уже выполняется"})
  return
 }
 dst:=tunnelInstallPath()
 plan:=tunnelPlanFor(runtime.GOOS,runtime.GOARCH,dst)
 if !plan.Ready {
  writeJSON(w,http.StatusConflict,map[string]any{"success":false,"mutation":"NONE","error":plan.Error})
  return
 }
 ctx,cancel:=context.WithTimeout(r.Context(),140*time.Second)
 defer cancel()
 if err:=installTunnelCandidate(ctx,tunnelHTTPClient(),tunnelReleaseBase,runtime.GOARCH,dst);err!=nil{
  writeJSON(w,http.StatusServiceUnavailable,map[string]any{"success":false,"mutation":"INSTALL_NOT_CONFIRMED","error":"Проверяемая установка остановлена: "+err.Error()})
  return
 }
 v3AppendEvent("connector","success","Проверенный официальный tunnel-client установлен без запуска; внешнее соединение отключено")
 writeJSON(w,http.StatusOK,map[string]any{"success":true,"mutation":"BINARY_ONLY","version":tunnelPinnedRelease,"state":"INSTALLED_NOT_STARTED","external_connected":false})
}
