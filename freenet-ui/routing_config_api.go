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

func (a *app) handleRoutingConfigValidate(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, routingValidateResponse{Success: false, Mutation: "NONE", Error: "cross-origin request rejected"})
		return
	}
	if ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))); !strings.HasPrefix(ct, "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, routingValidateResponse{Success: false, Mutation: "NONE", Error: "application/json required"})
		return
	}
	body := http.MaxBytesReader(w, r.Body, maxRoutingConfigBodyBytes)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req routingValidateRequest
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, routingValidateResponse{Success: false, Mutation: "NONE", Error: "invalid routing candidate request"})
		return
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, routingValidateResponse{Success: false, Mutation: "NONE", Error: "invalid routing candidate request"})
		return
	}

	routing, err := normalizeRoutingSectionJSON(req.Routing, "routing", true)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, routingValidateResponse{Success: false, Mutation: "NONE", Error: "05_routing.json candidate is not a valid routing object"})
		return
	}
	policy, err := normalizeRoutingSectionJSON(req.Policy, "policy", true)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, routingValidateResponse{Success: false, Mutation: "NONE", Error: "06_policy.json candidate is not a valid policy object"})
		return
	}

	if err := a.validateRoutingCandidate(r.Context(), routing, policy); err != nil {
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
		writeJSON(w, status, routingValidateResponse{Success: false, Mutation: "NONE", XrayValid: false, Error: message})
		return
	}
	writeJSON(w, http.StatusOK, routingValidateResponse{Success: true, Mutation: "NONE", XrayValid: true, Routing: routing, Policy: policy})
}
