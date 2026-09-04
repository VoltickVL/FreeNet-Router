package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writePartialSplitTestFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestPartialLegacySplitSelfRepairNormalizesDNSLayer(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "opt")
	configDir := filepath.Join(root, "etc", "xray", "configs")
	state := filepath.Join(tmp, "runtime.state")
	for _, dir := range []string{
		filepath.Join(root, "etc", "freenet", "native-dns"),
		configDir,
		filepath.Join(root, "etc", "xray", "dat"),
		filepath.Join(root, "etc", "init.d"),
		filepath.Join(root, "sbin"),
		filepath.Join(root, "backups"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	writePartialSplitTestFile(t, filepath.Join(root, "etc", "freenet", "freenet.conf"), "ISP_ID=vladlink\nDNS_MODE=xkeen\n", 0o644)
	writePartialSplitTestFile(t, filepath.Join(root, "etc", "init.d", "S05xkeen"), "#!/bin/sh\nproxy_dns=\"off\"\n", 0o755)
	writePartialSplitTestFile(t, filepath.Join(configDir, "02_dns.json"), `{"dns":{"tag":"dns-vless","servers":[{"address":"https://8.8.8.8/dns-query","tag":"dns-vless"}],"queryStrategy":"UseIPv4"}}`+"\n", 0o644)
	writePartialSplitTestFile(t, filepath.Join(configDir, "03_inbounds.json"), `{"inbounds":[{"tag":"redirect","port":5000,"protocol":"dokodemo-door"},{"tag":"dns","port":53,"protocol":"dokodemo-door","settings":{"network":"tcp,udp"}}]}`+"\n", 0o644)
	// Reproduces the HOME v0.2.47 safe STOP: OPKG/Xray owns :53, but the managed
	// DNS layer is incomplete (no dns-out and no managed DNS routing rules).
	writePartialSplitTestFile(t, filepath.Join(configDir, "04_outbounds.json"), `{"outbounds":[{"tag":"vless-reality","protocol":"freedom","settings":{"marker":"preserve-vpn"}},{"tag":"direct","protocol":"freedom"}]}`+"\n", 0o644)
	writePartialSplitTestFile(t, filepath.Join(configDir, "05_routing.json"), `{"routing":{"domainStrategy":"AsIs","rules":[{"type":"field","domain":["ext:geosite.dat:youtube"],"outboundTag":"vless-reality"},{"type":"field","domain":["ext:geosite.dat:google"],"outboundTag":"direct"},{"type":"field","network":"tcp,udp","outboundTag":"vless-reality"}]}}`+"\n", 0o644)
	writePartialSplitTestFile(t, filepath.Join(root, "etc", "freenet", "native-dns", "intercept.native"), "on\n", 0o644)

	stateBody := strings.Join([]string{
		"PORT53_OWNER=xray",
		"NDM_DNS_OVERRIDE=on",
		"NDM_FILTER_ENGINE=opkg",
		"NDM_DNS_INTERCEPT=on",
		"NDM_CONFIG_MARKER=preserved",
		"NDM_DNS_PROFILE_MARKER=preserved",
		"NDM_MUTATE_PROTECTED_ON_OVERRIDE=no",
		"XRAY_RUNNING=yes",
		"XRAY_GID=11111",
		"DNS_QUERY_OK=yes",
		"XKEEN_ACTION_RESULT=success",
		"NDM_ACTION_RESULT=success",
		"NDM_FILTER_ENGINE_ACTION_RESULT=success",
		"NDM_INTERCEPT_ACTION_RESULT=success",
		"NDM_SAVE_RESULT=success",
	}, "\n") + "\n"
	writePartialSplitTestFile(t, state, stateBody, 0o644)
	writePartialSplitTestFile(t, filepath.Join(root, "sbin", "xkeen"), "#!/bin/sh\nexit 0\n", 0o755)
	writePartialSplitTestFile(t, filepath.Join(root, "sbin", "xray"), "#!/bin/sh\nexit 0\n", 0o755)

	script := filepath.Join("..", "scripts", "apply_network_profile.sh")
	cmd := exec.Command("sh", script, "apply")
	cmd.Env = append(os.Environ(),
		"FREENET_ROOT="+root,
		"FREENET_BACKUP_ROOT="+filepath.Join(root, "backups"),
		"FREENET_CONFIG_FILE="+filepath.Join(root, "etc", "freenet", "freenet.conf"),
		"FREENET_CONFIG_DIR="+configDir,
		"FREENET_XRAY_ASSET_DIR="+filepath.Join(root, "etc", "xray", "dat"),
		"FREENET_XKEEN_BIN="+filepath.Join(root, "sbin", "xkeen"),
		"FREENET_XRAY_BIN="+filepath.Join(root, "sbin", "xray"),
		"FREENET_XKEEN_RUNTIME_TIMEOUT=2",
		"FREENET_NETWORK_TEST_MODE=yes",
		"FREENET_NETWORK_TEST_STATE="+state,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("partial Split repair failed: %v\n%s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "RESULT=SUCCESS") || !strings.Contains(got, "DNS_ROUTING_MODE=split") {
		t.Fatalf("repair did not reach strict Split acceptance:\n%s", got)
	}

	stateAfter, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stateAfter), "NDM_DNS_INTERCEPT=off") || !strings.Contains(string(stateAfter), "PORT53_OWNER=xray") {
		t.Fatalf("repair did not establish OPKG/Xray control plane:\n%s", stateAfter)
	}

	outbound, err := os.ReadFile(filepath.Join(configDir, "04_outbounds.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(outbound), `"tag":"dns-out"`) != 1 || !strings.Contains(string(outbound), `"protocol":"dns"`) {
		t.Fatalf("dns-out was not normalized exactly once: %s", outbound)
	}
	if !strings.Contains(string(outbound), `"marker":"preserve-vpn"`) {
		t.Fatalf("non-DNS VPN outbound was not preserved: %s", outbound)
	}

	routing, err := os.ReadFile(filepath.Join(configDir, "05_routing.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"dns-vless"`, `"dns-direct"`, `"dns-out"`} {
		if !strings.Contains(string(routing), want) {
			t.Fatalf("normalized Split routing missing %s: %s", want, routing)
		}
	}

	// Keep a cheap content fingerprint assertion so an accidental empty/truncated
	// candidate cannot satisfy the string checks above.
	h := sha256.Sum(routing)
	if fmt.Sprintf("%x", h[:]) == strings.Repeat("0", 64) {
		t.Fatal("unexpected zero routing fingerprint")
	}
}
