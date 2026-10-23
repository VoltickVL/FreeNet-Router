package main

import (
	"strings"
	"time"
)

// journalAutoDecisionStatus is a read-only snapshot of the last recorded AUTO
// decision and switch handoff. It is not proof that the recorded decision
// selected the currently active profile; the two timelines are independent.
type journalAutoDecisionStatus struct {
	CurrentProfile   string `json:"current_profile,omitempty"`
	DecisionAt       string `json:"decision_at,omitempty"`
	DecisionResult   string `json:"decision_result,omitempty"`
	DecisionReason   string `json:"decision_reason,omitempty"`
	LastSwitchAt     string `json:"last_switch_at,omitempty"`
	LastSwitchOrigin string `json:"last_switch_origin,omitempty"`
}

func journalValidDecisionTime(raw string) string {
	raw = strings.TrimSpace(raw)
	if _, err := time.Parse(time.RFC3339, raw); err != nil {
		return ""
	}
	return raw
}

func journalAutoDecisionFromState(profileLabel string, state map[string]string) journalAutoDecisionStatus {
	var view journalAutoDecisionStatus
	if strings.TrimSpace(profileLabel) != "" {
		view.CurrentProfile = journalDiagnosticRedact(profileDisplayName(profileLabel))
	}
	result := journalDiagnosticRedact(state["LAST_RESULT"])
	reason := journalDiagnosticRedact(state["LAST_REASON"])
	at := journalValidDecisionTime(state["LAST_RUN"])
	if result != "" && reason != "" && at != "" {
		view.DecisionAt = at
		view.DecisionResult = result
		view.DecisionReason = reason
	}
	view.LastSwitchAt = journalValidDecisionTime(state["LAST_SWITCH"])
	if view.LastSwitchAt != "" {
		switch strings.ToLower(strings.TrimSpace(state["LAST_SWITCH_ORIGIN"])) {
		case "auto":
			view.LastSwitchOrigin = "auto"
		case "manual":
			view.LastSwitchOrigin = "manual"
		}
	}
	return view
}

func (a *app) journalAutoDecision() journalAutoDecisionStatus {
	return journalAutoDecisionFromState(
		a.status().ProfileLabel,
		parseAutomationState(automationStatePath()),
	)
}
