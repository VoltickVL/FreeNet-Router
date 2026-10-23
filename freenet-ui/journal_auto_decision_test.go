package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestJournalAutoDecisionPreservesIndependentTimelines(t *testing.T) {
	state := map[string]string{
		"LAST_RUN": "2026-10-23T09:05:00Z",
		"LAST_RESULT": "same",
		"LAST_REASON": "Текущий VPN работает; замена не требуется.",
		"LAST_SWITCH": "2026-10-23T09:18:00Z",
		"LAST_SWITCH_ORIGIN": "manual",
	}
	got := journalAutoDecisionFromState("🇫🇷 Париж, Франция, Extra", state)
	if got.CurrentProfile != "Париж, Франция, Extra" ||
		got.DecisionAt != state["LAST_RUN"] || got.DecisionResult != "same" ||
		got.DecisionReason != state["LAST_REASON"] || got.LastSwitchAt != state["LAST_SWITCH"] ||
		got.LastSwitchOrigin != "manual" {
		t.Fatalf("independent AUTO decision/manual switch history changed: %+v", got)
	}
	state["LAST_SWITCH_ORIGIN"] = "auto"
	if next := journalAutoDecisionFromState("🇫🇷 Париж, Франция, Extra", state); next.LastSwitchOrigin != "auto" {
		t.Fatalf("AUTO switch origin missing: %+v", next)
	}
}

func TestJournalAutoDecisionNeverGuessesAbsentOrInvalidFacts(t *testing.T) {
	for _, state := range []map[string]string{
		{},
		{"LAST_RUN":"2026-10-23T09:05:00Z", "LAST_RESULT":"same"},
		{"LAST_RUN":"not-a-time", "LAST_RESULT":"same", "LAST_REASON":"healthy"},
		{"LAST_SWITCH":"invalid", "LAST_SWITCH_ORIGIN":"auto"},
	} {
		got := journalAutoDecisionFromState("", state)
		if got.CurrentProfile != "" || got.DecisionAt != "" || got.DecisionReason != "" || got.LastSwitchAt != "" || got.LastSwitchOrigin != "" {
			t.Fatalf("unknown timeline presented as confirmed: %+v", got)
		}
	}
}

func TestJournalAutoDecisionAndHealthReasonRedactSecrets(t *testing.T) {
	rawReason := "VPN работает https://example.invalid/private?token=secret123 password=secret456"
	state := map[string]string{
		"LAST_RUN": "2026-10-23T09:05:00Z", "LAST_RESULT":"same", "LAST_REASON":rawReason,
		"LAST_SWITCH": "2026-10-23T09:06:00Z", "LAST_SWITCH_ORIGIN": "untrusted",
	}
	got := journalAutoDecisionFromState("🇫🇷 Париж, Франция, Extra", state)
	if got.DecisionAt == "" || got.DecisionResult != "same" {
		t.Fatalf("valid decision disappeared: %+v", got)
	}
	if got.LastSwitchOrigin != "" {
		t.Fatalf("untrusted origin returned: %+v", got)
	}
	for _, secret := range []string{"secret123", "secret456", "example.invalid"} {
		if strings.Contains(got.DecisionReason, secret) {
			t.Fatalf("decision leaked %q: %+v", secret, got)
		}
	}
	health := journalHealthFreshness(true, 30*time.Second, map[string]string{
		"HEALTH_LAST":"2026-10-23T09:04:59Z", "HEALTH_RESULT":"healthy",
		"HEALTH_MESSAGE":rawReason,
	}, time.Date(2026,10,23,9,5,0,0,time.UTC))
	if health.Freshness != "fresh" || health.LastMessage == "" {
		t.Fatalf("last recorded health explanation missing: %+v", health)
	}
	for _, secret := range []string{"secret123", "secret456", "example.invalid"} {
		if strings.Contains(health.LastMessage, secret) {
			t.Fatalf("health reason leaked %q: %+v", secret, health)
		}
	}
	encoded, err := json.Marshal(journalResponse{Health:health, AutoDecision:got})
	if err != nil { t.Fatal(err) }
	for _, key := range []string{"auto_decision", "last_message", "decision_reason", "last_switch_origin"} {
		if !strings.Contains(string(encoded), key) {
			t.Fatalf("journal API lost read-only explanation %s: %s", key, encoded)
		}
	}
}

func TestJournalDecisionUIShowsHistoryAndDoesNotApplyVPN(t *testing.T) {
	data, err := os.ReadFile("web/settings-v3-core.js")
	if err != nil { t.Fatal(err) }
	source := string(data)
	for _, contract := range []string{
		"fn3JournalAutoDecision", "fn3JournalAutoHistory",
		"state.journalDecision = data.auto_decision",
		"renderJournalAutoDecision();",
		"state.journalFilter = 'auto';",
		"state.journalDatePreset = '7d';",
		"last_switch_origin", "last_message",
		"Старая запись не доказывает",
	} {
		if !strings.Contains(source, contract) {
			t.Errorf("missing decision history contract %q", contract)
		}
	}
}
