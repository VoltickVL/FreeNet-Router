package main

import (
	"os"
	"path/filepath"
	"testing"
)

func momNativeResiduePlan() map[string]string {
	return map[string]string{
		"PROXY_DNS":              "off",
		"NDM_DNS_OVERRIDE":        "off",
		"NDM_FILTER_ENGINE":       "public",
		"NDM_DNS_INTERCEPT":       "on",
		"NDM_DNS_ASSIGNMENTS":     "present",
		"PORT53_OWNER":            "ndnproxy",
		"XRAY_DNS_INBOUND_COUNT":  "1",
		"XRAY_RUNNING":            "yes",
		"XRAY_GID":                "11111",
		"DNS_ROUTING_MODE":        "split-intercept",
		"DNS_OUT":                 "yes",
		"VLESS_PROFILE":           "yes",
	}
}

func TestNetworkBridgeClassifiesMomNativeXrayResidue(t *testing.T) {
	plan := momNativeResiduePlan()
	if !networkBridgeNativeXrayResidue(plan) {
		t.Fatalf("MOM interrupted Native state must be repairable: %+v", plan)
	}

	for key, value := range map[string]string{
		"NDM_DNS_OVERRIDE":       "on",
		"PORT53_OWNER":           "xray",
		"NDM_FILTER_ENGINE":      "opkg",
		"XRAY_DNS_INBOUND_COUNT": "2",
		"DNS_OUT":                "no",
		"DNS_ROUTING_MODE":       "unknown",
	} {
		copy := momNativeResiduePlan()
		copy[key] = value
		if networkBridgeNativeXrayResidue(copy) {
			t.Fatalf("unsafe variant %s=%s was classified as repairable", key, value)
		}
	}
}

func setupNativeResidueCandidateFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	configDir := filepath.Join(root, "configs")
	stateDir := filepath.Join(root, "native-dns")
	assetDir := filepath.Join(root, "assets")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_CONFIG_DIR", configDir)
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", stateDir)
	t.Setenv("FREENET_XRAY_ASSET_DIR", assetDir)
	t.Setenv("FREENET_XRAY_BIN", writeLegacyMigrationXray(t, "exit 0"))
	writeNativeDNSSnapshotForTest(t, stateDir, []byte("{}\n"))

	writeLegacyMigrationConfig(t, configDir, "02_dns.json", `{"dns":{"servers":["https://8.8.8.8/dns-query"]}}`)
	writeLegacyMigrationConfig(t, configDir, "03_inbounds.json", `{
  "inbounds": [
    {"tag":"redirect","port":5000,"protocol":"dokodemo-door"},
    {"tag":"dns","port":53,"protocol":"dokodemo-door","settings":{"network":"tcp,udp"}}
  ]
}`)
	writeLegacyMigrationConfig(t, configDir, "04_outbounds.json", `{
  "outbounds": [
    {"tag":"vless-reality","protocol":"vless"},
    {"tag":"direct","protocol":"freedom"},
    {"tag":"dns-out","protocol":"dns"}
  ]
}`)
	writeLegacyMigrationConfig(t, configDir, "05_routing.json", `{
  "routing":{"rules":[
    {"type":"field","inboundTag":["dns-vless"],"outboundTag":"vless-reality"},
    {"type":"field","inboundTag":["dns-direct"],"outboundTag":"direct"},
    {"type":"field","port":53,"outboundTag":"dns-out"},
    {"type":"field","domain":["ext:geosite.dat:youtube"],"outboundTag":"direct"},
    {"type":"field","network":"tcp,udp","outboundTag":"vless-reality"}
  ]}
}`)
	return configDir
}

func TestNetworkBridgeNativeResidueCandidateStripsOnlyManagedDNS(t *testing.T) {
	configDir := setupNativeResidueCandidateFixture(t)
	candidate, err := networkBridgeBuildNativeResidueCandidate()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(candidate)

	inbounds, err := legacyNativeReadJSONObject(filepath.Join(candidate, "03_inbounds.json"))
	if err != nil {
		t.Fatal(err)
	}
	items, err := legacyNativeObjectSlice(inbounds, "inbounds")
	if err != nil || len(items) != 1 || legacyNativePortIs53(items[0]["port"]) {
		t.Fatalf("candidate inbounds=%v err=%v", items, err)
	}

	outbounds, err := legacyNativeReadJSONObject(filepath.Join(candidate, "04_outbounds.json"))
	if err != nil {
		t.Fatal(err)
	}
	items, err = legacyNativeObjectSlice(outbounds, "outbounds")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if legacyNativeString(item["tag"]) == "dns-out" {
			t.Fatal("dns-out remained in Native candidate")
		}
	}

	routing, err := legacyNativeReadJSONObject(filepath.Join(candidate, "05_routing.json"))
	if err != nil {
		t.Fatal(err)
	}
	routingObj := routing["routing"].(map[string]any)
	rules, err := legacyNativeObjectSlice(routingObj, "rules")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("non-DNS routing was not preserved exactly enough: %+v", rules)
	}
	if legacyNativeString(rules[0]["outboundTag"]) != "direct" || legacyNativeString(rules[1]["outboundTag"]) != "vless-reality" {
		t.Fatalf("unexpected preserved rules: %+v", rules)
	}

	dns, err := os.ReadFile(filepath.Join(candidate, "02_dns.json"))
	if err != nil || string(dns) != "{}\n" {
		t.Fatalf("candidate did not restore canonical Native 02_dns: %q err=%v", dns, err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "03_inbounds.json")); err != nil {
		t.Fatal("read-only candidate build changed live config")
	}
}

func TestNetworkBridgeNativeResidueRejectsUnknownDNSRule(t *testing.T) {
	configDir := setupNativeResidueCandidateFixture(t)
	writeLegacyMigrationConfig(t, configDir, "05_routing.json", `{
  "routing":{"rules":[
    {"type":"field","inboundTag":["dns-vless"],"outboundTag":"vless-reality"},
    {"type":"field","inboundTag":["dns-direct"],"outboundTag":"direct"},
    {"type":"field","port":53,"outboundTag":"dns-out"},
    {"type":"field","inboundTag":["dns-in"],"outboundTag":"block"}
  ]}
}`)
	if _, err := networkBridgeBuildNativeResidueCandidate(); err == nil {
		t.Fatal("unknown DNS-only rule was silently deleted")
	}
}

func TestNetworkBridgeNativeResiduePostAcceptancePreservesNativeControlPlane(t *testing.T) {
	before := momNativeResiduePlan()
	after := momNativeResiduePlan()
	after["XRAY_DNS_INBOUND_COUNT"] = "0"
	after["DNS_ROUTING_MODE"] = "native"
	after["DNS_OUT"] = "no"
	if !networkBridgeNativeResiduePostAccepted(before, after) {
		t.Fatalf("canonical Native post-state was rejected: %+v", after)
	}
	after["NDM_DNS_INTERCEPT"] = "off"
	if networkBridgeNativeResiduePostAccepted(before, after) {
		t.Fatal("Native reconcile accepted an unexpected NDM intercept change")
	}
}
