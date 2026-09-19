package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func resetSubscriptionRefreshGroupForTest() {
	subscriptionRefreshGroup.mu.Lock()
	subscriptionRefreshGroup.inFlight = false
	subscriptionRefreshGroup.done = nil
	subscriptionRefreshGroup.result = subscriptionRefreshResult{}
	subscriptionRefreshGroup.err = nil
	subscriptionRefreshGroup.mu.Unlock()
}

func testSafeSubscriptionProfiles() []subscriptionProfile {
	return []subscriptionProfile{
		{ID: "0123456789abcdef", Name: "DE Frankfurt, Germany, Extra", CountryCode: "de", Address: "203.0.113.10", Port: 443},
		{ID: "fedcba9876543210", Name: "NL Amsterdam, Netherlands, Extra", CountryCode: "nl", Address: "198.51.100.20", Port: 8443},
	}
}

func TestSubscriptionRefreshDefaultEnabledPreservesExplicitOff(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "freenet.conf")
	if err := os.WriteFile(configPath, []byte("AUTO_VPN_V1=no\n"), 0600); err != nil {
		t.Fatal(err)
	}
	values := settingsV3ManagedCronValuesFromConfig(configPath)
	if got := values["AUTO_SUBSCRIPTION_REFRESH_ENABLED"]; got != "yes" {
		t.Fatalf("missing key effective state=%q want yes", got)
	}

	if err := os.WriteFile(configPath, []byte("AUTO_SUBSCRIPTION_REFRESH_ENABLED=no\n"), 0600); err != nil {
		t.Fatal(err)
	}
	values = settingsV3ManagedCronValuesFromConfig(configPath)
	if got := values["AUTO_SUBSCRIPTION_REFRESH_ENABLED"]; got != "no" {
		t.Fatalf("explicit off effective state=%q want no", got)
	}

	if err := os.WriteFile(configPath, []byte("AUTO_SUBSCRIPTION_REFRESH_ENABLED=yes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	values = settingsV3ManagedCronValuesFromConfig(configPath)
	if got := values["AUTO_SUBSCRIPTION_REFRESH_ENABLED"]; got != "yes" {
		t.Fatalf("explicit on effective state=%q want yes", got)
	}
}

func TestSubscriptionRefreshKeepsLastKnownGoodOnFailure(t *testing.T) {
	resetSubscriptionRefreshGroupForTest()
	t.Setenv("FREENET_SUBSCRIPTION_PROFILES_CACHE", filepath.Join(t.TempDir(), "profiles.json"))
	oldDiscovery := subscriptionProfileDiscovery
	t.Cleanup(func() {
		subscriptionProfileDiscovery = oldDiscovery
		resetSubscriptionRefreshGroupForTest()
	})

	profiles := testSafeSubscriptionProfiles()
	subscriptionProfileDiscovery = func(_ *app, _ context.Context) ([]subscriptionProfile, error) {
		return cloneSubscriptionProfiles(profiles), nil
	}
	a := &app{}
	fresh, err := a.refreshSubscriptionProfiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Stale || len(fresh.Profiles) != len(profiles) || fresh.UpdatedAt == "" {
		t.Fatalf("unexpected fresh result: %+v", fresh)
	}

	raw, err := os.ReadFile(subscriptionProfilesCachePath())
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"vless://", "uuid=", "pbk=", "sid=", "https://"} {
		if strings.Contains(strings.ToLower(string(raw)), forbidden) {
			t.Fatalf("safe cache leaked %q: %s", forbidden, raw)
		}
	}

	subscriptionProfileDiscovery = func(_ *app, _ context.Context) ([]subscriptionProfile, error) {
		return nil, errors.New("subscription fetch failed")
	}
	stale, err := a.refreshSubscriptionProfiles(context.Background())
	if err == nil {
		t.Fatal("expected refresh error")
	}
	if !stale.Stale || len(stale.Profiles) != len(profiles) || stale.UpdatedAt != fresh.UpdatedAt {
		t.Fatalf("last-known-good not preserved: fresh=%+v stale=%+v", fresh, stale)
	}
}

func TestSubscriptionRefreshConcurrentCallsShareOneDiscovery(t *testing.T) {
	resetSubscriptionRefreshGroupForTest()
	t.Setenv("FREENET_SUBSCRIPTION_PROFILES_CACHE", filepath.Join(t.TempDir(), "profiles.json"))
	oldDiscovery := subscriptionProfileDiscovery
	t.Cleanup(func() {
		subscriptionProfileDiscovery = oldDiscovery
		resetSubscriptionRefreshGroupForTest()
	})

	var calls int32
	started := make(chan struct{})
	release := make(chan struct{})
	subscriptionProfileDiscovery = func(_ *app, _ context.Context) ([]subscriptionProfile, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			close(started)
		}
		<-release
		return testSafeSubscriptionProfiles(), nil
	}

	a := &app{}
	type outcome struct {
		result subscriptionRefreshResult
		err    error
	}
	out := make(chan outcome, 2)
	go func() {
		result, err := a.refreshSubscriptionProfiles(context.Background())
		out <- outcome{result: result, err: err}
	}()
	<-started
	go func() {
		result, err := a.refreshSubscriptionProfiles(context.Background())
		out <- outcome{result: result, err: err}
	}()
	time.Sleep(25 * time.Millisecond)
	close(release)

	for i := 0; i < 2; i++ {
		got := <-out
		if got.err != nil || got.result.Stale || len(got.result.Profiles) != 2 {
			t.Fatalf("unexpected shared result: %+v err=%v", got.result, got.err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("discovery calls=%d want 1", got)
	}
}

func TestSubscriptionNextCronRunMatchesManagedSchedule(t *testing.T) {
	now := time.Date(2026, 9, 20, 5, 41, 0, 0, time.UTC)
	cases := map[string]string{
		"30m": "2026-09-20T06:00:00Z",
		"1h":  "2026-09-20T06:00:00Z",
		"3h":  "2026-09-20T06:00:00Z",
		"6h":  "2026-09-20T06:00:00Z",
		"12h": "2026-09-20T12:00:00Z",
		"24h": "2026-09-21T04:17:00Z",
	}
	for interval, want := range cases {
		if got := subscriptionNextCronRun(interval, now); got != want {
			t.Fatalf("%s next=%q want %q", interval, got, want)
		}
	}
}
