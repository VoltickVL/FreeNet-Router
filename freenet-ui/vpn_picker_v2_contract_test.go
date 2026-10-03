package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVPNPickerV2CanonicalContract(t *testing.T) {
	data, err := webFS.ReadFile("web/vpn-picker-v2.js")
	if err != nil { t.Fatal(err) }
	js := string(data)
	for _, required := range []string{
		"__freenetVPNPickerV2Mounted", "fnVpnPickerV2Panel", "getBoundingClientRect()",
		"window.FreeNetFlags", "api.apply(node, code)", "dataset.flagSource",
		"xray-vpn-dns-freenet",
		"selectProviderProfile(profile)", "e.button.click()", "e.button.disabled",
		"observer.disconnect()", "requestAnimationFrame", "text(connect,L.connect)",
		"0x1F1E6", "cached rows may belong to another router", "refreshStaleCatalogOnOpen", "loadNetworkPlan",
		"height:min(760px,var(--fnv2-space,760px))", "flex:1 1 auto",
		"fnVpnPickerV2Resize", "setPointerCapture", "dataset.userHeight",
		"завершён частично", "не проверен",
		"refresh.addEventListener('click',refreshRTT)",
		"expectedSourceKey", "expectedCatalog", "sourceKey!==expectedSourceKey",
		"expectedCatalog.get(id)!==measuredEndpoint", "pingCatalogChanged", "!!staleRefresh",
	} {
		if !strings.Contains(js, required) { t.Fatalf("VPN picker v2 missing %q", required) }
	}
	if !strings.Contains(js, "status==='unknown') return Number.MAX_SAFE_INTEGER-3") {
		t.Fatal("picker fallback RTT order must be reachable -> unknown -> unreachable")
	}
	if strings.Contains(js, "VPN есть · DNS?") || strings.Contains(js, "transport_only") {
		t.Fatal("quick VPN RTT UI must not expose obsolete named-DNS preflight states")
	}
	for _, forbidden := range []string{"/api/network-profile/apply", "fetch(", "document.body.innerHTML", "renderProfileOptions =", "removeLegacyPickerStyles", "rows.find(p => s?.endpoint", "#bestCurrentFlag", "#bestCurrentEndpoint", "max-height:280px", "sheet.cssRules", "maybeAutoRTT", "rttAutoAttempted", "refreshRTT(false)", "refreshRTT(true)"} {
		if strings.Contains(js, forbidden) { t.Fatalf("presentation must not contain %q", forbidden) }
	}
	mainData, err := os.ReadFile("main.go")
	if err != nil { t.Fatal(err) }
	for _, required := range []string{"web/vpn-picker-v2.js", "window.__freenetVPNPickerV2=true;", "/vpn-picker-v2.js?v=v%s"} {
		if !strings.Contains(string(mainData), required) { t.Fatalf("delivery missing %q", required) }
	}
	operation, err := webFS.ReadFile("web/operation-coordinator.js")
	if err != nil { t.Fatal(err) }
	operationSrc := string(operation)
	for _, forbidden := range []string{"fnVpnPickerPopover", "fnVpnPickerHost", "fnVpnPickerToggle", "FreeNetVPNPicker", "Issue #562", "Issue #609"} {
		if strings.Contains(operationSrc, forbidden) { t.Fatalf("legacy picker visual owner survived: %q", forbidden) }
	}
}

// Opt-in, loopback-only production delivery fixture. It uses the REAL handler
// registration, canonical HTML and asset endpoints. Playwright mocks runtime
// JSON requests; no router, subscription or real Xray process is accessed.
func TestControlCenterBrowserServer(t *testing.T) {
	addressFile := os.Getenv("FREENET_BROWSER_ADDRESS_FILE")
	if addressFile == "" { t.Skip("used by the production browser gate") }
	t.Setenv("FREENET_DISABLE_SCHEDULER_RECONCILE", "yes")
	dir := t.TempDir()
	a := &app{cfg: config{ConfigPath: filepath.Join(dir,"freenet.conf"), OutPath: filepath.Join(dir,"04_outbounds.json"), SubPath: filepath.Join(dir,"subscription.url"), LockPath: filepath.Join(dir,"lock"), UpdateLock: filepath.Join(dir,"update-lock")}, sem: make(chan struct{},1)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.handleIndex)
	registerGeoDataAPI(mux,a)
	server := httptest.NewServer(securityHeaders(mux))
	defer server.Close()
	if err := os.WriteFile(addressFile, []byte(server.URL), 0600); err != nil { t.Fatal(err) }
	stopFile := addressFile + ".stop"
	ticker := time.NewTicker(100*time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(4*time.Minute)
	defer deadline.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(stopFile); err == nil { return }
		case <-deadline.C:
			t.Fatal("browser gate did not stop its fixture server")
		}
	}
}

func TestVPNPickerRTTBridgeIsExplicitRefreshOnly(t *testing.T) {
	data, err := os.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"async function scanProviderProfileRTT()",
		"fetch('/api/provider-profiles/rtt?refresh=1'",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("RTT bridge missing explicit-only contract %q", want)
		}
	}
	for _, forbidden := range []string{
		"scanProviderProfileRTT(force",
		"force ? '?refresh=1' : ''",
		"fetch('/api/provider-profiles/rtt',",
	} {
		if strings.Contains(js, forbidden) {
			t.Fatalf("RTT bridge must not expose hidden cache/auto mode: found %q", forbidden)
		}
	}
}
