package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	maxConfigStudioBodyBytes = 1024 << 10
	maxConfigStudioFileBytes = 384 << 10
	defaultXKeenConfigDir     = "/opt/etc/xkeen"
)

type configStudioTab struct {
	Name      string          `json:"name"`
	Kind      string          `json:"kind"`
	Access    string          `json:"access"`
	Present   bool            `json:"present"`
	Size      int64           `json:"size,omitempty"`
	SHA256    string          `json:"sha256,omitempty"`
	Roots     []string        `json:"roots,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	Text      string          `json:"text,omitempty"`
	Mutation  string          `json:"mutation"`
}

type configStudioXrayStatus struct {
	Online  bool   `json:"online"`
	Version string `json:"version,omitempty"`
}

type configStudioResponse struct {
	Success  bool                   `json:"success"`
	Mutation string                 `json:"mutation"`
	Tabs     []configStudioTab      `json:"tabs"`
	Xray     configStudioXrayStatus `json:"xray"`
	Error    string                 `json:"error,omitempty"`
}

type configStudioCandidateRequest struct {
	File    string          `json:"file"`
	Content json.RawMessage `json:"content"`
}

type configStudioMutationResponse struct {
	Success     bool   `json:"success"`
	Mutation    string `json:"mutation"`
	XrayValid   bool   `json:"xray_valid"`
	Applied     bool   `json:"applied"`
	Rollback    string `json:"rollback"`
	Snapshot    string `json:"snapshot,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	CoreRestart bool   `json:"core_restart"`
	Result      string `json:"result,omitempty"`
	Error       string `json:"error,omitempty"`
}

type configStudioBackup struct {
	Data     []byte
	Present  bool
	Snapshot string
}

func registerConfigStudioAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/config-studio", a.requireAuth(a.handleConfigStudioGet))
	mux.HandleFunc("POST /api/config-studio/validate", a.requireAuth(a.handleConfigStudioValidate))
	mux.HandleFunc("POST /api/config-studio/apply", a.requireAuth(a.handleConfigStudioApply))
}

func configStudioEditableRoot(name string) (string, bool) {
	switch name {
	case "01_log.json":
		return "log", true
	case "02_dns.json":
		return "dns", true
	case "03_inbounds.json":
		return "inbounds", true
	case "04_outbounds.json":
		return "outbounds", true
	default:
		return "", false
	}
}

func configStudioRoutingManaged(name string) (string, bool) {
	switch name {
	case "05_routing.json":
		return "routing", true
	case "06_policy.json":
		return "policy", true
	default:
		return "", false
	}
}

func readConfigStudioJSONFile(dir, name, access string) configStudioTab {
	tab := configStudioTab{Name: strings.TrimSuffix(name, filepath.Ext(name)), Kind: "json", Access: access, Mutation: "NONE"}
	path := filepath.Join(dir, name)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return tab
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxRoutingSourceFileBytes {
		return tab
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tab
	}
	tab.Present = true
	tab.Size = info.Size()
	tab.SHA256 = sha256Hex(data)

	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) == nil {
		for key := range obj {
			tab.Roots = append(tab.Roots, key)
		}
		sort.Strings(tab.Roots)
	}

	var root string
	var ok bool
	if root, ok = configStudioEditableRoot(name); !ok {
		root, ok = configStudioRoutingManaged(name)
	}
	if !ok {
		return tab
	}
	normalized, err := normalizeConfigStudioJSON(data, root)
	if err != nil {
		return tab
	}
	tab.Content = normalized
	return tab
}

func readConfigStudioListFile(name string) configStudioTab {
	tab := configStudioTab{Name: strings.TrimSuffix(name, filepath.Ext(name)), Kind: "list", Access: "read-only", Mutation: "NONE"}
	path := filepath.Join(defaultXKeenConfigDir, name)
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return tab
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxConfigStudioFileBytes {
		return tab
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tab
	}
	tab.Present = true
	tab.Size = info.Size()
	tab.SHA256 = sha256Hex(data)
	tab.Text = string(data)
	return tab
}

func (a *app) configStudioXrayStatus(parent context.Context) configStudioXrayStatus {
	status := configStudioXrayStatus{Online: processRunning("xray")}
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.routingXrayBin(), "version")
	out, err := cmd.Output()
	if err != nil || ctx.Err() != nil {
		return status
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if len(line) > 120 {
		line = line[:120]
	}
	for _, prefix := range []string{"Xray ", "Xray-core ", "Xray-core"} {
		line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
	}
	status.Version = line
	return status
}

func (a *app) handleConfigStudioGet(w http.ResponseWriter, r *http.Request) {
	dir := a.routingConfigDir()
	tabs := []configStudioTab{
		readConfigStudioJSONFile(dir, "01_log.json", "editable"),
		readConfigStudioJSONFile(dir, "02_dns.json", "editable"),
		readConfigStudioJSONFile(dir, "03_inbounds.json", "editable"),
		readConfigStudioJSONFile(dir, "04_outbounds.json", "editable"),
		readConfigStudioJSONFile(dir, "05_routing.json", "routing-managed"),
		readConfigStudioJSONFile(dir, "06_policy.json", "routing-managed"),
		readConfigStudioListFile("ip_exclude.lst"),
		readConfigStudioListFile("port_exclude.lst"),
		readConfigStudioListFile("port_proxying.lst"),
	}
	writeJSON(w, http.StatusOK, configStudioResponse{Success: true, Mutation: "NONE", Tabs: tabs, Xray: a.configStudioXrayStatus(r.Context())})
}

func normalizeConfigStudioJSON(raw []byte, root string) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > maxConfigStudioFileBytes {
		return nil, errors.New("config file is empty or too large")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var obj map[string]json.RawMessage
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, errors.New("config file must be a JSON object")
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, errors.New("config file contains trailing JSON")
	}
	nested, ok := obj[root]
	if !ok {
		return nil, errors.New("config file has unexpected root")
	}
	if root == "inbounds" || root == "outbounds" {
		var nestedArray []json.RawMessage
		if err := json.Unmarshal(nested, &nestedArray); err != nil || nestedArray == nil {
			return nil, errors.New("config root must be a JSON array")
		}
	} else {
		var nestedObj map[string]json.RawMessage
		if err := json.Unmarshal(nested, &nestedObj); err != nil || nestedObj == nil {
			return nil, errors.New("config root must be a JSON object")
		}
	}
	formatted, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return nil, errors.New("cannot normalize config file")
	}
	formatted = append(formatted, '\n')
	return json.RawMessage(formatted), nil
}

func decodeConfigStudioCandidate(w http.ResponseWriter, r *http.Request, mutation string) (string, json.RawMessage, bool) {
	failure := func(status int, message string) (string, json.RawMessage, bool) {
		writeJSON(w, status, configStudioMutationResponse{Success: false, Mutation: mutation, Rollback: "NOT_NEEDED", CoreRestart: false, Error: message})
		return "", nil, false
	}
	if !sameOrigin(r) {
		return failure(http.StatusForbidden, "cross-origin request rejected")
	}
	if ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))); !strings.HasPrefix(ct, "application/json") {
		return failure(http.StatusUnsupportedMediaType, "application/json required")
	}
	body := http.MaxBytesReader(w, r.Body, maxConfigStudioBodyBytes)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req configStudioCandidateRequest
	if err := dec.Decode(&req); err != nil {
		return failure(http.StatusBadRequest, "invalid config candidate request")
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return failure(http.StatusBadRequest, "invalid config candidate request")
	}
	root, ok := configStudioEditableRoot(strings.TrimSpace(req.File))
	if !ok {
		return failure(http.StatusBadRequest, "config file is not editable through this endpoint")
	}
	normalized, err := normalizeConfigStudioJSON(req.Content, root)
	if err != nil {
		return failure(http.StatusBadRequest, err.Error())
	}
	return strings.TrimSpace(req.File), normalized, true
}

func (a *app) validateConfigStudioCandidate(parent context.Context, name string, content json.RawMessage) error {
	tmpDir, err := os.MkdirTemp("", "freenet-config-studio-candidate.*")
	if err != nil {
		return errRoutingXrayUnavailable
	}
	defer os.RemoveAll(tmpDir)
	if err := copyRoutingCandidateBase(a.routingConfigDir(), tmpDir); err != nil {
		return errRoutingXrayUnavailable
	}
	if err := os.WriteFile(filepath.Join(tmpDir, name), content, 0600); err != nil {
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

func (a *app) handleConfigStudioValidate(w http.ResponseWriter, r *http.Request) {
	name, content, ok := decodeConfigStudioCandidate(w, r, "NONE")
	if !ok {
		return
	}
	if err := a.validateConfigStudioCandidate(r.Context(), name, content); err != nil {
		status, message := routingValidationFailureStatus(err)
		writeJSON(w, status, configStudioMutationResponse{Success: false, Mutation: "NONE", XrayValid: false, Rollback: "NOT_NEEDED", CoreRestart: false, Error: message})
		return
	}
	writeJSON(w, http.StatusOK, configStudioMutationResponse{Success: true, Mutation: "NONE", XrayValid: true, Rollback: "NOT_NEEDED", SHA256: sha256Hex(content), CoreRestart: false, Result: "candidate is valid; live config unchanged"})
}

func createConfigStudioSnapshot(dir, name string, backup configStudioBackup) (configStudioBackup, error) {
	snapshot := filepath.Join(dir, ".freenet-backups", "config-studio-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.MkdirAll(snapshot, 0700); err != nil {
		return backup, err
	}
	if backup.Present {
		if err := os.WriteFile(filepath.Join(snapshot, name), backup.Data, 0600); err != nil {
			return backup, err
		}
	} else if err := os.WriteFile(filepath.Join(snapshot, name+".absent"), []byte("absent\n"), 0600); err != nil {
		return backup, err
	}
	backup.Snapshot = snapshot
	return backup, nil
}

func readConfigStudioBackup(dir, name string) (configStudioBackup, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return configStudioBackup{}, nil
	}
	if err != nil {
		return configStudioBackup{}, err
	}
	return configStudioBackup{Data: data, Present: true}, nil
}

func restoreConfigStudioBackup(dir, name string, backup configStudioBackup) error {
	path := filepath.Join(dir, name)
	if backup.Present {
		return atomicWriteRoutingManagedFile(dir, name, backup.Data)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (a *app) validateConfigStudioLive(parent context.Context) error {
	tmpDir, err := os.MkdirTemp("", "freenet-config-studio-live.*")
	if err != nil {
		return errRoutingXrayUnavailable
	}
	defer os.RemoveAll(tmpDir)
	if err := copyRoutingCandidateBase(a.routingConfigDir(), tmpDir); err != nil {
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
		return errRoutingXrayInvalid
	}
	return nil
}

func (a *app) handleConfigStudioApply(w http.ResponseWriter, r *http.Request) {
	name, content, ok := decodeConfigStudioCandidate(w, r, "PENDING")
	if !ok {
		return
	}
	if err := a.validateConfigStudioCandidate(r.Context(), name, content); err != nil {
		status, message := routingValidationFailureStatus(err)
		writeJSON(w, status, configStudioMutationResponse{Success: false, Mutation: "NONE", XrayValid: false, Rollback: "NOT_NEEDED", CoreRestart: false, Error: message})
		return
	}

	releaseMutation, lockErr := acquireVPNMutationLock()
	if lockErr != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(lockErr, errVPNMutationBusy) {
			status = http.StatusConflict
		}
		writeJSON(w, status, configStudioMutationResponse{Success: false, Mutation: "NONE", XrayValid: true, Rollback: "NOT_APPLIED", CoreRestart: false, Error: vpnMutationLockMessage(lockErr)})
		return
	}
	defer releaseMutation()

	dir := a.routingConfigDir()
	backup, err := readConfigStudioBackup(dir, name)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, configStudioMutationResponse{Success: false, Mutation: "NONE", XrayValid: true, Rollback: "NOT_NEEDED", CoreRestart: false, Error: "cannot read live config for snapshot"})
		return
	}
	backup, err = createConfigStudioSnapshot(dir, name, backup)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, configStudioMutationResponse{Success: false, Mutation: "NONE", XrayValid: true, Rollback: "NOT_NEEDED", CoreRestart: false, Error: "cannot create config snapshot"})
		return
	}
	if err := atomicWriteRoutingManagedFile(dir, name, content); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, configStudioMutationResponse{Success: false, Mutation: "FAILED", XrayValid: true, Rollback: "NOT_NEEDED", Snapshot: backup.Snapshot, CoreRestart: false, Error: "atomic config write failed"})
		return
	}
	if err := a.validateConfigStudioLive(r.Context()); err == nil {
		writeJSON(w, http.StatusOK, configStudioMutationResponse{Success: true, Mutation: "APPLIED", XrayValid: true, Applied: true, Rollback: "NOT_NEEDED", Snapshot: backup.Snapshot, SHA256: sha256Hex(content), CoreRestart: false, Result: "config written and post-validated; Xray restart was not performed"})
		return
	}

	if err := restoreConfigStudioBackup(dir, name, backup); err != nil {
		writeJSON(w, http.StatusInternalServerError, configStudioMutationResponse{Success: false, Mutation: "STOP", XrayValid: false, Applied: false, Rollback: "FAILED", Snapshot: backup.Snapshot, CoreRestart: false, Error: "post-apply validation failed and rollback write failed"})
		return
	}
	if err := a.validateConfigStudioLive(r.Context()); err != nil {
		writeJSON(w, http.StatusInternalServerError, configStudioMutationResponse{Success: false, Mutation: "STOP", XrayValid: false, Applied: false, Rollback: "FAILED", Snapshot: backup.Snapshot, CoreRestart: false, Error: "post-apply validation failed and rollback validation is not confirmed"})
		return
	}
	writeJSON(w, http.StatusUnprocessableEntity, configStudioMutationResponse{Success: false, Mutation: "ROLLED_BACK", XrayValid: false, Applied: false, Rollback: "SUCCESS", Snapshot: backup.Snapshot, CoreRestart: false, Error: "post-apply validation failed; previous config restored"})
}
