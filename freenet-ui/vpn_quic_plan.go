package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type visionQUICPlan struct {
	Ready bool `json:"ready"`
	Current string `json:"current,omitempty"`
	Target string `json:"target,omitempty"`
	Token string `json:"token,omitempty"`
	IssuedAt int64 `json:"issued_at,omitempty"`
	ExpiresInSeconds int `json:"expires_in_seconds,omitempty"`
	Mutation string `json:"mutation"`
	Status string `json:"status"`
}

const visionQUICPlanLifetime = 120 * time.Second

func visionQUICPlanBind(data []byte, target string, issuedAt int64) string {
	marker:=fmt.Sprintf("vision-udp443-canary-plan-v1:%d:%s:",issuedAt,target)
	h:=sha256.New()
	h.Write([]byte(marker))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func visionQUICValidatePlan(data []byte, target, token string, issuedAt int64, now time.Time) bool {
	if !visionQUICMode(target) || issuedAt<=0 || len(token)!=64 || now.IsZero(){return false}
	t:=time.Unix(issuedAt,0)
	if t.After(now.Add(3*time.Second)) || now.Sub(t)>visionQUICPlanLifetime {return false}
	return visionQUICPlanBind(data,target,issuedAt)==token
}

func visionQUICReadActive(a *app, target string) (original,candidate []byte,current string,err error) {
	if a==nil || !visionQUICMode(target) {return nil,nil,"",errors.New("unsupported mode")}
	info,e:=os.Lstat(a.cfg.OutPath)
	if e!=nil || !info.Mode().IsRegular() || info.Size()<=0 || info.Size()>visionQUICMaxConfigBytes {
		return nil,nil,"",errors.New("active outbound unavailable")
	}
	original,e=os.ReadFile(a.cfg.OutPath)
	if e!=nil || len(original)==0 || len(original)>visionQUICMaxConfigBytes {
		return nil,nil,"",errors.New("active outbound unreadable")
	}
	candidate,current,e=visionQUICCandidate(original,target)
	if e!=nil {return nil,nil,"",e}
	_,endpoint,ok:=readBestServerActiveOutbound(a.cfg.OutPath)
	live:=readBestServerCurrentEndpoint(a.cfg.OutPath)
	if !ok || endpoint=="" || live=="" || !endpointsEqual(endpoint,live) ||
		strings.TrimSpace(readBestServerCurrentFilter(a.cfg.FilterPath))=="" {
		return nil,nil,"",errors.New("current VPN identity not verified")
	}
	return original,candidate,current,nil
}

func (a *app) visionQUICPreflightCandidate(ctx context.Context, candidate []byte) error {
	binary:=strings.TrimSpace(os.Getenv("FREENET_XRAY_BIN"))
	if binary=="" {binary=defaultBestServerXrayPath}
	return visionQUICPreflight(ctx,binary,a.geoDataAssetDir(),
		filepath.Dir(a.cfg.OutPath),filepath.Base(a.cfg.OutPath),candidate)
}

func (a *app) visionQUICReady() bool {
	if a==nil || a.updateLockHeld() || automationMutationBlockedState() ||
		vpnTransactionPending() || xkeenUpdateLocalActive() ||
		!a.liveXrayRunning() || len(a.sem)>0 {
		return false
	}
	if _,err:=os.Lstat(a.cfg.LockPath);err==nil || !os.IsNotExist(err) {
		return false
	}
	if _,err:=os.Lstat(visionQUICPendingPath());err==nil || !os.IsNotExist(err) {
		return false
	}
	return true
}

func (a *app) handleVisionQUICPlan(w http.ResponseWriter,r *http.Request) {
	resp:=visionQUICPlan{Mutation:"NONE",Status:"unknown"}
	target:=strings.TrimSpace(r.URL.Query().Get("target"))
	if !visionQUICMode(target) {
		resp.Status="unsupported-target"
		writeJSON(w,http.StatusBadRequest,resp);return
	}
	if !a.visionQUICReady() {
		resp.Status="transaction-or-runtime-busy"
		writeJSON(w,http.StatusConflict,resp);return
	}
	original,candidate,previous,err:=visionQUICReadActive(a,target)
	if err!=nil {
		resp.Status="active-profile-uncertain"
		writeJSON(w,http.StatusConflict,resp);return
	}
	ctx,cancel:=context.WithTimeout(r.Context(),25*time.Second)
	defer cancel()
	if err:=a.visionQUICPreflightCandidate(ctx,candidate);err!=nil {
		resp.Status="candidate-preflight-failed"
		writeJSON(w,http.StatusConflict,resp);return
	}
	now:=time.Now().UTC()
	resp.Ready=true;resp.Current=previous;resp.Target=target
	resp.IssuedAt=now.Unix();resp.ExpiresInSeconds=int(visionQUICPlanLifetime.Seconds())
	resp.Token=visionQUICPlanBind(original,target,resp.IssuedAt)
	resp.Status="ready"
	writeJSON(w,http.StatusOK,resp)
}
