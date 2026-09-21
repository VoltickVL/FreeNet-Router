package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func testProviderCredentialSubscription() []byte {
	return []byte(strings.Join([]string{
		"vless://TEST-UUID-A@203.0.113.10:443?flow=xtls-rprx-vision&security=reality&type=tcp&fp=firefox&sni=example.test&pbk=TEST-PBK-A&sid=TEST-SID-A&spx=%2F#DE%20Frankfurt%2C%20Germany%2C%20Extra",
		"vless://TEST-UUID-B@198.51.100.20:443?flow=xtls-rprx-vision&security=reality&type=tcp&fp=firefox&sni=example.test&pbk=TEST-PBK-B&sid=TEST-SID-B&spx=%2F#NL%20Amsterdam%2C%20Netherlands%2C%20Extra",
	}, "\n") + "\n")
}

func setProviderCachePathsForTest(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_CACHE", filepath.Join(dir, "provider.lkg"))
	t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_SOURCE", filepath.Join(dir, "provider.source"))
}

func TestDiscoverSubscriptionProfilesFallsBackToActiveVPNAndPersistsSecureCache(t *testing.T) {
	setProviderCachePathsForTest(t)
	oldDirect := directSubscriptionBodyFetch
	oldVPN := activeVPNSubscriptionBodyFetch
	t.Cleanup(func() {
		directSubscriptionBodyFetch = oldDirect
		activeVPNSubscriptionBodyFetch = oldVPN
	})

	var directCalls, vpnCalls int32
	directSubscriptionBodyFetch = func(context.Context, *url.URL) ([]byte, error) {
		atomic.AddInt32(&directCalls, 1)
		return nil, errors.New("direct path unavailable")
	}
	activeVPNSubscriptionBodyFetch = func(_ *app, _ context.Context, _ *url.URL) ([]byte, error) {
		atomic.AddInt32(&vpnCalls, 1)
		return testProviderCredentialSubscription(), nil
	}

	dir := t.TempDir()
	subPath := filepath.Join(dir, "subscription.url")
	const subURL = "https://subscription.example.invalid/private-token"
	if err := os.WriteFile(subPath, []byte(subURL+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{SubPath: subPath}}
	profiles, err := a.discoverSubscriptionProfiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 || atomic.LoadInt32(&directCalls) != 1 || atomic.LoadInt32(&vpnCalls) != 1 {
		t.Fatalf("profiles=%d direct=%d vpn=%d", len(profiles), directCalls, vpnCalls)
	}
	if !providerSubscriptionCacheMatches(subURL) {
		t.Fatal("secure provider cache does not match the exact configured source")
	}
	for _, path := range []string{providerSubscriptionCachePath(), providerSubscriptionSourcePath()} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode=%#o want 0600", path, info.Mode().Perm())
		}
	}

	safeJSON, err := json.Marshal(profiles)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(safeJSON))
	for _, forbidden := range []string{"vless://", "test-uuid", "test-pbk", "test-sid", "private-token"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("safe API profile data leaked %q", forbidden)
		}
	}
}

func TestProviderSubscriptionCacheIsBoundToExactSubscriptionSource(t *testing.T) {
	setProviderCachePathsForTest(t)
	const sourceA = "https://subscription-a.example.invalid/private-token"
	const sourceB = "https://subscription-b.example.invalid/private-token"
	if err := saveProviderSubscriptionCache(sourceA, testProviderCredentialSubscription()); err != nil {
		t.Fatal(err)
	}
	if !providerSubscriptionCacheMatches(sourceA) {
		t.Fatal("matching source rejected")
	}
	if providerSubscriptionCacheMatches(sourceB) {
		t.Fatal("credential-bearing provider cache must not survive subscription source change")
	}
}

func TestEnsureProviderSubscriptionCacheUsesMatchingLKGWithoutNetwork(t *testing.T) {
	setProviderCachePathsForTest(t)
	oldDirect := directSubscriptionBodyFetch
	oldVPN := activeVPNSubscriptionBodyFetch
	t.Cleanup(func() {
		directSubscriptionBodyFetch = oldDirect
		activeVPNSubscriptionBodyFetch = oldVPN
	})
	const subURL = "https://subscription.example.invalid/private-token"
	if err := saveProviderSubscriptionCache(subURL, testProviderCredentialSubscription()); err != nil {
		t.Fatal(err)
	}

	var calls int32
	directSubscriptionBodyFetch = func(context.Context, *url.URL) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("must not fetch")
	}
	activeVPNSubscriptionBodyFetch = func(*app, context.Context, *url.URL) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("must not fetch")
	}
	dir := t.TempDir()
	subPath := filepath.Join(dir, "subscription.url")
	if err := os.WriteFile(subPath, []byte(subURL+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{SubPath: subPath}}
	if err := a.ensureProviderSubscriptionCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("matching secure LKG unexpectedly hit network: %d", calls)
	}
}

func TestEnsureProviderSubscriptionCacheFailsClosedWhenDirectAndVPNSourcesFail(t *testing.T) {
	setProviderCachePathsForTest(t)
	oldDirect := directSubscriptionBodyFetch
	oldVPN := activeVPNSubscriptionBodyFetch
	t.Cleanup(func() {
		directSubscriptionBodyFetch = oldDirect
		activeVPNSubscriptionBodyFetch = oldVPN
	})
	directSubscriptionBodyFetch = func(context.Context, *url.URL) ([]byte, error) {
		return nil, errors.New("direct unavailable")
	}
	activeVPNSubscriptionBodyFetch = func(*app, context.Context, *url.URL) ([]byte, error) {
		return nil, errors.New("vpn unavailable")
	}
	dir := t.TempDir()
	subPath := filepath.Join(dir, "subscription.url")
	const subURL = "https://subscription.example.invalid/private-token"
	if err := os.WriteFile(subPath, []byte(subURL+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{SubPath: subPath}}
	err := a.ensureProviderSubscriptionCache(context.Background())
	if err == nil || !strings.Contains(err.Error(), "secure provider cache is missing") {
		t.Fatalf("unexpected failure: %v", err)
	}
	if _, statErr := os.Stat(providerSubscriptionCachePath()); !os.IsNotExist(statErr) {
		t.Fatalf("failed preparation must not fabricate provider cache: %v", statErr)
	}
}

func TestActiveVPNSubscriptionFetchIsReadOnlyAndSecretSafeByContract(t *testing.T) {
	data, err := os.ReadFile("subscription_active_vpn_fetch.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, required := range []string{"readBestServerActiveOutbound", "reserveBestServerPort", "--socks5-hostname", "--config", "XRAY_LOCATION_ASSET", "os.MkdirTemp"} {
		if !strings.Contains(src, required) {
			t.Fatalf("active VPN subscription fallback missing %q", required)
		}
	}
	for _, forbidden := range []string{"xkeen", "-restart", "network-profile/apply", "subscriptionURL.String(),"} {
		if strings.Contains(src, forbidden) {
			t.Fatalf("active VPN subscription fallback must stay read-only/secret-safe: found %q", forbidden)
		}
	}
}
