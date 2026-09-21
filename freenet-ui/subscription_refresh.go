package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const subscriptionProfilesCachePathDefault = "/opt/var/lib/freenet/subscription-profiles.json"

type subscriptionProfilesCache struct {
	UpdatedAt string                `json:"updated_at"`
	Profiles  []subscriptionProfile `json:"profiles"`
}

type subscriptionRefreshResult struct {
	Profiles  []subscriptionProfile
	UpdatedAt string
	Stale     bool
}

var subscriptionProfileDiscovery = func(a *app, ctx context.Context) ([]subscriptionProfile, error) {
	return a.discoverSubscriptionProfiles(ctx)
}

var subscriptionRefreshGroup struct {
	mu       sync.Mutex
	inFlight bool
	done     chan struct{}
	result   subscriptionRefreshResult
	err      error
}

func subscriptionProfilesCachePath() string {
	if path := strings.TrimSpace(os.Getenv("FREENET_SUBSCRIPTION_PROFILES_CACHE")); path != "" {
		return path
	}
	return subscriptionProfilesCachePathDefault
}

func cloneSubscriptionProfiles(profiles []subscriptionProfile) []subscriptionProfile {
	out := make([]subscriptionProfile, len(profiles))
	copy(out, profiles)
	return out
}

func selectableSubscriptionProfiles(profiles []subscriptionProfile) []subscriptionProfile {
	out := make([]subscriptionProfile, 0, len(profiles))
	for _, profile := range profiles {
		code := strings.ToLower(strings.TrimSpace(profile.CountryCode))
		name := strings.ToLower(strings.TrimSpace(profile.Name))
		if code == "ru" || strings.Contains(name, "russia") || strings.Contains(name, "росси") {
			continue
		}
		if !isBaseExtraProfileName(profile.Name) {
			continue
		}
		out = append(out, profile)
	}
	return out
}

// Read paths consume the last successful safe catalog without forcing a
// provider fetch every time an unrelated page requests a plan. A fresh fetch
// is used only to bootstrap the cache when no last-known-good catalog exists.
func (a *app) subscriptionProfilesForRead(ctx context.Context) (subscriptionRefreshResult, error) {
	if cached, err := loadSubscriptionProfilesCache(); err == nil {
		cached.Stale = false
		return cached, nil
	}
	return a.refreshSubscriptionProfiles(ctx)
}

func validCachedSubscriptionProfile(p subscriptionProfile) bool {
	if len(p.ID) != 16 || p.Name == "" || p.Address == "" || p.Port < 1 || p.Port > 65535 {
		return false
	}
	for _, r := range p.ID {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	if sanitizeProfileName(p.Name) != p.Name {
		return false
	}
	if strings.ContainsAny(p.Address, "\r\n\x00 /?#@") {
		return false
	}
	return true
}

func loadSubscriptionProfilesCache() (subscriptionRefreshResult, error) {
	data, err := os.ReadFile(subscriptionProfilesCachePath())
	if err != nil {
		return subscriptionRefreshResult{}, err
	}
	var cache subscriptionProfilesCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return subscriptionRefreshResult{}, errors.New("subscription profile cache is invalid")
	}
	if _, err := time.Parse(time.RFC3339, cache.UpdatedAt); err != nil || len(cache.Profiles) == 0 || len(cache.Profiles) > 100 {
		return subscriptionRefreshResult{}, errors.New("subscription profile cache is invalid")
	}
	for _, profile := range cache.Profiles {
		if !validCachedSubscriptionProfile(profile) {
			return subscriptionRefreshResult{}, errors.New("subscription profile cache contains invalid profile")
		}
	}
	return subscriptionRefreshResult{Profiles: cloneSubscriptionProfiles(cache.Profiles), UpdatedAt: cache.UpdatedAt, Stale: true}, nil
}

func saveSubscriptionProfilesCache(profiles []subscriptionProfile, updatedAt string) error {
	if len(profiles) == 0 || len(profiles) > 100 {
		return errors.New("subscription profile cache is empty or oversized")
	}
	for _, profile := range profiles {
		if !validCachedSubscriptionProfile(profile) {
			return errors.New("subscription profile cache contains invalid profile")
		}
	}
	cache := subscriptionProfilesCache{UpdatedAt: updatedAt, Profiles: cloneSubscriptionProfiles(profiles)}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	path := subscriptionProfilesCachePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'), 0600)
}

func (a *app) performSubscriptionRefresh(ctx context.Context) (subscriptionRefreshResult, error) {
	profiles, err := subscriptionProfileDiscovery(a, ctx)
	if err != nil {
		if cached, cacheErr := loadSubscriptionProfilesCache(); cacheErr == nil {
			return cached, err
		}
		return subscriptionRefreshResult{}, err
	}
	updatedAt := time.Now().UTC().Format(time.RFC3339)
	if err := saveSubscriptionProfilesCache(profiles, updatedAt); err != nil {
		return subscriptionRefreshResult{}, errors.New("cannot persist safe subscription profile cache")
	}
	return subscriptionRefreshResult{Profiles: cloneSubscriptionProfiles(profiles), UpdatedAt: updatedAt}, nil
}

func (a *app) refreshSubscriptionProfiles(ctx context.Context) (subscriptionRefreshResult, error) {
	subscriptionRefreshGroup.mu.Lock()
	if subscriptionRefreshGroup.inFlight {
		done := subscriptionRefreshGroup.done
		subscriptionRefreshGroup.mu.Unlock()
		select {
		case <-done:
			subscriptionRefreshGroup.mu.Lock()
			result, err := subscriptionRefreshGroup.result, subscriptionRefreshGroup.err
			result.Profiles = cloneSubscriptionProfiles(result.Profiles)
			subscriptionRefreshGroup.mu.Unlock()
			return result, err
		case <-ctx.Done():
			return subscriptionRefreshResult{}, ctx.Err()
		}
	}
	subscriptionRefreshGroup.inFlight = true
	subscriptionRefreshGroup.done = make(chan struct{})
	done := subscriptionRefreshGroup.done
	subscriptionRefreshGroup.mu.Unlock()

	result, err := a.performSubscriptionRefresh(ctx)

	subscriptionRefreshGroup.mu.Lock()
	subscriptionRefreshGroup.result = result
	subscriptionRefreshGroup.result.Profiles = cloneSubscriptionProfiles(result.Profiles)
	subscriptionRefreshGroup.err = err
	subscriptionRefreshGroup.inFlight = false
	close(done)
	subscriptionRefreshGroup.mu.Unlock()
	return result, err
}

func subscriptionNextCronRun(interval string, now time.Time) string {
	var next time.Time
	switch strings.TrimSpace(interval) {
	case "30m":
		next = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
		if now.Minute() >= 30 {
			next = next.Add(time.Hour)
		} else {
			next = next.Add(30 * time.Minute)
		}
	case "1h":
		next = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location()).Add(time.Hour)
	case "3h", "6h", "12h":
		hours := map[string]int{"3h": 3, "6h": 6, "12h": 12}[strings.TrimSpace(interval)]
		nextHour := ((now.Hour() / hours) + 1) * hours
		next = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Add(time.Duration(nextHour) * time.Hour)
	case "24h":
		next = time.Date(now.Year(), now.Month(), now.Day(), 4, 17, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
	default:
		return ""
	}
	return next.Format(time.RFC3339)
}
