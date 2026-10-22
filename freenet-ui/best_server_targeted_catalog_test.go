package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const targetedCatalogSource = "https://provider.example.invalid/targeted-catalog-fixture"

const targetedCatalogRaw = "vless://11111111-1111-4111-8111-111111111111@198.51.100.91:443?flow=xtls-rprx-vision&security=reality&type=tcp&fp=firefox&sni=old.example.invalid&pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&sid=0123456789abcdef&spx=%2F#CZ%20Prague%2C%20Extra"

func targetedCatalogFixture(t *testing.T) (*app, bestServerInternalCandidate, string) {
	t.Helper()
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "provider.lkg")
	t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_CACHE", cachePath)
	t.Setenv("FREENET_PROVIDER_SUBSCRIPTION_SOURCE", cachePath+".source")
	t.Setenv("FREENET_BEST_SELECTION_DIR", filepath.Join(dir, "selections"))
	t.Setenv("FREENET_CURRENT_QUALITY_CACHE", filepath.Join(dir, "current-quality.json"))
	a := &app{
		cfg: config{
			SubPath: filepath.Join(dir, "subscription.url"),
			OutPath: filepath.Join(dir, "04_outbounds.json"),
			FilterPath: filepath.Join(dir, "filter"),
		},
		sem: make(chan struct{}, 1),
	}
	targetedCatalogWrite(t, a.cfg.SubPath, targetedCatalogSource+"\n")
	targetedCatalogWrite(t, a.cfg.OutPath, `{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"198.51.100.245","port":18443}]}}]}`)
	targetedCatalogWrite(t, a.cfg.FilterPath, "^US Current Fixture$\n")
	if err := saveProviderSubscriptionCache(targetedCatalogSource, []byte(targetedCatalogRaw+"\n")); err != nil {
		t.Fatal(err)
	}
	measuredAt := time.Unix(1700000000, 0).UTC()
	if err := os.Chtimes(cachePath, measuredAt, measuredAt); err != nil {
		t.Fatal(err)
	}
	profile, ok := parseSafeVLESSProfile(targetedCatalogRaw)
	if !ok {
		t.Fatal("invalid targeted catalog fixture")
	}
	bestServerCurrentQualityCache.Lock()
	oldEntry := bestServerCurrentQualityCache.Entry
	bestServerCurrentQualityCache.Entry = bestServerCurrentQualityCacheEntry{}
	bestServerCurrentQualityCache.Unlock()
	t.Cleanup(func() {
		bestServerCurrentQualityCache.Lock()
		bestServerCurrentQualityCache.Entry = oldEntry
		bestServerCurrentQualityCache.Unlock()
	})
	return a, bestServerInternalCandidate{Profile: profile, Raw: targetedCatalogRaw}, measuredAt.Format(time.RFC3339)
}

func targetedCatalogWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func targetedCatalogRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func targetedCatalogProbeResult() bestServerQualityApplicationResult {
	return bestServerQualityApplicationResult{
		OK: true,
		HTTP: bestServerProbeResult{OK: true, Samples: []int{100, 101, 102, 103, 104}, Median: 102, Jitter: 4},
		DownloadOK: true, DownloadMbps: 90,
		Media: bestServerMediaQualityResult{
			OK: true, Samples: 3, MedianMbps: 50, MinMbps: 48,
			ServiceOK: 4, ServiceTotal: 4, Grade: "good",
		},
	}
}

func targetedCatalogHooks(t *testing.T, probe func(*app, context.Context, bestServerInternalCandidate) bestServerQualityApplicationResult) *int {
	t.Helper()
	oldProbe, oldDirect, oldVPN := bestServerTargetedQualityProbe, directSubscriptionBodyFetch, activeVPNSubscriptionBodyFetch
	t.Cleanup(func() {
		bestServerTargetedQualityProbe, directSubscriptionBodyFetch, activeVPNSubscriptionBodyFetch = oldProbe, oldDirect, oldVPN
	})
	bestServerTargetedQualityProbe = probe
	fetches := new(int)
	// A provider can legitimately rotate SNI on every read. The targeted
	// handler must not request this new revision after the user chose an ID.
	rotated := strings.Replace(targetedCatalogRaw, "old.example.invalid", "new.example.invalid", 1)
	directSubscriptionBodyFetch = func(context.Context, *url.URL) ([]byte, error) {
		(*fetches)++
		return []byte(rotated+"\n"), nil
	}
	activeVPNSubscriptionBodyFetch = func(*app, context.Context, *url.URL) ([]byte, error) {
		(*fetches)++
		return []byte(rotated+"\n"), nil
	}
	return fetches
}

func targetedCatalogRequest(t *testing.T, a *app, id string) (int, bestServerQualityResponse) {
	t.Helper()
	w := httptest.NewRecorder()
	a.handleBestServerCandidateRetry(w, httptest.NewRequest(http.MethodGet, "/api/vpn/best-candidate?id="+url.QueryEscape(id), nil))
	var response bestServerQualityResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Mutation != "NONE" {
		t.Fatal("targeted measurement must not mutate the live VPN")
	}
	for _, forbidden := range []string{targetedCatalogSource, "vless://", "11111111-1111-4111-8111-111111111111", "old.example.invalid", "new.example.invalid", "pbk=", "sid="} {
		if strings.Contains(w.Body.String(), forbidden) {
			t.Fatal("targeted response exposed private catalog data")
		}
	}
	return w.Code, response
}

func TestTargetedCandidateUsesExactLocalCatalogDespiteSNIRotation(t *testing.T) {
	for _, rotateCacheDuringProbe := range []bool{false, true} {
		name := "upstream-rotated"
		if rotateCacheDuringProbe {
			name = "cache-rotated-during-probe"
		}
		t.Run(name, func(t *testing.T) {
			a, target, updatedAt := targetedCatalogFixture(t)
			currentEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
			currentFilter := readBestServerCurrentFilter(a.cfg.FilterPath)
			beforeOut, beforeFilter := targetedCatalogRead(t, a.cfg.OutPath), targetedCatalogRead(t, a.cfg.FilterPath)
			rotatedRaw := strings.Replace(target.Raw, "old.example.invalid", "new.example.invalid", 1)
			rotated, ok := parseSafeVLESSProfile(rotatedRaw)
			if !ok || rotated.ID == target.Profile.ID || rotated.Name != target.Profile.Name || profileEndpoint(rotated) != profileEndpoint(target.Profile) {
				t.Fatal("fixture must reproduce ID rotation without endpoint or label changes")
			}
			probes := 0
			fetches := targetedCatalogHooks(t, func(gotApp *app, ctx context.Context, got bestServerInternalCandidate) bestServerQualityApplicationResult {
				probes++
				if gotApp != a || got.Raw != target.Raw || got.Profile != target.Profile {
					t.Fatal("probe did not use the exact selected catalog entry")
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) <= 0 || time.Until(deadline) > bestServerQualityCandidateTimeout {
					t.Fatal("targeted probe lost its bounded candidate context")
				}
				if rotateCacheDuringProbe {
					if err := saveProviderSubscriptionCache(targetedCatalogSource, []byte(rotatedRaw)); err != nil {
						t.Fatal(err)
					}
				}
				return targetedCatalogProbeResult()
			})
			status, response := targetedCatalogRequest(t, a, target.Profile.ID)
			candidate := qualityCandidateByID(response.Candidates, target.Profile.ID)
			if status != http.StatusOK || !response.Success || candidate == nil || !candidate.Eligible || response.SelectionToken == "" {
				t.Fatalf("selected local candidate was not measured and authorized: status=%d error=%q", status, response.Error)
			}
			if probes != 1 || *fetches != 0 || response.CatalogFresh || response.CatalogSource != "protected_cache" || response.CatalogUpdatedAt != updatedAt {
				t.Fatal("targeted request fetched or misrepresented the catalog")
			}
			got, err := a.loadBestServerSelectionCandidate(response.SelectionToken, target.Profile.ID, currentEndpoint, currentFilter)
			if err != nil || got.Raw != target.Raw || got.Profile != target.Profile {
				t.Fatal("selection token did not retain the exact measured Raw")
			}
			if _, err := a.loadBestServerSelectionCandidate(response.SelectionToken, rotated.ID, currentEndpoint, currentFilter); err == nil {
				t.Fatal("a different SNI revision must not inherit the measured token")
			}
			var saved bestServerSelectionSnapshot
			if err := json.Unmarshal([]byte(targetedCatalogRead(t, bestServerSelectionSnapshotPath(response.SelectionToken))), &saved); err != nil {
				t.Fatal(err)
			}
			if saved.SourceFingerprint != providerSubscriptionSourceFingerprint(targetedCatalogSource) || saved.CurrentEndpoint != currentEndpoint || saved.CurrentFilterHash != bestServerSelectionFilterHash(currentFilter) {
				t.Fatal("selection snapshot lost source or current identity binding")
			}
			if targetedCatalogRead(t, a.cfg.OutPath) != beforeOut || targetedCatalogRead(t, a.cfg.FilterPath) != beforeFilter {
				t.Fatal("targeted measurement changed the live VPN")
			}
			targetedCatalogWrite(t, a.cfg.SubPath, "https://other.example.invalid/different-source")
			if _, err := a.loadBestServerSelectionCandidate(response.SelectionToken, target.Profile.ID, currentEndpoint, currentFilter); err == nil {
				t.Fatal("selection token must remain bound to the original subscription")
			}
		})
	}
}

func TestTargetedCandidateRefusesUnknownOrUnboundLocalCatalog(t *testing.T) {
	for _, scenario := range []string{"missing-cache", "different-source", "unsafe-cache", "unknown-id", "active-endpoint"} {
		t.Run(scenario, func(t *testing.T) {
			a, target, _ := targetedCatalogFixture(t)
			id, wantStatus := target.Profile.ID, http.StatusServiceUnavailable
			switch scenario {
			case "missing-cache":
				if err := os.Remove(providerSubscriptionCachePath()); err != nil {
					t.Fatal(err)
				}
			case "different-source":
				targetedCatalogWrite(t, a.cfg.SubPath, "https://other.example.invalid/different-source")
			case "unsafe-cache":
				if err := os.Chmod(providerSubscriptionCachePath(), 0644); err != nil {
					t.Fatal(err)
				}
			case "unknown-id":
				id, wantStatus = "0000000000000000", http.StatusNotFound
				if id == target.Profile.ID {
					t.Fatal("unknown fixture ID collision")
				}
			case "active-endpoint":
				targetedCatalogWrite(t, a.cfg.OutPath, `{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"198.51.100.91","port":443}]}}]}`)
				wantStatus = http.StatusConflict
			}
			probes := 0
			fetches := targetedCatalogHooks(t, func(*app, context.Context, bestServerInternalCandidate) bestServerQualityApplicationResult {
				probes++
				return targetedCatalogProbeResult()
			})
			status, response := targetedCatalogRequest(t, a, id)
			if status != wantStatus || response.Success || response.SelectionToken != "" || probes != 0 || *fetches != 0 {
				t.Fatalf("unbound/unknown candidate did not fail closed: status=%d probes=%d fetches=%d", status, probes, *fetches)
			}
		})
	}
}

func TestTargetedCandidateDiscardsChangedIdentityDuringProbe(t *testing.T) {
	for _, scenario := range []string{"subscription", "endpoint", "filter"} {
		t.Run(scenario, func(t *testing.T) {
			a, target, _ := targetedCatalogFixture(t)
			probes := 0
			fetches := targetedCatalogHooks(t, func(*app, context.Context, bestServerInternalCandidate) bestServerQualityApplicationResult {
				probes++
				switch scenario {
				case "subscription":
					targetedCatalogWrite(t, a.cfg.SubPath, "https://other.example.invalid/different-source")
				case "endpoint":
					targetedCatalogWrite(t, a.cfg.OutPath, `{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"198.51.100.246","port":18443}]}}]}`)
				case "filter":
					targetedCatalogWrite(t, a.cfg.FilterPath, "^US Different Fixture$")
				}
				return targetedCatalogProbeResult()
			})
			status, response := targetedCatalogRequest(t, a, target.Profile.ID)
			if status != http.StatusConflict || response.Success || response.SelectionToken != "" || probes != 1 || *fetches != 0 {
				t.Fatalf("changed identity authorized a stale result: status=%d", status)
			}
			entries, err := os.ReadDir(bestServerSelectionSnapshotDir())
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatal("discarded measurement left an authorization token")
			}
		})
	}
}

func TestTargetedCandidateStillRequiresCompleteEligibleMeasurement(t *testing.T) {
	for _, scenario := range []string{"missing-throughput", "partial-services", "failed-probe"} {
		t.Run(scenario, func(t *testing.T) {
			a, target, updatedAt := targetedCatalogFixture(t)
			probes := 0
			fetches := targetedCatalogHooks(t, func(*app, context.Context, bestServerInternalCandidate) bestServerQualityApplicationResult {
				probes++
				result := targetedCatalogProbeResult()
				switch scenario {
				case "missing-throughput":
					result.DownloadOK, result.DownloadMbps = false, 0
				case "partial-services":
					result.Media.ServiceOK = 2
				case "failed-probe":
					result.OK = false
				}
				return result
			})
			status, response := targetedCatalogRequest(t, a, target.Profile.ID)
			candidate := qualityCandidateByID(response.Candidates, target.Profile.ID)
			if status != http.StatusOK || !response.Success || candidate == nil || !candidate.Tested || candidate.Eligible || response.SelectionToken != "" || probes != 1 || *fetches != 0 {
				t.Fatalf("incomplete quality must remain diagnostic-only: status=%d", status)
			}
			if response.CatalogFresh || response.CatalogSource != "protected_cache" || response.CatalogUpdatedAt != updatedAt {
				t.Fatal("diagnostic-only result lost local catalog provenance")
			}
		})
	}
}
