package main

import (
	"errors"
	"net/http"
	"time"
)

// The normal server WriteTimeout is 110s, but a full quality scan has a
// bounded 150s budget. Extend only this response, including a small allowance
// for cleanup and encoding; other endpoints retain the server's normal limit.
func prepareBestServerResponse(w http.ResponseWriter) bool {
	deadline := time.Now().Add(bestServerQualityScanTimeout + 10*time.Second)
	err := http.NewResponseController(w).SetWriteDeadline(deadline)
	// ResponseRecorder and other in-memory writers have no socket deadline.
	if err == nil || errors.Is(err, http.ErrNotSupported) {
		return true
	}
	writeJSON(w, http.StatusServiceUnavailable, bestServerQualityResponse{
		Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
		Error: "Unable to prepare the bounded Best Server response deadline; scan was not started",
	})
	return false
}
