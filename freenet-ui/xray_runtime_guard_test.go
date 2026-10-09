package main

import (
	"context"
	"testing"
	"time"
)

func TestPrepareIsolatedProbeOutboundAddsBypassMarkWithoutMutatingSource(t *testing.T) {
	source := map[string]any{
		"tag": "vless-reality",
		"streamSettings": map[string]any{
			"network": "tcp",
			"sockopt": map[string]any{"tcpFastOpen": true},
		},
	}
	got, err := prepareIsolatedProbeOutbound(source)
	if err != nil {
		t.Fatal(err)
	}
	stream := got["streamSettings"].(map[string]any)
	sockopt := stream["sockopt"].(map[string]any)
	if sockopt["mark"] != float64(255) {
		t.Fatalf("isolated probe mark=%v want 255", sockopt["mark"])
	}
	originalStream := source["streamSettings"].(map[string]any)
	originalSockopt := originalStream["sockopt"].(map[string]any)
	if _, exists := originalSockopt["mark"]; exists {
		t.Fatal("isolated probe preparation mutated live outbound source")
	}
}

func TestIsolatedXrayProbeGateBoundsGlobalConcurrency(t *testing.T) {
	releases := make([]func(), 0, isolatedXrayProbeLimit)
	for i := 0; i < isolatedXrayProbeLimit; i++ {
		release, ok := acquireIsolatedXrayProbe(context.Background())
		if !ok {
			t.Fatalf("slot %d was not acquired", i)
		}
		releases = append(releases, release)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if release, ok := acquireIsolatedXrayProbe(ctx); ok {
		release()
		t.Fatal("global isolated Xray gate exceeded its limit")
	}
	for _, release := range releases {
		release()
	}
	release, ok := acquireIsolatedXrayProbe(context.Background())
	if !ok {
		t.Fatal("global isolated Xray gate did not recover after release")
	}
	release()
}
