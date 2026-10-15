package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

const isolatedXrayProbeLimit = 2

var isolatedXrayProbeGate = make(chan struct{}, isolatedXrayProbeLimit)

func acquireIsolatedXrayProbe(ctx context.Context) (func(), bool) {
	if ctx == nil {
		return nil, false
	}
	select {
	case isolatedXrayProbeGate <- struct{}{}:
		return func() {
			select {
			case <-isolatedXrayProbeGate:
			default:
			}
		}, true
	case <-ctx.Done():
		return nil, false
	}
}


// acquireExclusiveIsolatedXrayProbe drains both shared probe slots before a
// production Xray cutover. The legacy XKeen init script checks "pidof xray"
// without distinguishing the live core from a background RTT/health probe.
// Holding every slot until the complete apply/rollback helper returns stops
// FreeNet from presenting an isolated worker as XKeen's production process.
// This is deliberately a bounded wait, not a forced kill of any Xray process.
// Unknown/external workers are independently rejected by the shell preflight.
func acquireExclusiveIsolatedXrayProbe(ctx context.Context) (func(), bool) {
	if ctx == nil || ctx.Err() != nil {
		return nil, false
	}
	count := 0
	release := func() {
		for i := 0; i < count; i++ {
			<-isolatedXrayProbeGate
		}
	}
	for count < cap(isolatedXrayProbeGate) {
		if ctx.Err() != nil {
			release()
			return nil, false
		}
		select {
		case isolatedXrayProbeGate <- struct{}{}:
			count++
		case <-ctx.Done():
			release()
			return nil, false
		}
	}
	if ctx.Err() != nil {
		release()
		return nil, false
	}
	var once sync.Once
	return func() { once.Do(release) }, true
}

func prepareIsolatedProbeOutbound(outbound map[string]any) (map[string]any, error) {
	if len(outbound) == 0 {
		return nil, errors.New("empty outbound")
	}
	raw, err := json.Marshal(outbound)
	if err != nil {
		return nil, err
	}
	var clone map[string]any
	if err := json.Unmarshal(raw, &clone); err != nil {
		return nil, err
	}
	stream, _ := clone["streamSettings"].(map[string]any)
	if stream == nil {
		stream = map[string]any{}
		clone["streamSettings"] = stream
	}
	sockopt, _ := stream["sockopt"].(map[string]any)
	if sockopt == nil {
		sockopt = map[string]any{}
		stream["sockopt"] = sockopt
	}
	// Isolated probes are router-originated Xray processes. mark=255 guarantees
	// they bypass XKeen OUTPUT interception when proxy_router is enabled.
	sockopt["mark"] = float64(255)
	return clone, nil
}

func tryAcquireFreeNetOperation(a *app) (func(), bool) {
	if a == nil || a.sem == nil {
		return nil, false
	}
	select {
	case a.sem <- struct{}{}:
		return func() { <-a.sem }, true
	default:
		return nil, false
	}
}
