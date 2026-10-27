package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const xkeenGuardedUpdateMaxDuration = 110 * time.Second

type xkeenUpdatePlan struct {
	Success       bool   `json:"success"`
	Ready         bool   `json:"ready"`
	Installed     string `json:"installed_version,omitempty"`
	Channel       string `json:"installed_channel,omitempty"`
	Target        string `json:"target_version,omitempty"`
	ExpectedSHA   string `json:"expected_sha256,omitempty"`
	State         string `json:"state"`
	Reason        string `json:"reason,omitempty"`
	Mutation      string `json:"mutation"`
	RuntimeChange string `json:"runtime_change"`
}

type xkeenUpdateApplyRequest struct {
	Installed string `json:"installed_version"`
	Target    string `json:"target_version"`
	SHA       string `json:"expected_sha256"`
}

var (
	xkeenUpdateGetAsset = func(ctx context.Context) (xkeenUpdateAsset, error) {
		return fetchOfficialXKeenUpdateAsset(ctx, newXKeenUpdateHTTPClient())
	}
	xkeenUpdateGetArchive = func(ctx context.Context, asset xkeenUpdateAsset, parent string) (string, error) {
		return downloadVerifiedXKeenArchive(ctx, newXKeenUpdateHTTPClient(), asset, parent)
	}
	// Tests can replace this witness. Production requires both strict process
	// inventory and an inode/GID-based native proof; a working UI is not enough.
	xkeenUpdateProductionReady = func(a *app) bool {
		if a == nil || a.manualRecoveryCoreState() != "running" {
			return false
		}
		binary, _, err := a.resolvedXrayCoreBinary()
		if err != nil {
			return false
		}
		configDir, err := a.xrayCoreLiveConfigDir()
		if err != nil {
			return false
		}
		inventory := scanXrayProcessInventory("/proc", configDir, binary)
		return inventory.Success && inventory.RestartReady &&
			xrayCoreNativeRuntimeProof(binary)
	}
)

func xkeenUpdateBusy(a *app) bool {
	if a == nil {
		return true
	}
	if xrayCoreOperationSnapshot().State == "RUNNING" ||
		xrayCoreOperationSnapshot().State == "STOP" ||
		vpnTransactionPending() || automationMutationBlockedState() {
		return true
	}
	held, _ := a.updateLockStatus()
	if held || a.selfUpdateStateAdmission() != nil {
		return true
	}
	if a.sem != nil && len(a.sem) > 0 {
		return true
	}
	return false
}

func xkeenUpdateEnoughSpace(dir string) bool {
	var stat syscall.Statfs_t
	if syscall.Statfs(dir, &stat) != nil {
		return false
	}
	return stat.Bavail > 0 && uint64(stat.Bavail)*uint64(stat.Bsize) > 24<<20
}

func (a *app) xkeenUpdatePrepare(ctx context.Context) (xkeenUpdatePlan, xkeenUpdateAsset) {
	p := xkeenUpdatePlan{Mutation: "NONE", State: "unverified", RuntimeChange: "NONE"}
	if a == nil || !xkeenLiveTreeReady(a.cfg.XKeenPath) {
		p.Reason = "xkeen-installation-unverified"
		return p, xkeenUpdateAsset{}
	}
	current, channel, err := readXKeenInstalledMetadata(a.cfg.XKeenPath)
	if err != nil {
		p.Reason = "xkeen-current-version-unverified"
		return p, xkeenUpdateAsset{}
	}
	p.Installed, p.Channel = current, channel
	if state := publicXKeenUpdateState(a.cfg.XKeenPath); state.State == "RUNNING" || state.State == "STOP" {
		p.Reason = "xkeen-prior-operation-not-terminal"
		return p, xkeenUpdateAsset{}
	}
	if xkeenUpdateBusy(a) {
		p.Reason = "freenet-operation-or-recovery-busy"
		return p, xkeenUpdateAsset{}
	}
	if channel != "stable" {
		p.Reason = "xkeen-installed-channel-not-stable"
		return p, xkeenUpdateAsset{}
	}
	if !xkeenUpdateEnoughSpace(filepath.Dir(a.cfg.XKeenPath)) {
		p.Reason = "xkeen-snapshot-space-unavailable"
		return p, xkeenUpdateAsset{}
	}
	asset, err := xkeenUpdateGetAsset(ctx)
	if err != nil {
		p.Reason = "xkeen-upstream-unverified"
		return p, xkeenUpdateAsset{}
	}
	p.Target, p.ExpectedSHA = asset.Version, asset.Digest
	comparison, ok := compareXKeenVersion(current, asset.Version)
	if !ok {
		p.Reason = "xkeen-comparison-unverified"
		return p, xkeenUpdateAsset{}
	}
	if comparison >= 0 {
		p.Success = true
		p.State = "up-to-date"
		p.Reason = "xkeen-no-upgrade-available"
		return p, asset
	}
	if !xkeenUpdateProductionReady(a) {
		p.State = "blocked"
		p.Reason = "xray-production-identity-unverified"
		return p, asset
	}
	p.Success, p.Ready, p.State = true, true, "ready"
	return p, asset
}

type xkeenInstalledSnapshot struct {
	Success bool   `json:"success"`
	Version string `json:"installed_version,omitempty"`
	Channel string `json:"installed_channel,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Mutation string `json:"mutation"`
}

func (a *app) handleXKeenInstalled(w http.ResponseWriter, _ *http.Request) {
	result := xkeenInstalledSnapshot{Mutation:"NONE"}
	if a == nil {
		result.Reason = "runtime-unavailable"
		writeJSON(w,http.StatusServiceUnavailable,result)
		return
	}
	v,ch,err := readXKeenInstalledMetadata(a.cfg.XKeenPath)
	if err != nil {
		result.Reason = "installed-version-unverified"
		writeJSON(w,http.StatusServiceUnavailable,result)
		return
	}
	result.Success,result.Version,result.Channel = true,v,ch
	writeJSON(w,http.StatusOK,result)
}

func (a *app) handleXKeenUpdatePlan(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	plan, _ := a.xkeenUpdatePrepare(ctx)
	writeJSON(w, http.StatusOK, plan)
}

func (a *app) handleXKeenUpdateState(w http.ResponseWriter, _ *http.Request) {
	if a == nil {
		writeJSON(w, http.StatusServiceUnavailable, xkeenUpdateStatus{State:"STOP",Rollback:"UNKNOWN",Reason:"runtime-unavailable",Mutation:"NONE"})
		return
	}
	writeJSON(w, http.StatusOK, publicXKeenUpdateState(a.cfg.XKeenPath))
}

func decodeXKeenApply(w http.ResponseWriter, r *http.Request) (xkeenUpdateApplyRequest, bool) {
	fail := func(status int, reason string) (xkeenUpdateApplyRequest, bool) {
		writeJSON(w, status, xkeenUpdateStatus{State:"FAILED",Rollback:"NOT_NEEDED",Reason:reason,Mutation:"NONE"})
		return xkeenUpdateApplyRequest{}, false
	}
	if !sameOrigin(r) {
		return fail(http.StatusForbidden, "cross-origin")
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))), "application/json") {
		return fail(http.StatusUnsupportedMediaType, "json-required")
	}
	var request xkeenUpdateApplyRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if dec.Decode(&request) != nil {
		return fail(http.StatusBadRequest, "invalid-request")
	}
	if _, ok := xkeenVersionParts(request.Installed); !ok {
		return fail(http.StatusBadRequest, "invalid-installed-version")
	}
	if _, ok := xkeenVersionParts(request.Target); !ok {
		return fail(http.StatusBadRequest, "invalid-target-version")
	}
	if len(request.SHA) != sha256.Size*2 {
		return fail(http.StatusBadRequest, "invalid-expected-hash")
	}
	if _, err := hex.DecodeString(request.SHA); err != nil {
		return fail(http.StatusBadRequest, "invalid-expected-hash")
	}
	return request, true
}

func (a *app) handleXKeenUpdateApply(w http.ResponseWriter, r *http.Request) {
	if a.mutationBlockedBySelfUpdate(w) {
		return
	}
	request, ok := decodeXKeenApply(w, r)
	if !ok {
		return
	}
	if xkeenUpdateBusy(a) {
		writeJSON(w, http.StatusConflict, xkeenUpdateStatus{State:"STOP",Rollback:"UNKNOWN",Reason:"operation-busy-or-unknown",Mutation:"NONE"})
		return
	}
	select {
	case a.sem <- struct{}{}:
	default:
		writeJSON(w, http.StatusConflict, xkeenUpdateStatus{State:"FAILED",Rollback:"NOT_NEEDED",Reason:"operation-busy",Mutation:"NONE"})
		return
	}
	releaseSem := func() { <-a.sem }
	lockRelease, lockErr := reserveXKeenCrossProcessLock(a.cfg.XKeenPath)
	if lockErr != nil {
		releaseSem()
		writeJSON(w, http.StatusConflict, xkeenUpdateStatus{State:"FAILED",Rollback:"NOT_NEEDED",Reason:"update-lock-unavailable",Mutation:"NONE"})
		return
	}
	if !xkeenLiveTreeReady(a.cfg.XKeenPath) {
		lockRelease()
		releaseSem()
		writeJSON(w, http.StatusConflict, xkeenUpdateStatus{State:"FAILED",Rollback:"NOT_NEEDED",Reason:"installation-unverified",Mutation:"NONE"})
		return
	}
	// a.sem is reserved, so the read-only gate must not treat our own slot as
	// a conflicting mutation. Temporarily use the same guards excluding sem.
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	asset, err := xkeenUpdateGetAsset(ctx)
	current, channel, installedErr := readXKeenInstalledMetadata(a.cfg.XKeenPath)
	comp, valid := compareXKeenVersion(current, request.Target)
	if err != nil || installedErr != nil || !valid || comp >= 0 || channel != "stable" ||
		request.Installed != current || request.Target != asset.Version ||
		!strings.EqualFold(request.SHA, asset.Digest) ||
		!xkeenUpdateProductionReady(a) || automationMutationBlockedState() ||
		vpnTransactionPending() {
		lockRelease()
		releaseSem()
		writeJSON(w, http.StatusConflict, xkeenUpdateStatus{State:"FAILED",Rollback:"NOT_NEEDED",Reason:"plan-expired-or-runtime-unverified",Mutation:"NONE"})
		return
	}
	old, stateErr := readXKeenUpdateDiskState(a.cfg.XKeenPath)
	if stateErr != nil || old.State == "RUNNING" || old.State == "STOP" {
		lockRelease()
		releaseSem()
		writeJSON(w, http.StatusConflict, xkeenUpdateStatus{State:"STOP",Rollback:"UNKNOWN",Reason:"previous-operation-unverified",Mutation:"NONE"})
		return
	}
	record := xkeenUpdateDiskState{State:"RUNNING",Rollback:"PENDING",Phase:"PREPARE",Previous:current,Target:asset.Version}
	if err := writeXKeenUpdateDiskState(a.cfg.XKeenPath, record); err != nil {
		lockRelease()
		releaseSem()
		writeJSON(w, http.StatusServiceUnavailable, xkeenUpdateStatus{State:"FAILED",Rollback:"NOT_NEEDED",Reason:"update-journal-unavailable",Mutation:"NONE"})
		return
	}
	setXKeenUpdateLocalActive(true)
	done := make(chan xkeenUpdateStatus, 1)
	go func() {
		defer func() {
			setXKeenUpdateLocalActive(false)
			lockRelease()
			releaseSem()
		}()
		done <- a.runXKeenGuardedUpdate(asset, current)
	}()
	select {
	case outcome := <-done:
		status := http.StatusOK
		if outcome.State != "SUCCESS" {
			status = http.StatusUnprocessableEntity
		}
		if outcome.State == "STOP" {
			status = http.StatusInternalServerError
		}
		writeJSON(w, status, outcome)
	case <-r.Context().Done():
		// Detached worker remains responsible for terminal receipt/rollback.
		return
	}
}

func (a *app) runXKeenGuardedUpdate(asset xkeenUpdateAsset, oldVersion string) xkeenUpdateStatus {
	binary := a.cfg.XKeenPath
	record := xkeenUpdateDiskState{State:"RUNNING",Previous:oldVersion,Target:asset.Version,Rollback:"PENDING",Phase:"DOWNLOAD"}
	finish := func(state, rollback, reason string) xkeenUpdateStatus {
		record.State, record.Rollback, record.Reason = state, rollback, reason
		record.Phase = "TERMINAL"
		if state != "STOP" {
			record.SnapshotDir = ""
		}
		if writeXKeenUpdateDiskState(binary, record) != nil {
			setAutomationMutationBlocked(true)
			return xkeenUpdateStatus{State:"STOP",Rollback:"UNKNOWN",Reason:"update-journal-unverified",Mutation:"NONE"}
		}
		if state == "STOP" {
			setAutomationMutationBlocked(true)
		}
		return publicXKeenUpdateState(binary)
	}
	ctx,cancel := context.WithTimeout(context.Background(), xkeenGuardedUpdateMaxDuration)
	defer cancel()
	archive, err := xkeenUpdateGetArchive(ctx, asset, filepath.Dir(binary))
	if err != nil {
		return finish("FAILED","NOT_NEEDED","download-or-integrity-failed")
	}
	defer os.Remove(archive)
	stage, err := os.MkdirTemp(filepath.Dir(binary), ".freenet-xkeen-stage-*")
	if err != nil {
		return finish("FAILED","NOT_NEEDED","staging-unavailable")
	}
	defer os.RemoveAll(stage)
	if extractVerifiedXKeenArchive(archive, stage, asset.Version) != nil {
		return finish("FAILED","NOT_NEEDED","candidate-invalid")
	}
	// Recheck production before the first filesystem mutation. An unknown
	// process, active VPN transaction or self-update means STOP without apply.
	if ctx.Err() != nil || !xkeenUpdateProductionReady(a) ||
		automationMutationBlockedState() || vpnTransactionPending() ||
		xrayCoreOperationSnapshot().State == "RUNNING" {
		return finish("FAILED","NOT_NEEDED","runtime-no-longer-confirmed")
	}
	installed, _, err := readXKeenInstalledMetadata(binary)
	if err != nil || installed != oldVersion {
		return finish("FAILED","NOT_NEEDED","installed-version-drift")
	}
	snapshot, err := os.MkdirTemp(filepath.Dir(binary), ".freenet-xkeen-snapshot-*")
	if err != nil {
		return finish("FAILED","NOT_NEEDED","snapshot-unavailable")
	}
	record.SnapshotDir, record.Phase = snapshot, "SNAPSHOT"
	if writeXKeenUpdateDiskState(binary, record) != nil {
		_ = os.RemoveAll(snapshot)
		return finish("FAILED","NOT_NEEDED","snapshot-journal-unavailable")
	}
	record.Phase = "APPLY"
	if writeXKeenUpdateDiskState(binary, record) != nil {
		return finish("STOP","UNKNOWN","update-journal-unverified")
	}
	applyErr := swapXKeenVersionedFiles(binary, stage, snapshot)
	if applyErr == nil {
		got, channel, err := readXKeenInstalledMetadata(binary)
		if err != nil || got != asset.Version || channel != "stable" ||
			!xkeenLiveTreeReady(binary) || !xkeenUpdateProductionReady(a) {
			applyErr = errors.New("xkeen-postcheck-failed")
		}
	}
	if applyErr != nil {
		record.Phase = "ROLLBACK"
		_ = writeXKeenUpdateDiskState(binary, record)
		if rollbackXKeenFiles(binary, snapshot, oldVersion) != nil ||
			!xkeenUpdateProductionReady(a) {
			return finish("STOP","FAILED","rollback-not-confirmed")
		}
		if os.RemoveAll(snapshot) != nil {
			return finish("STOP","UNKNOWN","rollback-cleanup-unverified")
		}
		record.SnapshotDir = ""
		return finish("ROLLED_BACK","SUCCESS","candidate-not-applied")
	}
	// Only the two XKeen script paths changed; production Xray PID, binary,
	// routing and configuration were not restarted or re-registered.
	record.SnapshotDir = ""
	record.Phase = "POSTCHECK"
	if writeXKeenUpdateDiskState(binary, record) != nil {
		return finish("STOP","UNKNOWN","receipt-unavailable")
	}
	if os.RemoveAll(snapshot) != nil {
		return finish("STOP","UNKNOWN","snapshot-cleanup-unverified")
	}
	return finish("SUCCESS","NOT_NEEDED","")
}

// Recovery never invokes upstream -uk or restarts Xray. It restores a valid
// original script/tree snapshot under the same transaction lock.
func (a *app) handleXKeenUpdateRecover(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) || !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		writeJSON(w, http.StatusForbidden, xkeenUpdateStatus{State:"STOP",Rollback:"UNKNOWN",Reason:"recovery-request-invalid",Mutation:"NONE"})
		return
	}
	var req struct { ExpectedVersion string `json:"expected_version"` }
	dec := json.NewDecoder(http.MaxBytesReader(w,r.Body,1024))
	dec.DisallowUnknownFields()
	if dec.Decode(&req) != nil {
		writeJSON(w,http.StatusBadRequest,xkeenUpdateStatus{State:"STOP",Rollback:"UNKNOWN",Reason:"recovery-request-invalid",Mutation:"NONE"})
		return
	}
	if _,ok := xkeenVersionParts(req.ExpectedVersion); !ok {
		writeJSON(w,http.StatusBadRequest,xkeenUpdateStatus{State:"STOP",Rollback:"UNKNOWN",Reason:"recovery-version-invalid",Mutation:"NONE"})
		return
	}
	select {
	case a.sem <- struct{}{}:
		defer func(){ <-a.sem }()
	default:
		writeJSON(w,http.StatusConflict,xkeenUpdateStatus{State:"STOP",Rollback:"UNKNOWN",Reason:"operation-busy",Mutation:"NONE"})
		return
	}
	release,err:=reserveXKeenCrossProcessLock(a.cfg.XKeenPath)
	if err!=nil {writeJSON(w,http.StatusConflict,xkeenUpdateStatus{State:"STOP",Rollback:"UNKNOWN",Reason:"update-lock-unavailable",Mutation:"NONE"});return}
	defer release()
	record,err:=readXKeenUpdateDiskState(a.cfg.XKeenPath)
	if err!=nil || req.ExpectedVersion!=record.Previous ||
		(record.State!="STOP" && record.State!="RUNNING") ||
		!xkeenSnapshotDirValid(a.cfg.XKeenPath,record.SnapshotDir) {
		writeJSON(w,http.StatusConflict,xkeenUpdateStatus{State:"STOP",Rollback:"UNKNOWN",Reason:"recovery-snapshot-unverified",Mutation:"NONE"})
		return
	}
	if err:=rollbackXKeenFiles(a.cfg.XKeenPath,record.SnapshotDir,record.Previous);err!=nil ||
		!xkeenUpdateProductionReady(a) {
		record.State,record.Rollback,record.Reason="STOP","FAILED","recovery-postcheck-unconfirmed"
		_ = writeXKeenUpdateDiskState(a.cfg.XKeenPath,record)
		setAutomationMutationBlocked(true)
		writeJSON(w,http.StatusConflict,publicXKeenUpdateState(a.cfg.XKeenPath))
		return
	}
	if os.RemoveAll(record.SnapshotDir)!=nil {
		record.State,record.Rollback,record.Reason="STOP","UNKNOWN","recovery-cleanup-unverified"
		_ = writeXKeenUpdateDiskState(a.cfg.XKeenPath,record)
		setAutomationMutationBlocked(true)
		writeJSON(w,http.StatusConflict,publicXKeenUpdateState(a.cfg.XKeenPath))
		return
	}
	record.State,record.Rollback,record.Reason,record.SnapshotDir="ROLLED_BACK","SUCCESS","",""
	if writeXKeenUpdateDiskState(a.cfg.XKeenPath,record)!=nil {
		setAutomationMutationBlocked(true)
		writeJSON(w,http.StatusServiceUnavailable,xkeenUpdateStatus{State:"STOP",Rollback:"UNKNOWN",Reason:"recovery-receipt-unverified",Mutation:"NONE"})
		return
	}
	writeJSON(w,http.StatusOK,publicXKeenUpdateState(a.cfg.XKeenPath))
}
