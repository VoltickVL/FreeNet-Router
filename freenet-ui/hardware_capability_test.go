package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	if splitDNSSelectionError("xkeen") == nil {
		t.Fatal("unknown memory must reject a new Split DNS selection")
	}
	if splitDNSSelectionError("firmware") != nil {
		t.Fatal("unknown memory must not block direct/native DNS")
	}
}

func TestApplySplitDNSMemoryGateKeepsActiveModeVisibleAndChangesRecommendation(t *testing.T) {
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
	if _, ok := dnsModes["xkeen"]; !ok {
		t.Fatal("existing active xkeen state must remain representable for controlled return to native")
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

func TestLowMemoryPlanRejectsSplitBeforeHelper(t *testing.T) {
	memInfo := writeMemInfoFixture(t, "MemTotal:         500000 kB\n")
	t.Setenv("FREENET_MEMINFO_PATH", memInfo)
	marker := filepath.Join(t.TempDir(), "helper-ran")
	helper := writeFakeNetworkHelper(t, "echo ran > \""+marker+"\"\nexit 9")
	t.Setenv("FREENET_NETWORK_HELPER", helper)
	a := testNetworkApp(t, "ISP_ID=vladlink\nDNS_MODE=firmware\nSETUP_COMPLETE=yes\n")

	r := httptest.NewRequest(http.MethodGet, "http://192.168.50.1:1001/api/network-profile/plan?isp=vladlink&dns_mode=xkeen", nil)
	w := httptest.NewRecorder()
	a.handleNetworkProfilePlan(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("network helper must not run for blocked Split plan")
	}
	var plan networkPlanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Supported || plan.Mutation != "NONE" || !strings.Contains(plan.Reason, "768") {
		t.Fatalf("unexpected blocked plan: %+v", plan)
	}
}

func TestLowMemoryApplyRejectsSplitBeforeMutation(t *testing.T) {
	memInfo := writeMemInfoFixture(t, "MemTotal:         500000 kB\n")
	t.Setenv("FREENET_MEMINFO_PATH", memInfo)
	marker := filepath.Join(t.TempDir(), "helper-ran")
	helper := writeFakeNetworkHelper(t, "echo ran > \""+marker+"\"\nexit 9")
	t.Setenv("FREENET_NETWORK_HELPER", helper)
	a := testNetworkApp(t, "ISP_ID=vladlink\nDNS_MODE=firmware\nSETUP_COMPLETE=yes\n")

	payload := `{"operation":"network","isp":"vladlink","dns_mode":"xkeen","confirm":true}`
	r := httptest.NewRequest(http.MethodPost, "http://192.168.50.1:1001/api/network-profile/apply", strings.NewReader(payload))
	r.Host = "192.168.50.1:1001"
	r.Header.Set("Origin", "http://192.168.50.1:1001")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.handleNetworkProfileApply(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("network helper must not run for blocked Split apply")
	}
	var response networkApplyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Applied || response.RollbackState != "NOT_APPLIED" || !strings.Contains(response.PrimaryError, "768") {
		t.Fatalf("unexpected blocked apply: %+v", response)
	}
}

func TestLowMemoryDirectPlanStillRunsNormally(t *testing.T) {
	memInfo := writeMemInfoFixture(t, "MemTotal:         500000 kB\n")
	t.Setenv("FREENET_MEMINFO_PATH", memInfo)
	helper := writeFakeNetworkHelper(t, dynamicPlanHelper("exit 0"))
	t.Setenv("FREENET_NETWORK_HELPER", helper)
	a := testNetworkApp(t, "ISP_ID=vladlink\nDNS_MODE=firmware\nSETUP_COMPLETE=yes\n")

	r := httptest.NewRequest(http.MethodGet, "http://192.168.50.1:1001/api/network-profile/plan?isp=vladlink&dns_mode=firmware", nil)
	w := httptest.NewRecorder()
	a.handleNetworkProfilePlan(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("direct/native plan must remain available: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestLowMemoryInternalDraftCanRepresentExistingSplitForRollback(t *testing.T) {
	memInfo := writeMemInfoFixture(t, "MemTotal:         500000 kB\n")
	t.Setenv("FREENET_MEMINFO_PATH", memInfo)
	a := testNetworkApp(t, "ISP_ID=vladlink\nDNS_MODE=firmware\n")
	draft, err := a.createNetworkDraftConfig("vladlink", "xkeen")
	if err != nil {
		t.Fatalf("internal rollback draft must remain representable: %v", err)
	}
	defer os.Remove(draft)
	_, dnsMode := readNetworkProfileConfig(draft)
	if dnsMode != "xkeen" {
		t.Fatalf("draft lost existing Split mode: %s", dnsMode)
	}
}

func TestSplitDNSMemoryGateUIContract(t *testing.T) {
	data, err := os.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, required := range []string{
		"/api/capabilities",
		"option[value=\"xkeen\"]",
		"split_dns_supported",
		"splitDNSMemoryNotice",
		"mountSplitDNSMemoryGate();",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Split DNS memory gate UI contract missing %q", required)
		}
	}
}
