package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeMemInfoFixture(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "meminfo")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHardwareCapabilityBlocks512MiBClass(t *testing.T) {
	path := writeMemInfoFixture(t, "MemTotal:         500000 kB\nMemFree:          100000 kB\n")
	t.Setenv("FREENET_MEMINFO_PATH", path)
	capability := currentHardwareCapabilities()
	if !capability.Success {
		t.Fatalf("capability read failed: %+v", capability)
	}
	if capability.SplitDNSSupported {
		t.Fatalf("512 MiB class must not support Split DNS: %+v", capability)
	}
	if capability.MemoryTotalMiB >= splitDNSMinMemoryMiB {
		t.Fatalf("fixture unexpectedly above threshold: %+v", capability)
	}
	if capability.Reason == "" {
		t.Fatal("blocked capability must explain the reason")
	}
}

func TestHardwareCapabilityAllows1024MiBClass(t *testing.T) {
	path := writeMemInfoFixture(t, "MemTotal:        1000000 kB\n")
	t.Setenv("FREENET_MEMINFO_PATH", path)
	capability := currentHardwareCapabilities()
	if !capability.Success || !capability.SplitDNSSupported {
		t.Fatalf("1024 MiB class must support Split DNS: %+v", capability)
	}
}

func TestHardwareCapabilityThresholdIs768MiB(t *testing.T) {
	path := writeMemInfoFixture(t, "MemTotal:         786432 kB\n")
	t.Setenv("FREENET_MEMINFO_PATH", path)
	capability := currentHardwareCapabilities()
	if !capability.SplitDNSSupported || capability.MemoryTotalMiB != 768 {
		t.Fatalf("threshold must accept exact 768 MiB: %+v", capability)
	}
}

func TestHardwareCapabilityUnknownMemoryFailsClosed(t *testing.T) {
	path := writeMemInfoFixture(t, "MemFree: 12345 kB\n")
	t.Setenv("FREENET_MEMINFO_PATH", path)
	capability := currentHardwareCapabilities()
	if capability.Success || capability.SplitDNSSupported {
		t.Fatalf("unknown memory must fail closed for Split DNS: %+v", capability)
	}
}

func TestApplySplitDNSMemoryGateRemovesUnsafeChoiceAndRecommendation(t *testing.T) {
	originalDNS, hadDNS := dnsModes["xkeen"]
	originalVladlink := ispProfiles["vladlink"]
	originalAlliance := ispProfiles["alliancetelecom"]
	defer func() {
		if hadDNS {
			dnsModes["xkeen"] = originalDNS
		} else {
			delete(dnsModes, "xkeen")
		}
		ispProfiles["vladlink"] = originalVladlink
		ispProfiles["alliancetelecom"] = originalAlliance
	}()

	dnsModes["xkeen"] = "XKeen/Xray DNS"
	v := ispProfiles["vladlink"]
	v.RecommendedDNSMode = "xkeen"
	ispProfiles["vladlink"] = v
	a := ispProfiles["alliancetelecom"]
	a.RecommendedDNSMode = "xkeen"
	ispProfiles["alliancetelecom"] = a

	applySplitDNSMemoryGate(hardwareCapabilitiesResponse{SplitDNSSupported: false, Reason: "low memory"})
	if _, ok := dnsModes["xkeen"]; ok {
		t.Fatal("xkeen DNS must be removed from backend-supported modes")
	}
	if ispProfiles["vladlink"].RecommendedDNSMode != "firmware" || ispProfiles["alliancetelecom"].RecommendedDNSMode != "firmware" {
		t.Fatal("low-memory ISP recommendations must fall back to direct/native DNS")
	}
}

func TestHardwareCapabilitiesAPIReportsMemoryGate(t *testing.T) {
	path := writeMemInfoFixture(t, "MemTotal:         500000 kB\n")
	t.Setenv("FREENET_MEMINFO_PATH", path)
	a := &app{}
	r := httptest.NewRequest(http.MethodGet, "/api/capabilities", nil)
	w := httptest.NewRecorder()
	a.handleHardwareCapabilities(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var response hardwareCapabilitiesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.SplitDNSSupported || response.MemoryTotalMiB == 0 || response.SplitDNSMinMiB != splitDNSMinMemoryMiB {
		t.Fatalf("unexpected capability response: %+v", response)
	}
}
