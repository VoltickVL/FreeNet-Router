package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRefreshApplicationHTTPSAcceptanceIsNotMediaQuality(t *testing.T) {
	// QoE warnings must remain available to AutoVPN, but a completed
	// named HTTPS request through the stable current outbound is live proof.
	qoe := classifyAutomationReachableQuality(720, 0, 4)
	if qoe.State != automationHealthUncertain || !qoe.QualityDegraded {
		t.Fatalf("unexpected QoE regression: %+v", qoe)
	}
	app := classifyRefreshApplicationAcceptance(context.Background(), true, true)
	if app.State != automationHealthHealthy {
		t.Fatalf("healthy application HTTPS must not inherit QoE warning: %+v", app)
	}
}

func TestRefreshApplicationAcceptanceFailsClosed(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		ctx context.Context
		https, unchanged bool
	}{
		{"nil_context", nil, true, true},
		{"canceled", canceled, true, true},
		{"https_unconfirmed", context.Background(), false, true},
		{"identity_drift", context.Background(), true, false},
		{"neither", context.Background(), false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyRefreshApplicationAcceptance(tc.ctx, tc.https, tc.unchanged)
			if got.State != automationHealthUncertain {
				t.Fatalf("incomplete postcheck must not confirm live VPN: %+v", got)
			}
		})
	}
	deadline, cancelDeadline := context.WithCancelCause(context.Background())
	cancelDeadline(errors.New("expired"))
	if got := classifyRefreshApplicationAcceptance(deadline, true, true); got.State == automationHealthHealthy {
		t.Fatalf("canceled context produced success: %+v", got)
	}
}

func TestRefreshPostCheckDiagnosticsDoNotLeakReasons(t *testing.T) {
	for _, tc := range []struct {
		name, state, reason, want string
	}{
		{"busy", automationHealthUncertain, "Ресурс изолированной проверки VPN сейчас занят.", "PROBE_BUSY"},
		{"timed_out", automationHealthUncertain, "VPN health probe истёк до подтверждения транспортного состояния.", "PROBE_INCOMPLETE"},
		{"start", automationHealthUncertain, "Локальная проверка VPN не запустилась.", "PROBE_START_FAILED"},
		{"named", automationHealthFailed, "оба независимых HTTPS недоступны", "NAMED_HTTPS_FAILED"},
		{"unknown", automationHealthUncertain, "reality-uuid=secret-12345", "VPN_APPLICATION_UNVERIFIED"},
		{"failed", automationHealthFailed, "secret-12345", "VPN_APPLICATION_FAILED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code := bestServerRefreshPostCheckCode(automationHealthProbe{State: tc.state, Reason: tc.reason})
			if code != tc.want || strings.Contains(code, "secret-12345") {
				t.Fatalf("unsafe or misleading diagnostic: %q want %q", code, tc.want)
			}
		})
	}
	if got := bestServerRefreshSafePostState("secret-12345"); got != "unverified" {
		t.Fatalf("unknown probe state leaked: %q", got)
	}
}

func TestRefreshPostCheckUsesApplicationOnlyWithoutDisablingAutoQuality(t *testing.T) {
	refreshBytes, err := os.ReadFile("best_server_targeted.go")
	if err != nil { t.Fatal(err) }
	refresh := string(refreshBytes)
	start := strings.Index(refresh, "var bestServerRefreshPostProbe =")
	end := strings.Index(refresh[start:], "var bestServerTargetedQualityProbe =")
	if start < 0 || end < 0 {
		t.Fatal("cannot locate refresh postcheck source")
	}
	post := refresh[start:start+end]
	if !strings.Contains(post, "a.probeAutomationCurrentVPNApplicationOnly(ctx)") ||
		strings.Contains(post, "return a.probeAutomationCurrentVPN(ctx)") {
		t.Fatal("refresh postcheck must not depend on media and QoE scores")
	}
	healthBytes, err := os.ReadFile("automation_health.go")
	if err != nil { t.Fatal(err) }
	health := string(healthBytes)
	if !strings.Contains(health, "func (a *app) probeAutomationCurrentVPN(ctx context.Context) automationHealthProbe {\n\treturn a.probeAutomationCurrentVPNWithMode(ctx, false)") {
		t.Fatal("background AutoVPN health semantics must remain full QoE")
	}
	mode := strings.Index(health, "if applicationOnly {")
	service := strings.Index(health, "services := probeBestServerServiceReachabilityDetailed(ctx, curlPath, socks)")
	if mode < 0 || service < 0 || mode >= service {
		t.Fatal("refresh application-only acceptance must occur before QoE/service probes")
	}
}
