package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxRoutingConfigBodyBytes = 1024 << 10
	maxRoutingSectionBytes    = 384 << 10
	maxRoutingSourceFileBytes = 4 << 20
	defaultRoutingXrayBin     = "/opt/sbin/xray"
)

//go:embed web/routing-v2.js
var routingV2Asset []byte

type routingConfigResponse struct {
	Success        bool            `json:"success"`
	Mutation       string          `json:"mutation"`
	Routing        json.RawMessage `json:"routing"`
	Policy         json.RawMessage `json:"policy"`
	RoutingPresent bool            `json:"routing_present"`
	PolicyPresent  bool            `json:"policy_present"`
	RoutingSHA256  string          `json:"routing_sha256,omitempty"`
	PolicySHA256   string          `json:"policy_sha256,omitempty"`
	Error          string          `json:"error,omitempty"`
}

type routingValidateRequest struct {
	Routing json.RawMessage `json:"routing"`
	Policy  json.RawMessage `json:"policy"`
}

type routingValidateResponse struct {
	Success   bool            `json:"success"`
	Mutation  string          `json:"mutation"`
	XrayValid bool            `json:"xray_valid"`
	Routing   json.RawMessage `json:"routing,omitempty"`
	Policy    json.RawMessage `json:"policy,omitempty"`
	Error     string          `json:"error,omitempty"`
}

type routingApplyResponse struct {
	Success           bool              `json:"success"`
	Mutation          string            `json:"mutation"`
	XrayValid         bool              `json:"xray_valid"`
	Applied           bool              `json:"applied"`
	Rollback          string            `json:"rollback"`
	Snapshot          string            `json:"snapshot,omitempty"`
	Before            map[string]string `json:"before,omitempty"`
	After             map[string]string `json:"after,omitempty"`
	Result            string            `json:"result,omitempty"`
	Error             string            `json:"error,omitempty"`
}

type routingManagedBackup struct {
	Routing        []byte
	Policy         []byte
	RoutingPresent bool
	PolicyPresent  bool
	Snapshot        string
}

var (
	errRoutingXrayUnavailable = errors.New("routing Xray validator unavailable")
	errRoutingXrayInvalid     = errors.New("routing Xray candidate invalid")
	errRoutingXrayTimeout     = errors.New("routing Xray candidate validation timed out")
)

func registerRoutingConfigAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /routing-v2.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(routingV2Asset)
	})
	mux.HandleFunc("GET /api/routing/config", a.requireAuth(a.handleRoutingConfigGet))
	mux.HandleFunc("POST /api/routing/validate", a.requireAuth(a.handleRoutingConfigValidate))
	mux.HandleFunc("POST /api/routing/apply", a.requireAuth(a.handleRoutingConfigApply))
}

func (a *app) routingConfigDir() string {
	if dir := strings.TrimSpace(os.Getenv("FREENET_ROUTING_CONFIG_DIR")); dir != "" {
		return dir
	}
	out := strings.TrimSpace(a.cfg.OutPath)
	if out == "" {
		out = defaultOutPath
	}
	return filepath.Dir(out)
}

func (a *app) routingXrayBin() string {
	if bin := strings.TrimSpace(os.Getenv("FREENET_XRAY_BIN")); bin != "" {
		return bin
	}
	return defaultRoutingXrayBin
}

func (a *app) routingAssetDir() string {
	if dir := strings.TrimSpace(a.cfg.GeoDataDir); dir != "" {
		return dir
	}
	return defaultGeoDataAssetDir
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func normalizeRoutingSectionJSON(raw []byte, root string, requireRoot bool) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > maxRoutingSectionBytes {
		return nil, errors.New("routing section is empty or too large")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, errors.New("routing section must be a JSON object")
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, errors.New("routing section contains trailing JSON")
	}
	if requireRoot {
		nested, ok := obj[root]
		if !ok {
			return nil, errors.New("routing section has unexpected root")
		}
		var nestedObj map[string]json.RawMessage
		if err := json.Unmarshal(nested, &nestedObj); err != nil || nestedObj == nil {
			return nil, errors.New("routing section root must be a JSON object")
		}
	}
	formatted, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return nil, errors.New("cannot normalize routing section")
	}
	return json.RawMessage(formatted), nil
}

func readRoutingSection(path, root string) (json.RawMessage, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return json.RawMessage(`{}`), false, nil
	}
	if err != nil {
		return nil, false, err
	}
	normalized, err := normalizeRoutingSectionJSON(data, root, true)
	if err != nil {
		return nil, true, err
	}
	return normalized, true, nil
}

func (a *app) handleRoutingConfigGet(w http.ResponseWriter, _ *http.Request) {
	dir := a.routingConfigDir()
	routing, routingPresent, err := readRoutingSection(filepath.Join(dir, "05_routing.json"), "routing")
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, routingConfigResponse{Success: false, Mutation: "NONE", Routing: json.RawMessage(`{}`), Policy: json.RawMessage(`{}`), Error: "routing configuration is unavailable or invalid"})
		return
	}
	policy, policyPresent, err := readRoutingSection(filepath.Join(dir, "06_policy.json"), "policy")
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, routingConfigResponse{Success: false, Mutation: "NONE", Routing: json.RawMessage(`{}`), Policy: json.RawMessage(`{}`), Error: "policy configuration is unavailable or invalid"})
		return
	}
	resp := routingConfigResponse{
		Success: true, Mutation: "NONE", Routing: routing, Policy: policy,
		RoutingPresent: routingPresent, PolicyPresent: policyPresent,
	}
	if routingPresent {
		resp.RoutingSHA256 = sha256Hex(routing)
	}
	if policyPresent {
		resp.PolicySHA256 = sha256Hex(policy)
	}
	writeJSON(w, http.StatusOK, resp)
}

func copyRoutingCandidateBase(srcDir, dstDir string) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	copied := 0
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxRoutingSourceFileBytes {
			return errors.New("Xray config source is unsafe or too large")
		}
		data, err := os.ReadFile(filepath.Join(srcDir, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dstDir, entry.Name()), data, 0600); err != nil {
			return err
		}
		copied++
	}
	if copied == 0 {
		return errors.New("Xray config directory has no JSON files")
	}
	return nil
}

func (a *app) validateRoutingCandidate(parent context.Context, routing, policy json.RawMessage) error {
	tmpDir, err := os.MkdirTemp("", "freenet-routing-candidate.*")
	if err != nil {
		return errRoutingXrayUnavailable
	}
	defer os.RemoveAll(tmpDir)
	if err := copyRoutingCandidateBase(a.routingConfigDir(), tmpDir); err != nil {
		return errRoutingXrayUnavailable
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "05_routing.json"), routing, 0600); err != nil {
		return errRoutingXrayUnavailable
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "06_policy.json"), policy, 0600); err != nil {
		return errRoutingXrayUnavailable
	}

	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.routingXrayBin(), "run", "-test", "-confdir", tmpDir)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+a.routingAssetDir())
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return errRoutingXrayTimeout
		}
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return errRoutingXrayUnavailable
		}
		return errRoutingXrayInvalid
	}
	return nil
}

func decodeRoutingCandidateRequest(w http.ResponseWriter, r *http.Request, mutation string) (routingValidateRequest, bool) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, routingValidateResponse{Success: false, Mutation: mutation, Error: "cross-origin request rejected"})
		return routingValidateRequest{}, false
	}
	if ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))); !strings.HasPrefix(ct, "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, routingValidateResponse{Success: false, Mutation: mutation, Error: "application/json required"})
		return routingValidateRequest{}, false
	}
	body := http.MaxBytesReader(w, r.Body, maxRoutingConfigBodyBytes)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req routingValidateRequest
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, routingValidateResponse{Success: false, Mutation: mutation, Error: "invalid routing candidate request"})
		return routingValidateRequest{}, false
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, routingValidateResponse{Success: false, Mutation: mutation, Error: "invalid routing candidate request"})
		return routingValidateRequest{}, false
	}
	return req, true
}

func normalizeRoutingCandidate(req routingValidateRequest) (json.RawMessage, json.RawMessage, error) {
	routing, err := normalizeRoutingSectionJSON(req.Routing, "routing", true)
	if err != nil {
		return nil, nil, errors.New("05_routing.json candidate is not a valid routing object")
	}
	policy, err := normalizeRoutingSectionJSON(req.Policy, "policy", true)
	if err != nil {
		return nil, nil, errors.New("06_policy.json candidate is not a valid policy object")
	}
	return routing, policy, nil
}

func routingValidationFailureStatus(err error) (int, string) {
	message := "candidate Xray configuration validation failed"
	status := http.StatusUnprocessableEntity
	switch {
	case errors.Is(err, errRoutingXrayUnavailable):
		message = "Xray candidate validator is unavailable"
		status = http.StatusServiceUnavailable
	case errors.Is(err, errRoutingXrayTimeout):
		message = "Xray candidate validation timed out"
		status = http.StatusGatewayTimeout
	}
	return status, message
}

func (a *app) handleRoutingConfigValidate(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeRoutingCandidateRequest(w, r, "NONE")
	if !ok {
		return
	}
	routing, policy, err := normalizeRoutingCandidate(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, routingValidateResponse{Success: false, Mutation: "NONE", Error: err.Error()})
		return
	}

	if err := a.validateRoutingCandidate(r.Context(), routing, policy); err != nil {
		status, message := routingValidationFailureStatus(err)
		writeJSON(w, status, routingValidateResponse{Success: false, Mutation: "NONE", XrayValid: false, Error: message})
		return
	}
	writeJSON(w, http.StatusOK, routingValidateResponse{Success: true, Mutation: "NONE", XrayValid: true, Routing: routing, Policy: policy})
}

func readRoutingManagedBackup(dir string) (routingManagedBackup, error) {
	var b routingManagedBackup
	for _, item := range []struct {
		name    string
		target  *[]byte
		present *bool
	}{
		{name: "05_routing.json", target: &b.Routing, present: &b.RoutingPresent},
		{name: "06_policy.json", target: &b.Policy, present: &b.PolicyPresent},
	} {
		data, err := os.ReadFile(filepath.Join(dir, item.name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return b, err
		}
		*item.target = append([]byte{}, data...)
		*item.present = true
	}
	return b, nil
}

func createRoutingManagedSnapshot(dir string, b routingManagedBackup) (routingManagedBackup, error) {
	snapshot := filepath.Join(dir, ".freenet-backups", "routing-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.MkdirAll(snapshot, 0700); err != nil {
		return b, err
	}
	if b.RoutingPresent {
		if err := os.WriteFile(filepath.Join(snapshot, "05_routing.json"), b.Routing, 0600); err != nil {
			return b, err
		}
	} else if err := os.WriteFile(filepath.Join(snapshot, "05_routing.absent"), []byte("absent\n"), 0600); err != nil {
		return b, err
	}
	if b.PolicyPresent {
		if err := os.WriteFile(filepath.Join(snapshot, "06_policy.json"), b.Policy, 0600); err != nil {
			return b, err
		}
	} else if err := os.WriteFile(filepath.Join(snapshot, "06_policy.absent"), []byte("absent\n"), 0600); err != nil {
		return b, err
	}
	b.Snapshot = snapshot
	return b, nil
}

func atomicWriteRoutingManagedFile(dir, name string, data []byte) error {
	tmp, err := os.CreateTemp(dir, "."+name+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, filepath.Join(dir, name))
}

func writeRoutingManagedCandidate(dir string, routing, policy json.RawMessage) error {
	if err := atomicWriteRoutingManagedFile(dir, "05_routing.json", routing); err != nil {
		return err
	}
	if err := atomicWriteRoutingManagedFile(dir, "06_policy.json", policy); err != nil {
		return err
	}
	return nil
}

func restoreRoutingManagedBackup(dir string, b routingManagedBackup) error {
	if b.RoutingPresent {
		if err := atomicWriteRoutingManagedFile(dir, "05_routing.json", b.Routing); err != nil {
			return err
		}
	} else if err := os.Remove(filepath.Join(dir, "05_routing.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if b.PolicyPresent {
		if err := atomicWriteRoutingManagedFile(dir, "06_policy.json", b.Policy); err != nil {
			return err
		}
	} else if err := os.Remove(filepath.Join(dir, "06_policy.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func routingApplyHashes(before routingManagedBackup, routing, policy json.RawMessage) (map[string]string, map[string]string) {
	beforeHashes := map[string]string{}
	if before.RoutingPresent {
		beforeHashes["05_routing.json"] = sha256Hex(before.Routing)
	}
	if before.PolicyPresent {
		beforeHashes["06_policy.json"] = sha256Hex(before.Policy)
	}
	afterHashes := map[string]string{
		"05_routing.json": sha256Hex(routing),
		"06_policy.json":  sha256Hex(policy),
	}
	return beforeHashes, afterHashes
}

func (a *app) handleRoutingConfigApply(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeRoutingCandidateRequest(w, r, "NONE")
	if !ok {
		return
	}
	routing, policy, err := normalizeRoutingCandidate(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, routingApplyResponse{Success: false, Mutation: "NONE", Rollback: "NOT_NEEDED", Error: err.Error()})
		return
	}
	if err := a.validateRoutingCandidate(r.Context(), routing, policy); err != nil {
		status, message := routingValidationFailureStatus(err)
		writeJSON(w, status, routingApplyResponse{Success: false, Mutation: "NONE", XrayValid: false, Rollback: "NOT_NEEDED", Error: message})
		return
	}

	dir := a.routingConfigDir()
	backup, err := readRoutingManagedBackup(dir)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, routingApplyResponse{Success: false, Mutation: "NONE", XrayValid: true, Rollback: "NOT_NEEDED", Error: "routing managed files are unavailable"})
		return
	}
	backup, err = createRoutingManagedSnapshot(dir, backup)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, routingApplyResponse{Success: false, Mutation: "NONE", XrayValid: true, Rollback: "NOT_NEEDED", Error: "cannot create routing snapshot"})
		return
	}
	before, after := routingApplyHashes(backup, routing, policy)

	if err := writeRoutingManagedCandidate(dir, routing, policy); err != nil {
		_ = restoreRoutingManagedBackup(dir, backup)
		writeJSON(w, http.StatusInternalServerError, routingApplyResponse{Success: false, Mutation: "ROLLED_BACK", XrayValid: true, Rollback: "SUCCESS", Snapshot: backup.Snapshot, Before: before, After: after, Error: "routing apply write failed; backup restored"})
		return
	}

	if err := a.validateRoutingCandidate(r.Context(), routing, policy); err != nil {
		if restoreErr := restoreRoutingManagedBackup(dir, backup); restoreErr != nil {
			writeJSON(w, http.StatusInternalServerError, routingApplyResponse{Success: false, Mutation: "STOP", XrayValid: false, Applied: true, Rollback: "FAILED", Snapshot: backup.Snapshot, Before: before, After: after, Error: "post-apply validation failed and rollback restore failed; STOP"})
			return
		}
		rollbackRouting, rollbackPolicy, rollbackErr := routingRollbackCandidate(backup)
		if rollbackErr != nil || a.validateRoutingCandidate(r.Context(), rollbackRouting, rollbackPolicy) != nil {
			writeJSON(w, http.StatusInternalServerError, routingApplyResponse{Success: false, Mutation: "STOP", XrayValid: false, Applied: true, Rollback: "FAILED", Snapshot: backup.Snapshot, Before: before, After: after, Error: "post-apply validation failed and rollback validation failed; STOP"})
			return
		}
		writeJSON(w, http.StatusConflict, routingApplyResponse{Success: false, Mutation: "ROLLED_BACK", XrayValid: false, Applied: false, Rollback: "SUCCESS", Snapshot: backup.Snapshot, Before: before, After: after, Error: "post-apply validation failed; backup restored"})
		return
	}

	writeJSON(w, http.StatusOK, routingApplyResponse{Success: true, Mutation: "APPLIED", XrayValid: true, Applied: true, Rollback: "NOT_NEEDED", Snapshot: backup.Snapshot, Before: before, After: after, Result: "routing policy applied to managed sections"})
}

func routingRollbackCandidate(b routingManagedBackup) (json.RawMessage, json.RawMessage, error) {
	if !b.RoutingPresent || !b.PolicyPresent {
		return nil, nil, errors.New("rollback candidate is incomplete")
	}
	routing, err := normalizeRoutingSectionJSON(b.Routing, "routing", true)
	if err != nil {
		return nil, nil, err
	}
	policy, err := normalizeRoutingSectionJSON(b.Policy, "policy", true)
	if err != nil {
		return nil, nil, err
	}
	return routing, policy, nil
}
