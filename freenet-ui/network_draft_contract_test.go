package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSelfUpdateScriptIsEmbeddedAndServed(t *testing.T) {
	if _, err := webFS.ReadFile("web/self-update.js"); err != nil {
		t.Fatalf("self-update.js is not embedded: %v", err)
	}
	a := &app{}
	r := httptest.NewRequest(http.MethodGet, "http://192.168.50.1:1001/self-update.js", nil)
	w := httptest.NewRecorder()
	a.handleIndex(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Content-Type"), "application/javascript") {
		t.Fatalf("unexpected content type: %s", w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Body.String(), "Проверить обновление") {
		t.Fatal("served script does not contain Web Update UI")
	}
}

func TestLegacyNetworkSaveEndpointCannotPersistDraft(t *testing.T) {
	a := &app{cfg: config{UpdateLock: t.TempDir() + "/no-lock"}}
	r := httptest.NewRequest(http.MethodPost, "http://192.168.50.1:1001/api/network-profile", strings.NewReader(`{"isp":"vladlink","dns_mode":"xkeen"}`))
	r.Host = "192.168.50.1:1001"
	r.Header.Set("Origin", "http://192.168.50.1:1001")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.handleNetworkProfilePost(w, r)
	if w.Code != http.StatusGone {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Проверить изменения") {
		t.Fatalf("legacy endpoint must direct clients to transactional flow: %s", w.Body.String())
	}
}

func TestNetworkDraftEnhancementRemovesSaveFirstFlow(t *testing.T) {
	js, err := webFS.ReadFile("web/self-update.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(js)
	for _, required := range []string{
		"Проверить изменения",
		"Активный ISP/DNS будет сохранён только после успешной проверки результата",
		"/api/network-profile/plan?",
		"q.set('isp', isp.value)",
		"q.set('dns_mode', dns.value)",
		"if (oldSave) oldSave.hidden = true",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("network draft UI contract missing %q", required)
		}
	}
}
