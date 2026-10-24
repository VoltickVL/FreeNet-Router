package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestChatGPTHTTPSClassificationDoesNotEquate403WithVPNOutage(t *testing.T) {
	cases := []struct{ code, expected string }{
		{"200", bestServerChatGPTHTTPSResponded},
		{"204", bestServerChatGPTHTTPSResponded},
		{"302", bestServerChatGPTHTTPSResponded},
		{"401", bestServerChatGPTHTTPSRestricted},
		{"403", bestServerChatGPTHTTPSRestricted},
		{"405", bestServerChatGPTHTTPSRestricted},
		{"429", bestServerChatGPTHTTPSRestricted},
		{"503", bestServerChatGPTHTTPError},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			curl, recordsDir := newServiceCurlFixture(t, "status")
			t.Setenv("FREENET_SERVICE_CURL_STATUS", tc.code)
			got := probeBestServerChatGPTHTTPS(context.Background(), curl, "127.0.0.1:18181")
			if got != tc.expected {
				t.Fatalf("ChatGPT HTTP %s: got %s, want %s", tc.code, got, tc.expected)
			}
			records := serviceCurlFixtureRecords(t, recordsDir)
			if len(records) != 1 || records[0].Service != 4 {
				t.Fatalf("unexpected ChatGPT request count/target: %+v", records)
			}
			assertServiceCurlChildrenReaped(t, records)
		})
	}
}

func TestChatGPTHTTPSMalformedTLSErrorAndDeadlineRemainUnknown(t *testing.T) {
	for _, mode := range []string{"certificate", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			curl, recordsDir := newServiceCurlFixture(t, mode)
			got := probeBestServerChatGPTHTTPS(context.Background(), curl, "127.0.0.1:18181")
			if got != bestServerChatGPTHTTPSUnknown {
				t.Fatalf("unknown %s probe turned into a VPN verdict: %q", mode, got)
			}
			assertServiceCurlChildrenReaped(t, serviceCurlFixtureRecords(t, recordsDir))
		})
	}
	curl, recordsDir := newServiceCurlFixture(t, "hang")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan string, 1)
	go func() { done <- probeBestServerChatGPTHTTPS(ctx, curl, "127.0.0.1:18181") }()
	deadline := time.Now().Add(2 * time.Second)
	for len(serviceCurlFixtureRecords(t, recordsDir)) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(serviceCurlFixtureRecords(t, recordsDir)) != 1 {
		cancel()
		t.Fatal("optional ChatGPT fixture never started")
	}
	cancel()
	select {
	case got := <-done:
		if got != bestServerChatGPTHTTPSUnknown {
			t.Fatalf("cancelled probe was not unknown: %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled ChatGPT child did not exit within the bound")
	}
	assertServiceCurlChildrenReaped(t, serviceCurlFixtureRecords(t, recordsDir))
}

func TestChatGPTHTTPSDiagnosticCannotChangeQoEEligibilityOrServiceMask(t *testing.T) {
	if bestServerServiceAllMask != 15 || len(bestServerMediaServiceURLs) != 4 {
		t.Fatal("the four mandatory QoE service gates were modified")
	}
	candidate := bestServerQualityCandidate{
		Tested: true, Current: true, Available: true, Eligible: true,
		ApplicationMS: 147, JitterMS: 22, VPNRTTMS: 149,
		DownloadMbps: 80.5, MediaSamples: bestServerMediaRequiredRuns,
		MediaGrade: "good", ServiceOK: 4, ServiceTotal: 4,
	}
	eligible := eligibleBestServerQuality(candidate)
	if !eligible {
		t.Fatal("test precondition must be eligible")
	}
	for _, status := range []string{
		"", bestServerChatGPTHTTPSResponded,
		bestServerChatGPTHTTPSRestricted, bestServerChatGPTHTTPError,
		bestServerChatGPTHTTPSUnknown,
	} {
		candidate.ChatGPTHTTPSStatus = status
		if eligibleBestServerQuality(candidate) != eligible {
			t.Fatalf("ChatGPT HTTPS status %q changed recommendation eligibility", status)
		}
		encoded, err := json.Marshal(candidate)
		if err != nil {
			t.Fatal(err)
		}
		if status != "" && !strings.Contains(string(encoded), "\"chatgpt_https_status\":\""+status+"\"") {
			t.Fatalf("ChatGPT diagnostic missing in public candidate: %s", encoded)
		}
	}
	if got := sanitizeBestServerChatGPTHTTPSStatus("secret-bearing-URL-should-not-be-stored"); got != "" {
		t.Fatalf("untrusted ChatGPT status was persisted: %q", got)
	}
}

func TestChatGPTHTTPSUIAndMetricTooltipContract(t *testing.T) {
	source, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(source)
	for _, required := range []string{
		"chatgpt_https_status", "best-v4-chatgpt", "ChatGPT HTTPS",
		"https_response:", "http_restricted:", "http_error:", "unknown:",
		"if (hint) name.title = hint;",
	} {
		if !strings.Contains(js, required) {
			t.Fatalf("ChatGPT UI or non-overlapping metric tooltip contract missing: %q", required)
		}
	}
	if strings.Contains(js, "node.title = hint") {
		t.Fatal("metric's entire tile again has a native tooltip which covers its neighbor")
	}
	style, err := os.ReadFile("web/design-system.css")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(style), ".best-v4-chatgpt{grid-column:1 / -1;") {
		t.Fatal("informational ChatGPT line must span both metric columns")
	}
}
