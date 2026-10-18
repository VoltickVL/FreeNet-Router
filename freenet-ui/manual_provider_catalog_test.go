package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestManualCatalogReadRepairsDisplayDriftWithoutNetworkOrWrites(t *testing.T) {
	for _, scenario := range []string{"legacy-id", "endpoint", "missing-display", "matching-display"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			cachePath := filepath.Join(dir, "provider.lkg")
			t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_CACHE", cachePath)
			t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_SOURCE", cachePath+".source")
			t.Setenv("FREENET_SUBSCRIPTION_PROFILES_CACHE", filepath.Join(dir, "display.json"))
			const source = "https://provider.example.invalid/catalog-fixture-token"
			subPath := filepath.Join(dir, "subscription.url")
			if err := os.WriteFile(subPath, []byte(source+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			old := time.Unix(1700000000, 0)
			if err := os.Chtimes(subPath, old, old); err != nil {
				t.Fatal(err)
			}
			if err := saveProviderSubscriptionCache(source, []byte(testSubscriptionPlain)); err != nil {
				t.Fatal(err)
			}
			parsed, err := parseSubscriptionBody([]byte(testSubscriptionPlain))
			if err != nil {
				t.Fatal(err)
			}
			want := selectableSubscriptionProfiles(parsed)
			if len(want) == 0 {
				t.Fatal("fixture must contain selectable profiles")
			}
			display := cloneSubscriptionProfiles(want)
			switch scenario {
			case "legacy-id":
				display[0].ID = "0123456789abcdef"
				if display[0].ID == want[0].ID {
					t.Fatal("fixture must reproduce different stored and computed IDs")
				}
			case "endpoint":
				display[0].Address = "192.0.2.222"
				display[0].Port = 9443
			}
			if scenario != "missing-display" {
				if err := saveSubscriptionProfilesCache(display, time.Now().UTC().Format(time.RFC3339)); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"vpn-transaction.pending", "04_outbounds.json", "filter", "automation.state"} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("protected-before\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			a := &app{cfg: config{SubPath: subPath, OutPath: filepath.Join(dir, "04_outbounds.json")}}
			oldDiscovery, oldDirect, oldVPN := subscriptionProfileDiscovery, directSubscriptionBodyFetch, activeVPNSubscriptionBodyFetch
			t.Cleanup(func() {
				subscriptionProfileDiscovery, directSubscriptionBodyFetch, activeVPNSubscriptionBodyFetch = oldDiscovery, oldDirect, oldVPN
			})
			var calls atomic.Int32
			subscriptionProfileDiscovery = func(*app, context.Context) ([]subscriptionProfile, error) {
				calls.Add(1)
				return nil, errors.New("network must not be needed for manual catalog")
			}
			directSubscriptionBodyFetch = func(context.Context, *url.URL) ([]byte, error) {
				calls.Add(1)
				return nil, errors.New("unexpected direct fetch")
			}
			activeVPNSubscriptionBodyFetch = func(*app, context.Context, *url.URL) ([]byte, error) {
				calls.Add(1)
				return nil, errors.New("unexpected VPN fetch")
			}
			before := manualCatalogFileSnapshot(t, dir)
			got, err := a.subscriptionProfilesForRead(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(selectableSubscriptionProfiles(got.Profiles), want) {
				t.Fatal("display-only cache overrode the protected selectable catalog")
			}
			if got.Stale != (scenario != "matching-display") || got.UpdatedAt == "" {
				t.Fatalf("wrong cache provenance: stale=%v updated_at=%q", got.Stale, got.UpdatedAt)
			}
			for _, profile := range got.Profiles {
				candidate, err := a.localManualProviderCandidate(profile.ID, profileEndpoint(profile))
				if err != nil || candidate.Profile != profile || candidate.Raw == "" {
					t.Fatalf("displayed profile is not admissible from the same local cache: %v", err)
				}
			}
			if calls.Load() != 0 {
				t.Fatalf("manual catalog read made %d network calls", calls.Load())
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{source, "vless://", "TEST-ID-", "pbk=", "sid="} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatal("safe catalog response contains credential-bearing source data")
				}
			if !reflect.DeepEqual(before, manualCatalogFileSnapshot(t, dir)) {
				t.Fatal("catalog read changed persistent files or checkpoint")
			}
		})
	}
}

type manualCatalogFileState struct {
	Data     string
	Mode     os.FileMode
	Modified int64
}

func manualCatalogFileSnapshot(t *testing.T, dir string) map[string]manualCatalogFileState {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]manualCatalogFileState, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[entry.Name()] = manualCatalogFileState{Data: string(data), Mode: info.Mode(), Modified: info.ModTime().UnixNano()}
	}
	return out
}

func TestManualCatalogReaderKeepsSourceAndPermissionGuards(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "provider.lkg")
	t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_CACHE", cachePath)
	t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_SOURCE", cachePath+".source")
	const source = "https://provider.example.invalid/catalog-fixture-token"
	subPath := filepath.Join(dir, "subscription.url")
	if err := os.WriteFile(subPath, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{SubPath: subPath}}
	if _, _, err := a.localManualProviderCatalog(); err == nil {
		t.Fatal("missing private cache must not become an admissible catalog")
	}
	if err := saveProviderSubscriptionCache(source, []byte(testSubscriptionPlain)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cachePath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.localManualProviderCatalog(); err == nil {
		t.Fatal("unsafe permissions must be rejected")
	}
	if err := os.Chmod(cachePath, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subPath, []byte("https://other.example.invalid/different-source"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.localManualProviderCatalog(); err == nil {
		t.Fatal("a different subscription must not receive private candidates")
	}
}
