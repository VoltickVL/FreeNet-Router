package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const (
	maxPolicyPreviewBodyBytes = 64 << 10
	maxPolicyPreviewRules     = 128
)

type policyPreviewRequest struct {
	Rules []PolicyRule `json:"rules"`
}

type policyPreviewResponse struct {
	Success  bool           `json:"success"`
	Mutation string         `json:"mutation"`
	Compiled CompiledPolicy `json:"compiled"`
	Error    string         `json:"error,omitempty"`
}

func registerPolicyPreviewAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("POST /api/policy/compile", a.requireAuth(a.handlePolicyCompilePreview))
}

func (a *app) handlePolicyCompilePreview(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, policyPreviewResponse{Success: false, Mutation: "NONE", Error: "cross-origin request rejected"})
		return
	}
	if ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))); !strings.HasPrefix(ct, "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, policyPreviewResponse{Success: false, Mutation: "NONE", Error: "application/json required"})
		return
	}

	body := http.MaxBytesReader(w, r.Body, maxPolicyPreviewBodyBytes)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req policyPreviewRequest
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, policyPreviewResponse{Success: false, Mutation: "NONE", Error: "invalid policy preview request"})
		return
	}
	if len(req.Rules) == 0 || len(req.Rules) > maxPolicyPreviewRules {
		writeJSON(w, http.StatusBadRequest, policyPreviewResponse{Success: false, Mutation: "NONE", Error: "policy preview requires 1..128 rules"})
		return
	}
	// Reject trailing JSON values as well as unknown fields. The endpoint is a
	// deterministic preview surface, not a permissive import parser.
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, policyPreviewResponse{Success: false, Mutation: "NONE", Error: "invalid policy preview request"})
		return
	}

	compiled, err := CompilePolicy(req.Rules)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, policyPreviewResponse{Success: false, Mutation: "NONE", Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, policyPreviewResponse{Success: true, Mutation: "NONE", Compiled: compiled})
}
