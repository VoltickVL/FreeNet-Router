package main

import (
	"crypto/sha256"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// The transaction doctor has no route to apply, unlock, restart or remove
// anything. In particular, even matching files do not prove that Xray and
// the old VPN are operational. A pending checkpoint remains a hard STOP.
type vpnTransactionDoctorResponse struct {
	Mode            string `json:"mode"`
	Mutation        string `json:"mutation"`
	Checkpoint      string `json:"checkpoint"`
	Outbound        string `json:"outbound"`
	Profile         string `json:"profile"`
	Filter          string `json:"filter"`
	FilesRestored   bool   `json:"files_restored"`
	XrayCore        string `json:"xray_core"`
	MutationBlocked bool   `json:"mutation_blocked"`
	Decision        string `json:"decision"`
}

// The path is private and is never part of any HTTP response or journal.
func vpnTransactionCheckpointPath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_VPN_TRANSACTION_DIR")); value != "" {
		return value
	}
	return "/opt/var/lib/freenet/vpn-transaction.pending"
}

func vpnTransactionProfilePath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_PROFILE_FILE")); value != "" {
		return value
	}
	return "/opt/etc/freenet/vpn_profile_name"
}

func vpnTransactionRegularFile(path string, maxSize int64) ([]byte, string) {
	stat, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "missing"
		}
		return nil, "unknown"
	}
	if !stat.Mode().IsRegular() || stat.Size() > maxSize || stat.Size() < 0 {
		return nil, "unsafe"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "unknown"
	}
	if int64(len(data)) > maxSize {
		return nil, "unsafe"
	}
	return data, "ok"
}

func vpnTransactionFileMatch(checkpoint, kind, live string) string {
	before := filepath.Join(checkpoint, kind+".before")
	absent := filepath.Join(checkpoint, kind+".absent")
	old, state := vpnTransactionRegularFile(before, 2*1024*1024)
	absence, absentState := vpnTransactionRegularFile(absent, 128)
	if state == "ok" && absentState == "ok" {
		return "invalid"
	}
	if state == "unknown" || state == "unsafe" || absentState == "unknown" || absentState == "unsafe" {
		return "unknown"
	}
	if state == "missing" && absentState == "missing" {
		return "invalid"
	}
	if absentState == "ok" {
		if len(absence) != 0 {
			return "invalid"
		}
		_, err := os.Lstat(live)
		if os.IsNotExist(err) {
			return "matched"
		}
		if err != nil {
			return "unknown"
		}
		return "different"
	}
	current, currentState := vpnTransactionRegularFile(live, 2*1024*1024)
	if currentState == "missing" {
		return "different"
	}
	if currentState != "ok" {
		return "unknown"
	}
	if sha256.Sum256(old) == sha256.Sum256(current) {
		return "matched"
	}
	return "different"
}

func (a *app) vpnTransactionDoctor() vpnTransactionDoctorResponse {
	resp := vpnTransactionDoctorResponse{
		Mode: "read-only", Mutation: "NONE", Checkpoint: "absent",
		Outbound: "not-applicable", Profile: "not-applicable", Filter: "not-applicable",
		MutationBlocked: automationMutationBlockedState(), Decision: "NO_CHECKPOINT",
		XrayCore: "stopped-or-unknown",
	}
	if a != nil && a.liveXrayRunning() {
		resp.XrayCore = "running"
	}
	checkpoint := vpnTransactionCheckpointPath()
	info, err := os.Lstat(checkpoint)
	if os.IsNotExist(err) {
		if resp.MutationBlocked {
			resp.Decision = "STOP_LEGACY_UNKNOWN"
		}
		return resp
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		resp.Checkpoint = "invalid"
		resp.Decision = "STOP_CHECKPOINT_INVALID"
		return resp
	}
	marker, state := vpnTransactionRegularFile(filepath.Join(checkpoint, "state"), 128)
	if state != "ok" || strings.TrimSpace(string(marker)) != "PENDING" {
		resp.Checkpoint = "invalid"
		resp.Decision = "STOP_CHECKPOINT_INVALID"
		return resp
	}
	resp.Checkpoint = "pending"
	// A previous manual recovery that died between snapshot and commit must
	// never be overwritten. Both before generations are protected evidence.
	if _, recoveryErr := os.Lstat(filepath.Join(checkpoint, "recovery.pending")); recoveryErr == nil || !os.IsNotExist(recoveryErr) {
		resp.Decision = "STOP_RECOVERY_INTERRUPTED"
		return resp
	}
	if a == nil {
		resp.Decision = "STOP_RUNTIME_UNKNOWN"
		return resp
	}
	resp.Outbound = vpnTransactionFileMatch(checkpoint, "out", a.cfg.OutPath)
	resp.Profile = vpnTransactionFileMatch(checkpoint, "profile", vpnTransactionProfilePath())
	resp.Filter = vpnTransactionFileMatch(checkpoint, "filter", a.cfg.FilterPath)
	resp.FilesRestored = resp.Outbound == "matched" && resp.Profile == "matched" && resp.Filter == "matched"
	resp.Decision = "STOP_FILES_UNRESOLVED"
	if resp.FilesRestored {
		resp.Decision = "STOP_VPN_UNVERIFIED"
		if resp.XrayCore != "running" {
			resp.Decision = "STOP_XRAY_NOT_CONFIRMED"
		}
	}
	// Never clear a checkpoint based on bytes matching: original VPN service
	// may already have been down when the failed replacement was attempted.
	return resp
}

// A pending checkpoint is not an indefinite manual-operation lock. It is
// eligible for a SEPARATE, explicitly confirmed recovery transaction only
// when the original checkpoint is coherent and the active core is known.
// "different" is allowed: it means the interrupted AUTO may have partially
// applied its candidate; the recovery helper snapshots the actual live files.
func vpnManualRecoveryEligible(resp vpnTransactionDoctorResponse) bool {
	if resp.Checkpoint != "pending" || resp.XrayCore != "running" {
		return false
	}
	if resp.Decision != "STOP_VPN_UNVERIFIED" && resp.Decision != "STOP_FILES_UNRESOLVED" {
		return false
	}
	for _, status := range []string{resp.Outbound, resp.Profile, resp.Filter} {
		if status != "matched" && status != "different" {
			return false
		}
	}
	return true
}

func registerVPNTransactionDoctor(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/vpn/transaction-doctor", a.requireAuth(a.handleVPNTransactionDoctor))
}

func (a *app) handleVPNTransactionDoctor(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, a.vpnTransactionDoctor())
}
