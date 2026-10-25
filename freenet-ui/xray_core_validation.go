package main

import (
	"context"
	"net/http"
	"time"
)

// The browser can inspect a core-only upgrade's installed-config prerequisite
// without starting a binary update, copying or exporting live configuration,
// or returning raw stderr, paths, credentials, or Xray config fragments.
type xrayCoreValidationResponse struct {
	Success  bool   `json:"success"`
	Valid    bool   `json:"valid"`
	Mutation string `json:"mutation"`
	Reason   string `json:"reason,omitempty"`
}

func (a *app) handleXrayCoreValidationGet(w http.ResponseWriter, r *http.Request) {
	result := xrayCoreValidationResponse{Success: true, Mutation: "NONE"}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	binary, _, err := a.resolvedXrayCoreBinary()
	if err != nil {
		result.Reason = "reason=xray-test-executable-unavailable"
		writeJSON(w, http.StatusOK, result)
		return
	}
	if err := a.validateXrayCoreBaseline(ctx, binary); err != nil {
		result.Reason = err.Error()
		writeJSON(w, http.StatusOK, result)
		return
	}
	result.Valid = true
	writeJSON(w, http.StatusOK, result)
}
