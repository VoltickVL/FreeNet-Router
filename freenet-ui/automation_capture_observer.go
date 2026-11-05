package main

import (
	"context"
	"strings"
	"time"
)

// The existing 30s/1m AUTO watchdog owns this observation. No new timer,
// network connection, kernel mutation, client identifier or log of raw rules.
const automationCapturePathInterval = 5 * time.Minute
const automationCapturePathTimeout = 8 * time.Second

type automationCapturePathStatus struct {
	State      string `json:"state"`
	TCP        string `json:"tcp"`
	UDP        string `json:"udp"`
	ObservedAt string `json:"observed_at,omitempty"`
	HookActivity automationCaptureFlowStatus `json:"hook_activity"`
}

func automationCaptureState(raw string) string {
	switch strings.TrimSpace(raw) {
	case "tcp-redirect-missing", "tcp-hook-missing", "capture-rule-missing",
		"policy-rule-missing", "local-route-missing", "installed-unverified", "unverified":
		return raw
	default:
		return "unverified"
	}
}

// XKeen supports TProxy (TCP+UDP), Hybrid (TCP REDIRECT+UDP TProxy)
// and Redirect (TCP only). A Hybrid-oriented inventory cannot diagnose a
// *configured* mode from its absence alone. Treat the two ambiguous shapes
// as UNKNOWN rather than fabricating a missing-path alert.
func automationCaptureObservedState(observed captureDoctorResponse) string {
	if observed.State == "tcp-redirect-missing" && observed.Netfilter != "tproxy-rule-absent" {
		return "unverified" // pure TProxy is possible; no NAT TCP is expected
	}
	if observed.State == "capture-rule-missing" &&
		observed.TCPRedirect == "tcp-redirect-present" &&
		observed.TCPPrerouting == "tcp-prerouting-hook-present" {
		return "unverified" // TCP-only Redirect may be intentional
	}
	return automationCaptureState(observed.State)
}

func automationCaptureTCP(raw string) string {
	switch strings.TrimSpace(raw) {
	case "tcp-prerouting-hook-present", "tcp-prerouting-hook-absent",
		"tcp-prerouting-invalid-only":
		return raw
	default:
		return "unknown"
	}
}

func automationCaptureUDP(raw string) string {
	switch strings.TrimSpace(raw) {
	case "tproxy-rule-present", "tproxy-rule-absent":
		return raw
	default:
		return "unknown"
	}
}

func automationCapturePathDue(last string, now time.Time) bool {
	if now.IsZero() {
		return false
	}
	if strings.TrimSpace(last) == "" {
		return true
	}
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(last))
	if err != nil {
		return false // corrupt reservation never authorizes unbounded probing
	}
	if at.After(now) {
		return false // clock moved backwards: preserve reservation
	}
	return !now.Before(at.Add(automationCapturePathInterval))
}

func automationCapturePathFromState(state map[string]string, now time.Time) automationCapturePathStatus {
	result := automationCapturePathStatus{State: "unverified", TCP: "unknown", UDP: "unknown",
		HookActivity: automationCaptureFlowFromState(state, now)}
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(state["CLIENT_CAPTURE_LAST"]))
	if err != nil || now.IsZero() || at.After(now.Add(time.Minute)) ||
		now.Sub(at) > 2*automationCapturePathInterval {
		return result
	}
	result.ObservedAt = at.UTC().Format(time.RFC3339Nano)
	result.State = automationCaptureState(state["CLIENT_CAPTURE_STATE"])
	result.TCP = automationCaptureTCP(state["CLIENT_CAPTURE_TCP"])
	result.UDP = automationCaptureUDP(state["CLIENT_CAPTURE_UDP"])
	result.HookActivity = automationCaptureFlowFromState(state, now)
	return result
}

// A successful isolated VLESS/SOCKS check does not prove LAN interception.
// This function observes installed kernel rules independently and stores only
// bounded enums. Any missing rule is reported, NOT treated as a dead VPN
// endpoint and NEVER fed into AUTO server-switch/restart logic.
func (a *app) observeAutomationCapturePath(parent context.Context, now time.Time,
	inspect func(context.Context) captureDoctorResponse) {
	a.observeAutomationCapturePathWithCounters(parent, now, inspect, nil)
}

func (a *app) observeAutomationCapturePathWithCounters(parent context.Context, now time.Time,
	inspect func(context.Context) captureDoctorResponse,
	readCounters func(context.Context,string,...string)(string,bool)) {
	if a == nil || parent == nil || inspect == nil || now.IsZero() || parent.Err() != nil {
		return
	}
	now = now.UTC()
	token := now.Format(time.RFC3339Nano)
	claimed := false
	err := v3UpdateState(func(state map[string]string) map[string]string {
		if !automationCapturePathDue(state["CLIENT_CAPTURE_REQUEST_AT"], now) {
			return nil
		}
		claimed = true
		return map[string]string{
			"CLIENT_CAPTURE_REQUEST_AT": token,
			"CLIENT_CAPTURE_STATE": "unverified",
			"CLIENT_CAPTURE_TCP": "unknown",
			"CLIENT_CAPTURE_UDP": "unknown",
			"CLIENT_CAPTURE_LAST": "",
			"CLIENT_FLOW_TCP_RESULT":"unknown",
			"CLIENT_FLOW_UDP_RESULT":"unknown",
			"CLIENT_FLOW_OBSERVED_AT":"",
		}
	})
	if err != nil || !claimed {
		return
	}
	ctx, cancel := context.WithTimeout(parent, automationCapturePathTimeout)
	defer cancel()
	observed := inspect(ctx)
	var counters automationCaptureFlowSnapshot
	if readCounters != nil && ctx.Err() == nil {
		counters = readAutomationCaptureFlows(ctx,readCounters)
	}
	state := automationCaptureObservedState(observed)
	tcp := automationCaptureTCP(observed.TCPPrerouting)
	udp := automationCaptureUDP(observed.Netfilter)
	if ctx.Err() != nil {
		state, tcp, udp = "unverified", "unknown", "unknown"
	}
	finished := time.Now().UTC()
	_ = v3UpdateState(func(current map[string]string) map[string]string {
		// An older worker cannot replace a newer reserved observation.
		if current["CLIENT_CAPTURE_REQUEST_AT"] != token {
			return nil
		}
		updates := map[string]string{
			"CLIENT_CAPTURE_LAST": finished.Format(time.RFC3339Nano),
			"CLIENT_CAPTURE_STATE": state,
			"CLIENT_CAPTURE_TCP": tcp,
			"CLIENT_CAPTURE_UDP": udp,
		}
		for key,value := range automationCaptureFlowSave(current,counters,finished) {
			updates[key] = value
		}
		return updates
	})
}

func (a *app) observeScheduledAutomationCapturePath(parent context.Context) {
	a.observeAutomationCapturePathWithCounters(parent, time.Now().UTC(), func(ctx context.Context) captureDoctorResponse {
		return captureDoctorClassify(ctx, captureDoctorRead, "scheduled-kernel-inventory")
	},captureDoctorRead)
}
