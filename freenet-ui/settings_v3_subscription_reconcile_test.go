package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func prepareScheduledSubscriptionTest(t *testing.T, enabled bool) (*app, *int) {
	t.Helper()
	resetSubscriptionRefreshGroupForTest()
	dir := t.TempDir()
	t.Setenv("FREENET_SUBSCRIPTION_PROFILES_CACHE", filepath.Join(dir, "profiles.json"))
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(dir, "settings.state"))
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", filepath.Join(dir, "settings.history"))
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "automation.state"))
	t.Setenv("FREENET_AUTOMATION_HISTORY", filepath.Join(dir, "automation.history"))

	oldDiscovery := subscriptionProfileDiscovery
	oldRefresh := settingsV3ScheduledCurrentRefresh
	t.Cleanup(func() {
		subscriptionProfileDiscovery = oldDiscovery
		settingsV3ScheduledCurrentRefresh = oldRefresh
		resetSubscriptionRefreshGroupForTest()
	})
	subscriptionProfileDiscovery = func(_ *app, _ context.Context) ([]subscriptionProfile, error) {
		return []subscriptionProfile{{
			ID: "0123456789abcdef", Name: "DE Frankfurt, Germany, Extra",
			CountryCode: "de", Address: "203.0.113.10", Port: 443,
		}}, nil
	}

	configPath := filepath.Join(dir, "freenet.conf")
	flag := "no"
	if enabled {
		flag = "yes"
	}
	if err := os.WriteFile(configPath, []byte(
		"AUTO_VPN_V1="+flag+"\n"+
			"AUTO_VPN_MODE=best\n"+
			"AUTO_VPN_AUTO_APPLY=yes\n",
	), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	settingsV3ScheduledCurrentRefresh = func(_ *app, _ context.Context) (int, bestServerRefreshResponse) {
		calls++
		return http.StatusOK, bestServerRefreshResponse{
			Success: true, Outcome: "no_new", Mutation: "NONE", RollbackState: "NOT_NEEDED",
		}
	}
	return &app{cfg: config{ConfigPath: configPath, UpdateLock: filepath.Join(dir, "no-update-lock")}}, &calls
}

func TestScheduledSubscriptionRefreshReconcilesCurrentEndpoint(t *testing.T) {
	a, calls := prepareScheduledSubscriptionTest(t, true)
	if err := a.runV3ScheduledSubscription(context.Background()); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Fatalf("scheduled subscription refresh must reconcile the current endpoint exactly once; calls=%d", *calls)
	}
}

func TestScheduledSubscriptionRefreshSkipsEndpointReconcileWhenAutoVPNDisabled(t *testing.T) {
	a, calls := prepareScheduledSubscriptionTest(t, false)
	if err := a.runV3ScheduledSubscription(context.Background()); err != nil {
		t.Fatal(err)
	}
	if *calls != 0 {
		t.Fatalf("disabled AUTO VPN must not mutate or reconcile current endpoint; calls=%d", *calls)
	}
}

func TestManualSubscriptionCheckRemainsReadOnly(t *testing.T) {
	a, calls := prepareScheduledSubscriptionTest(t, true)
	req := httptest.NewRequest(http.MethodPost, "/api/settings-v3/action", strings.NewReader(`{"action":"subscription_check"}`))
	rec := httptest.NewRecorder()
	a.handleSettingsV3Action(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if *calls != 0 {
		t.Fatalf("manual subscription_check must remain read-only; scheduled current-endpoint reconcile calls=%d", *calls)
	}
}
