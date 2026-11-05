package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const visionQUICCanaryWindow = 35 * time.Second

type visionQUICTryRequest struct {
	Target string `json:"target"`
	Token string `json:"token"`
	IssuedAt int64 `json:"issued_at"`
	Acknowledge string `json:"acknowledge"`
}

type visionQUICTryResult struct {
	Success bool `json:"success"`
	Mutation string `json:"mutation"`
	Applied bool `json:"applied"`
	Rollback string `json:"rollback"`
	Status string `json:"status"`
	Note string `json:"note"`
}

func visionQUICLockOwned(path string) bool {
	b,err:=os.ReadFile(filepath.Join(path,"vision-canary-owner"))
	return err==nil && string(b)=="vision-quic-v1\n"
}

func visionQUICReleaseOwnedLock(path string) {
	if visionQUICLockOwned(path) { _=os.RemoveAll(path) }
}

func (a *app) visionQUICRestart(ctx context.Context) error {
	if ctx==nil || ctx.Err()!=nil {return errors.New("runtime restart unavailable")}
	out,err:=runCommand(ctx,a.cfg.XKeenPath,"-restart")
	_ = out // never log credential-bearing output
	if err!=nil {return errors.New("XKeen restart failed")}
	// Avoid equating an early process spawn with a stable core.
	for i:=0;i<4;i++ {
		if ctx.Err()!=nil {return errors.New("Xray restart interrupted")}
		if !a.liveXrayRunning() {time.Sleep(800*time.Millisecond);continue}
		time.Sleep(800*time.Millisecond)
		if a.liveXrayRunning() {return nil}
	}
	return errors.New("Xray did not recover after restart")
}

// Reverts an applied candidate on normal completion and after a FreeNet crash.
// Never clobber somebody else's changed config: different hashes are UNKNOWN.
func (a *app) visionQUICRollback(restart func(context.Context)error) error {
	return a.visionQUICRollbackWith(restart, a.liveXrayRunning)
}

func (a *app) visionQUICRollbackWith(restart func(context.Context)error, running func()bool) error {
	if a==nil || restart==nil || running==nil{return errors.New("rollback runtime unavailable")}
	before,receipt,err:=visionQUICReadCheckpoint()
	if err!=nil {return errors.New("rollback checkpoint invalid")}
	info,err:=os.Lstat(a.cfg.OutPath)
	if err!=nil || !info.Mode().IsRegular() {return errors.New("live outbound uncertain")}
	current,err:=os.ReadFile(a.cfg.OutPath)
	if err!=nil || len(current)>visionQUICMaxConfigBytes {return errors.New("live outbound unreadable")}
	actual:=visionQUICDigest(current)
	switch actual {
	case receipt.BeforeSHA:
		// A crash may have left the disk restored but runtime still running
		// the experimental candidate: restart to synchronize runtime.
	case receipt.CandidateSHA:
		if err:=atomicWrite(a.cfg.OutPath,before,info.Mode().Perm());err!=nil{
			return errors.New("rollback file restore failed")
		}
	default:
		return errors.New("live outbound changed outside canary")
	}
	ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
	defer cancel()
	if restart(ctx)!=nil || !running() {
		return errors.New("rollback runtime acceptance failed")
	}
	check,err:=os.ReadFile(a.cfg.OutPath)
	if err!=nil || visionQUICDigest(check)!=receipt.BeforeSHA {
		return errors.New("rollback bytes not verified")
	}
	if visionQUICClearCheckpoint()!=nil {return errors.New("checkpoint cleanup failed")}
	visionQUICReleaseOwnedLock(a.cfg.LockPath)
	return nil
}

func (a *app) recoverVisionQUICOnStartup() {
	if _,err:=os.Lstat(visionQUICPendingPath());os.IsNotExist(err) {
		return
	}
	if err:=a.visionQUICRollback(a.visionQUICRestart);err!=nil {
		setAutomationMutationBlocked(true)
		v3AppendEvent("vpn","failed","Незавершённый эксперимент QUIC: откат не подтверждён, дальнейшие изменения VPN заблокированы.")
		return
	}
	v3AppendEvent("vpn","success","Восстановлен исходный VPN после незавершённого QUIC-эксперимента.")
}

func (a *app) handleVisionQUICTry(w http.ResponseWriter,r *http.Request) {
	reply:=visionQUICTryResult{Mutation:"NONE",Rollback:"NOT_APPLIED",Status:"blocked"}
	if !sameOrigin(r) {
		reply.Status="cross-origin-rejected";writeJSON(w,http.StatusForbidden,reply);return
	}
	if ct:=r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct),"application/json"){
		reply.Status="invalid-content-type";writeJSON(w,http.StatusUnsupportedMediaType,reply);return
	}
	body:=http.MaxBytesReader(w,r.Body,2048)
	defer body.Close()
	var req visionQUICTryRequest
	dec:=json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if dec.Decode(&req)!=nil || req.Acknowledge!="temporary-canary-with-auto-rollback" ||
		!visionQUICMode(req.Target) {
		reply.Status="invalid-request";writeJSON(w,http.StatusBadRequest,reply);return
	}
	if a.mutationBlockedBySelfUpdate(w){return}
	select{case a.sem<-struct{}{}:defer func(){<-a.sem}();default:
		reply.Status="another-operation-running";writeJSON(w,http.StatusConflict,reply);return
	}
	releaseHealth,err:=acquireAutomationHealthLock()
	if err!=nil {reply.Status="health-watch-busy";writeJSON(w,http.StatusConflict,reply);return}
	defer releaseHealth()
	releaseBest,err:=acquireAutomationBestLock()
	if err!=nil {reply.Status="best-server-busy";writeJSON(w,http.StatusConflict,reply);return}
	defer releaseBest()
	if !a.visionQUICReadyWhileHeld() {
		reply.Status="runtime-or-transaction-unknown";writeJSON(w,http.StatusConflict,reply);return
	}
	before,candidate,current,err:=visionQUICReadActive(a,req.Target)
	if err!=nil || !visionQUICValidatePlan(before,req.Target,req.Token,req.IssuedAt,time.Now().UTC()){
		reply.Status="stale-or-uncertain-plan";writeJSON(w,http.StatusConflict,reply);return
	}
	preflightCtx,cancel:=context.WithTimeout(context.Background(),25*time.Second)
	err=a.visionQUICPreflightCandidate(preflightCtx,candidate)
	cancel()
	if err!=nil {
		reply.Status="candidate-preflight-failed";writeJSON(w,http.StatusConflict,reply);return
	}
	// Existing provider and updater writers recognize this lock path.
	if os.Mkdir(a.cfg.LockPath,0700)!=nil {
		reply.Status="provider-writer-busy";writeJSON(w,http.StatusConflict,reply);return
	}
	if os.WriteFile(filepath.Join(a.cfg.LockPath,"vision-canary-owner"),
		[]byte("vision-quic-v1\n"),0600)!=nil {
		_ = os.RemoveAll(a.cfg.LockPath)
		reply.Status="lock-owner-failed";writeJSON(w,http.StatusConflict,reply);return
	}
	// Close the race between the original read-only plan/preflight and the
	// acquired shared writer lock. A changed config must never be overwritten.
	lockedInfo,lockedErr:=os.Lstat(a.cfg.OutPath)
	lockedBytes,readErr:=os.ReadFile(a.cfg.OutPath)
	if lockedErr!=nil || readErr!=nil || !lockedInfo.Mode().IsRegular() ||
		visionQUICDigest(lockedBytes)!=visionQUICDigest(before) {
		visionQUICReleaseOwnedLock(a.cfg.LockPath)
		reply.Status="source-changed-before-apply"
		writeJSON(w,http.StatusConflict,reply);return
	}
	defer func() {
		// Never release the shared writer lock when rollback is UNKNOWN.
		// The on-disk checkpoint must remain a hard STOP for all writers.
		if _,err:=os.Lstat(visionQUICPendingPath());os.IsNotExist(err){
			visionQUICReleaseOwnedLock(a.cfg.LockPath)
		}
	}()
	if err:=visionQUICSaveCheckpoint(before,candidate,current,req.Target);err!=nil {
		// Leave incomplete receipt alone; all future mutations STOP until
		// state is reconciled. No active configuration has changed.
		setAutomationMutationBlocked(true)
		reply.Status="checkpoint-unknown";reply.Rollback="FAILED/UNKNOWN"
		writeJSON(w,http.StatusConflict,reply);return
	}
	v3AppendEvent("VPN","start","Запущено временное испытание QUIC на текущем сервере; исходный профиль сохранён для обязательного отката.")
	// Do not lose the rollback path when a client closes its browser.
	runCtx,cancelRun:=context.WithTimeout(context.Background(),100*time.Second)
	defer cancelRun()
	restore:=func() error {return a.visionQUICRollback(a.visionQUICRestart)}
	status:=http.StatusOK
	if err:=atomicWrite(a.cfg.OutPath,candidate,lockedInfo.Mode().Perm());err!=nil {
		reply.Status="candidate-write-failed"
		status=http.StatusBadGateway
		// This invocation has not requested a core restart. If the original
		// bytes survived unchanged, only discard the checkpoint; never cause
		// a needless network interruption merely to clean up a failed write.
		if live,e:=os.ReadFile(a.cfg.OutPath);e==nil &&
			visionQUICDigest(live)==visionQUICDigest(before) {
			if visionQUICClearCheckpoint()==nil {
				reply.Rollback="NOT_APPLIED"
				writeJSON(w,status,reply);return
			}
		}
	}else{
		reply.Mutation="APPLIED_TEMPORARILY";reply.Applied=true
		if a.visionQUICRestart(runCtx)!=nil {
			reply.Status="candidate-runtime-failed";status=http.StatusBadGateway
		}else{
			// Bounded observation window, WITHOUT promoting experimental flow.
			// Do not claim actual client Reels QoE from service availability.
			timer:=time.NewTimer(visionQUICCanaryWindow)
			select{case <-timer.C:case <-runCtx.Done():}
			timer.Stop()
			reply.Status="temporary-test-completed"
		}
	}
	if restoreErr:=restore();restoreErr!=nil {
		setAutomationMutationBlocked(true)
		reply.Success=false;reply.Rollback="FAILED/UNKNOWN";reply.Status="rollback-unconfirmed"
		reply.Note="Откат не подтверждён. Дальнейшие VPN-изменения остановлены."
		v3AppendEvent("VPN","failed","QUIC-испытание: откат не подтверждён; новые изменения VPN заблокированы.")
		writeJSON(w,http.StatusServiceUnavailable,reply);return
	}
	reply.Rollback="RESTORED";reply.Success=status==http.StatusOK
	reply.Note="Исходная конфигурация и процесс Xray восстановлены; реальное качество Reels не проверялось."
	if reply.Success{
		v3AppendEvent("VPN","success","Завершено временное QUIC-испытание; исходная конфигурация и процесс Xray восстановлены.")
	}else{
		v3AppendEvent("VPN","failed","QUIC-испытание не принято; конфигурация и процесс Xray восстановлены.")
	}
	writeJSON(w,status,reply)
}

func (a *app) visionQUICReadyWhileHeld() bool {
	// Caller owns the local, AUTO-health and Best-Server locks. Do not mistake
	// those locks for another transaction, but reject all other writers.
	if a.updateLockHeld() || automationMutationBlockedState() ||
		vpnTransactionPending() || xkeenUpdateLocalActive() || !a.liveXrayRunning(){
		return false
	}
	if _,err:=os.Lstat(a.cfg.LockPath);err==nil || !os.IsNotExist(err) {return false}
	if _,err:=os.Lstat(visionQUICPendingPath());err==nil || !os.IsNotExist(err) {return false}
	return true
}
