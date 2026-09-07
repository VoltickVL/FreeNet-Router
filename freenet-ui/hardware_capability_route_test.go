package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHardwareCapabilitiesRouteWorksBeforeLoginFor1024MiBClass(t *testing.T) {
	path := writeMemInfoFixture(t, "MemTotal:        1000000 kB\n")
	t.Setenv("FREENET_MEMINFO_PATH", path)

	a := &app{}
	mux := http.NewServeMux()
	registerHardwareCapabilityAPI(mux, a)

	r := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("pre-auth capability status=%d body=%s", w.Code, w.Body.String())
	}
	var response hardwareCapabilitiesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || !response.SplitDNSSupported {
		t.Fatalf("1024 MiB class must be offered Split DNS before login: %+v", response)
	}
	if response.MemoryTotalMiB != 0 {
		t.Fatalf("pre-auth response must redact exact RAM amount: %+v", response)
	}
	if response.SplitDNSMinMiB != splitDNSMinMemoryMiB {
		t.Fatalf("unexpected Split DNS threshold: %+v", response)
	}
}

func TestHardwareCapabilitiesRouteFailsClosedBeforeLoginWithoutLeakingRAM(t *testing.T) {
	path := writeMemInfoFixture(t, "MemTotal:         500000 kB\n")
	t.Setenv("FREENET_MEMINFO_PATH", path)

	a := &app{}
	mux := http.NewServeMux()
	registerHardwareCapabilityAPI(mux, a)

	r := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("pre-auth capability status=%d body=%s", w.Code, w.Body.String())
	}
	var response hardwareCapabilitiesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.SplitDNSSupported {
		t.Fatalf("low-memory class must remain blocked before login: %+v", response)
	}
	if response.MemoryTotalMiB != 0 {
		t.Fatalf("pre-auth response must redact exact RAM amount: %+v", response)
	}
	if response.Reason == "" || strings.Contains(response.Reason, "MiB") {
		t.Fatalf("pre-auth reason must be useful but not disclose exact RAM: %+v", response)
	}
}
