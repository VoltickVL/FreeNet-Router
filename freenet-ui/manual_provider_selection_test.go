package main

import (
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestLocalManualProviderCandidateIsSourceAndEndpointBound(t *testing.T) {
    dir := t.TempDir()
    cachePath := filepath.Join(dir, "provider.lkg")
    t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_CACHE", cachePath)
    t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_SOURCE", cachePath+".source")
    sourcePath := filepath.Join(dir, "subscription.url")
    const source = "https://provider.example.invalid/subscription-token"
    if err := os.WriteFile(sourcePath, []byte(source+"\n"), 0600); err != nil { t.Fatal(err) }
    raw := strings.Split(strings.TrimSpace(testSubscriptionPlain), "\n")[0]
    profile, ok := parseSafeVLESSProfile(raw)
    if !ok { t.Fatal("fixture profile is not selectable") }
    a := testNetworkApp(t, "DNS_MODE=firmware\n")
    a.cfg.SubPath = sourcePath

    if _, err := a.localManualProviderCandidate(profile.ID, profileEndpoint(profile)); err == nil {
        t.Fatal("manual apply must reject absent protected subscription cache")
    }
    if err := saveProviderSubscriptionCache(source, []byte(testSubscriptionPlain)); err != nil { t.Fatal(err) }
    candidate, err := a.localManualProviderCandidate(profile.ID, profileEndpoint(profile))
    if err != nil || candidate.Raw != raw || candidate.Profile.ID != profile.ID {
        t.Fatalf("valid local exact profile unavailable: profile=%+v err=%v", candidate.Profile, err)
    }
    if _, err := a.localManualProviderCandidate(profile.ID, "192.0.2.88:443"); err == nil {
        t.Fatal("UI endpoint drift must not silently select a different candidate")
    }
    if _, err := a.localManualProviderCandidate("0123456789abcdef", profileEndpoint(profile)); err == nil {
        t.Fatal("stale/unknown profile id must be rejected before live mutation")
    }
    if err := os.Chmod(cachePath, 0644); err != nil { t.Fatal(err) }
    if _, err := a.localManualProviderCandidate(profile.ID, profileEndpoint(profile)); err == nil {
        t.Fatal("world-readable credential-bearing cache must be rejected")
    }
    if err := os.Chmod(cachePath, 0600); err != nil { t.Fatal(err) }
    if err := os.WriteFile(sourcePath, []byte("https://other.example.invalid/key\n"), 0600); err != nil { t.Fatal(err) }
    if _, err := a.localManualProviderCandidate(profile.ID, profileEndpoint(profile)); err == nil {
        t.Fatal("cache must be rejected when subscription source changes")
    }
}

func TestManualOverrideAppliesWithoutPlanAndDoesNotClearSafetyLatch(t *testing.T) {
    dir := t.TempDir()
    cachePath := filepath.Join(dir, "provider.lkg")
    t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_CACHE", cachePath)
    t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_SOURCE", cachePath+".source")
    statePath := filepath.Join(dir, "automation.state")
    t.Setenv("FREENET_AUTOMATION_STATE", statePath)
    if err := os.WriteFile(statePath, []byte("MUTATION_BLOCKED=yes\n"), 0600); err != nil { t.Fatal(err) }
    const source = "https://provider.example.invalid/subscription-token"
    sourcePath := filepath.Join(dir, "subscription.url")
    if err := os.WriteFile(sourcePath, []byte(source+"\n"), 0600); err != nil { t.Fatal(err) }
    raw := strings.Split(strings.TrimSpace(testSubscriptionPlain), "\n")[0]
    profile, ok := parseSafeVLESSProfile(raw)
    if !ok { t.Fatal("fixture profile is not selectable") }
    if err := saveProviderSubscriptionCache(source, []byte(testSubscriptionPlain)); err != nil { t.Fatal(err) }
    a := testNetworkApp(t, "DNS_MODE=firmware\n")
    a.cfg.SubPath = sourcePath
    a.cfg.OutPath = filepath.Join(dir, "04_outbounds.json")
    a.cfg.FilterPath = filepath.Join(dir, "filter")
    if err := os.WriteFile(a.cfg.OutPath, []byte(`{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"192.0.2.99","port":443}]}}]}`), 0600); err != nil { t.Fatal(err) }
    if err := os.WriteFile(a.cfg.FilterPath, []byte("^old$\n"), 0600); err != nil { t.Fatal(err) }
    marker := filepath.Join(dir, "manual-applied")
    script := writeFakeNetworkHelper(t, `[ "$1" = "apply-core" ] || exit 21
[ "$FREENET_PROVIDER_RTT_MANUAL" = "1" ] || exit 22
grep -Fq 'TEST-ID-A@203.0.113.10:443' "$FREENET_PROVIDER_SUBSCRIPTION_CACHE" || exit 23
echo applied > "`+marker+`"
cat > "$FREENET_TEST_OUT_PATH" <<'EOF'
{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"203.0.113.10","port":443}]}}]}
EOF
echo '[FreeNet Provider] RESULT=SUCCESS'
`)
    network := writeFakeNetworkHelper(t, "[ \"$1\" = plan ] || exit 9\ncat <<'EOF'\n"+supportedPlanOutput()+"\nEOF")
    t.Setenv("FREENET_PROVIDER_HELPER", script)
    t.Setenv("FREENET_NETWORK_HELPER", network)
    t.Setenv("FREENET_TEST_OUT_PATH", a.cfg.OutPath)

    wrong := networkApplyRequest{Operation:"provider", ProfileID:profile.ID, ExpectedEndpoint:"192.0.2.88:443", ManualOverride:true, Confirm:true}
    code, result := a.executeProviderProfileApply(wrong)
    if code != http.StatusConflict || result.Success || result.RollbackState != "NOT_APPLIED" { t.Fatalf("changed endpoint must STOP: %d %+v",code,result) }
    if _, err := os.Stat(marker); !os.IsNotExist(err) { t.Fatal("rejected candidate entered helper") }

    code, result = a.executeProviderProfileApply(networkApplyRequest{Operation:"provider", ProfileID:profile.ID, ExpectedEndpoint:profileEndpoint(profile), ManualOverride:true, Confirm:true})
    if code != http.StatusOK || !result.Success || !result.Applied { t.Fatalf("explicit manual apply unavailable: %d %+v",code,result) }
    if !strings.Contains(result.Message, "не проверялась") { t.Fatalf("unprobed apply claimed Internet access: %q",result.Message) }
    if _, err := os.Stat(marker); err != nil { t.Fatalf("manual apply helper not called: %v",err) }
    if !automationMutationBlockedState() { t.Fatal("unprobed manual apply must not clear UNKNOWN rollback safety latch") }
}
