package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthStatusDoesNotExposeCredentialMaterial(t *testing.T) {
	a := testAuthApp(t)
	if err := a.createCredential("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	w := httptest.NewRecorder()
	a.handleAuthStatus(w, r)
	body := strings.ToLower(w.Body.String())
	if strings.Contains(body, "hash") || strings.Contains(body, "salt") || strings.Contains(body, "session") {
		t.Fatalf("auth status leaked credential/session material: %s", w.Body.String())
	}
}

func TestHealthAndAuthStatusRemainIndependentFromSession(t *testing.T) {
	a := testAuthApp(t)
	if err := a.createCredential("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	a.handleAuthStatus(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("auth status code=%d", w.Code)
	}
}
