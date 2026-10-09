package main

import (
	"context"
	"encoding/json"
	"errors"
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
