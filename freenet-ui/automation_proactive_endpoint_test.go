package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAutomationBestCycleRefreshesCurrentEndpointBeforeForeignOptimization(t *testing.T) {
	oldRefresh := automationBestCurrentRefresh
	t.Cleanup(func() { automationBestCurrentRefresh = oldRefresh })

	dir := t.TempDir()
	configPath := filepath.Join(dir, "freenet.conf")
	if err := os.WriteFile(configPath, []byte(
		"AUTO_VPN_V1=yes\n"+
			"AUTO_VPN_V1_INTERVAL=1h\n"+
			"AUTO_VPN_MODE=best\n"+
			"AUTO_VPN_POLICY=better\n"+
			"AUTO_VPN_COUNTRY_SCOPE=region\n"+
			"AUTO_VPN_AUTO_APPLY=yes\n",
	), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_AUTO_BEST_LOCK", filepath.Join(dir, "best.lock"))
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "automation.state"))
	t.Setenv("FREENET_AUTOMATION_HISTORY", filepath.Join(dir, "automation.history"))

	called := 0
	automationBestCurrentRefresh = func(_ *app, _ context.Context) (int, bestServerRefreshResponse) {
		called++
		return 200, bestServerRefreshResponse{
			Success: true, Outcome: "applied", Applied: true,
			Mutation: "APPLIED", RollbackState: "NOT_NEEDED",
		}
	}

	a := &app{cfg: config{ConfigPath: configPath}}
	result, err := a.runAutomationBestCycle(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("current endpoint refresh calls=%d want 1", called)
	}
	if result.Result != "updated" || !result.Mutated {
		t.Fatalf("result=%+v want updated + mutated", result)
	}
}

func TestAutomationBestCycleStopsWhenCurrentEndpointRefreshIsUnsafe(t *testing.T) {
	oldRefresh := automationBestCurrentRefresh
	t.Cleanup(func() { automationBestCurrentRefresh = oldRefresh })

	dir := t.TempDir()
	configPath := filepath.Join(dir, "freenet.conf")
	if err := os.WriteFile(configPath, []byte(
		"AUTO_VPN_V1=yes\n"+
			"AUTO_VPN_V1_INTERVAL=1h\n"+
			"AUTO_VPN_MODE=best\n"+
			"AUTO_VPN_AUTO_APPLY=yes\n",
	), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_AUTO_BEST_LOCK", filepath.Join(dir, "best.lock"))
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "automation.state"))
	t.Setenv("FREENET_AUTOMATION_HISTORY", filepath.Join(dir, "automation.history"))

	automationBestCurrentRefresh = func(_ *app, _ context.Context) (int, bestServerRefreshResponse) {
		return 503, bestServerRefreshResponse{
			Success: false, Outcome: "check_failed", Applied: false,
			Mutation: "NONE", RollbackState: "NOT_APPLIED",
			Error: "fresh current endpoint decision is uncertain",
		}
	}

	a := &app{cfg: config{ConfigPath: configPath}}
	result, err := a.runAutomationBestCycle(context.Background(), false)
	if err == nil {
		t.Fatal("unsafe current endpoint refresh must stop the cycle with an error")
	}
	if result.Result != "uncertain" || result.Mutated {
		t.Fatalf("result=%+v want uncertain + no mutation", result)
	}
}
