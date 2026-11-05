package main

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"
)

// These are packet hits at existing IPv4 PREROUTING -> XKeen hooks, not traffic
// throughput, an application flow trace, a QUIC detector, or VPN egress proof.
// NAT chain counters usually count new connections rather than media bytes.
type automationCaptureFlowStatus struct {
	TCP        string `json:"tcp"`
	UDP        string `json:"udp"`
	ObservedAt string `json:"observed_at,omitempty"`
}

type automationCaptureFlowSnapshot struct {
	tcpPackets uint64
	udpPackets uint64
	tcpRules   uint64
	udpRules   uint64
	tcpValid   bool
	udpValid   bool
}

const automationCaptureFlowMaxPackets = uint64(math.MaxInt64)

func automationCaptureFlowCount(raw, table string) (tcpPackets, udpPackets, tcpRules, udpRules uint64, ok bool) {
	if len(raw) == 0 || len(raw) > captureDoctorMaxOutput ||
		!strings.Contains(raw, "*"+table+"\n") || !strings.Contains(raw, "\nCOMMIT\n") {
		return 0, 0, 0, 0, false
	}
	for _, rawLine := range strings.Split(raw, "\n") {
		fields := strings.Fields(strings.ToLower(rawLine))
		if len(fields) < 2 || fields[0] != "-a" || fields[1] != "prerouting" {
			continue
		}
		proto, target, packetValue := "", "", ""
		for i := 2; i < len(fields); i++ {
			switch fields[i] {
			case "-p":
				if i+1 < len(fields) { proto = fields[i+1] }
			case "-j":
				if i+1 < len(fields) { target = fields[i+1] }
			case "-c":
				if i+2 < len(fields) { packetValue = fields[i+1] }
			}
		}
		if target != "xkeen" || (proto != "tcp" && proto != "udp") ||
			(table == "nat" && proto != "tcp") {
			continue
		}
		// Emergency INVALID-only and DSCP-only hooks are not evidence
		// of normal client traffic reaching the default interceptor.
		if captureDoctorSpecialOnlyTCPRule(fields) { continue }
		if packetValue == "" { return 0, 0, 0, 0, false }
		n, err := strconv.ParseUint(packetValue, 10, 64)
		if err != nil || n > automationCaptureFlowMaxPackets { return 0, 0, 0, 0, false }
		if proto == "tcp" {
			if tcpPackets > automationCaptureFlowMaxPackets-n { return 0, 0, 0, 0, false }
			tcpPackets += n
			tcpRules++
		} else {
			if udpPackets > automationCaptureFlowMaxPackets-n { return 0, 0, 0, 0, false }
			udpPackets += n
			udpRules++
		}
	}
	return tcpPackets, udpPackets, tcpRules, udpRules, true
}

func readAutomationCaptureFlows(ctx context.Context, read func(context.Context,string,...string)(string,bool)) automationCaptureFlowSnapshot {
	var result automationCaptureFlowSnapshot
	if ctx == nil || read == nil || ctx.Err() != nil { return result }
	nat, ok := read(ctx,"iptables-save","-c","-t","nat")
	if !ok || ctx.Err()!=nil { return result }
	natTCP, _, natRules, _, valid := automationCaptureFlowCount(nat,"nat")
	if !valid { return result }
	mangle, ok := read(ctx,"iptables-save","-c","-t","mangle")
	if !ok || ctx.Err()!=nil { return result }
	mangleTCP, mangleUDP, mangleTCPRules, mangleUDPRules, valid := automationCaptureFlowCount(mangle,"mangle")
	if !valid || natTCP > automationCaptureFlowMaxPackets-mangleTCP { return result }
	result.tcpPackets = natTCP + mangleTCP
	result.udpPackets = mangleUDP
	result.tcpRules = natRules + mangleTCPRules
	result.udpRules = mangleUDPRules
	result.tcpValid = result.tcpRules > 0
	result.udpValid = result.udpRules > 0
	return result
}

func automationCaptureFlowFromState(state map[string]string, now time.Time) automationCaptureFlowStatus {
	result := automationCaptureFlowStatus{TCP:"unknown",UDP:"unknown"}
	if now.IsZero() { return result }
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(state["CLIENT_FLOW_OBSERVED_AT"]))
	if err != nil || at.IsZero() || at.After(now.Add(time.Minute)) ||
		now.Sub(at)>2*automationCapturePathInterval {
		return result
	}
	result.ObservedAt = at.UTC().Format(time.RFC3339Nano)
	switch state["CLIENT_FLOW_TCP_RESULT"] {
	case "observed","no-increase","baseline":result.TCP=state["CLIENT_FLOW_TCP_RESULT"]
	}
	switch state["CLIENT_FLOW_UDP_RESULT"] {
	case "observed","no-increase","baseline":result.UDP=state["CLIENT_FLOW_UDP_RESULT"]
	}
	return result
}

func automationCaptureFlowPrevious(state map[string]string, now time.Time) (tcp,udp,tcpRules,udpRules uint64, valid bool) {
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(state["CLIENT_FLOW_BASELINE_AT"]))
	if err != nil || at.IsZero() || at.After(now) ||
		now.Sub(at)>2*automationCapturePathInterval { return 0,0,0,0,false }
	values:=make([]uint64,0,4)
	for _,key:=range []string{"CLIENT_FLOW_TCP_PACKETS","CLIENT_FLOW_UDP_PACKETS",
		"CLIENT_FLOW_TCP_RULES","CLIENT_FLOW_UDP_RULES"}{
		v,err:=strconv.ParseUint(strings.TrimSpace(state[key]),10,64)
		if err!=nil || v>automationCaptureFlowMaxPackets {return 0,0,0,0,false}
		values=append(values,v)
	}
	return values[0],values[1],values[2],values[3],true
}

func automationCaptureFlowSave(state map[string]string, sample automationCaptureFlowSnapshot, at time.Time) map[string]string {
	updates:=map[string]string{
		"CLIENT_FLOW_TCP_RESULT":"unknown", "CLIENT_FLOW_UDP_RESULT":"unknown",
		"CLIENT_FLOW_OBSERVED_AT":at.Format(time.RFC3339Nano),
		"CLIENT_FLOW_BASELINE_AT":"", "CLIENT_FLOW_TCP_PACKETS":"",
		"CLIENT_FLOW_UDP_PACKETS":"", "CLIENT_FLOW_TCP_RULES":"",
		"CLIENT_FLOW_UDP_RULES":"",
	}
	// A failed read, incomplete rule set or missing hook resets the baseline:
	// "no increase" must never be inferred from missing data or idle clients.
	if !sample.tcpValid && !sample.udpValid {return updates}
	prevTCP,prevUDP,prevTCPRules,prevUDPRules,havePrevious:=automationCaptureFlowPrevious(state,at)
	if havePrevious {
		if prevTCPRules!=sample.tcpRules || prevUDPRules!=sample.udpRules ||
			prevTCP>sample.tcpPackets || prevUDP>sample.udpPackets {
			havePrevious=false // rule change, counter reset, or reload
		}
	}
	// Always seed a fresh baseline from a successful read. No current count
	// is ever serialized to the authenticated API or the technical journal.
	updates["CLIENT_FLOW_BASELINE_AT"]=at.Format(time.RFC3339Nano)
	updates["CLIENT_FLOW_TCP_PACKETS"]=strconv.FormatUint(sample.tcpPackets,10)
	updates["CLIENT_FLOW_UDP_PACKETS"]=strconv.FormatUint(sample.udpPackets,10)
	updates["CLIENT_FLOW_TCP_RULES"]=strconv.FormatUint(sample.tcpRules,10)
	updates["CLIENT_FLOW_UDP_RULES"]=strconv.FormatUint(sample.udpRules,10)
	if sample.tcpValid {
		switch {
		case !havePrevious: updates["CLIENT_FLOW_TCP_RESULT"]="baseline"
		case sample.tcpPackets>prevTCP: updates["CLIENT_FLOW_TCP_RESULT"]="observed"
		default: updates["CLIENT_FLOW_TCP_RESULT"]="no-increase"
		}
	}
	if sample.udpValid {
		switch {
		case !havePrevious: updates["CLIENT_FLOW_UDP_RESULT"]="baseline"
		case sample.udpPackets>prevUDP: updates["CLIENT_FLOW_UDP_RESULT"]="observed"
		default: updates["CLIENT_FLOW_UDP_RESULT"]="no-increase"
		}
	}
	return updates
}
