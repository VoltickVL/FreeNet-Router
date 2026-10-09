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


func TestExclusiveXrayCutoverWaitsForProbeAndBlocksNewProbes(t *testing.T) {
	activeRelease, ok := acquireIsolatedXrayProbe(context.Background())
	if !ok {
		t.Fatal("cannot acquire active isolated worker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	fenceAcquired := make(chan struct{})
	allowFenceRelease := make(chan struct{})
	fenceDone := make(chan struct{})
	go func() {
		defer close(fenceDone)
		release, obtained := acquireExclusiveIsolatedXrayProbe(ctx)
		if !obtained {
			return
		}
		close(fenceAcquired)
		<-allowFenceRelease
		release()
		release() // idempotent: cannot steal another worker's permit
	}()
	select {
	case <-fenceAcquired:
		activeRelease()
		t.Fatal("core cutover overlapped an existing isolated Xray probe")
	case <-time.After(30 * time.Millisecond):
	}
	activeRelease()
	select {
	case <-fenceAcquired:
	case <-time.After(time.Second):
		t.Fatal("core cutover did not start after the last isolated probe exited")
	}
	blockedCtx, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	if release, ok := acquireIsolatedXrayProbe(blockedCtx); ok {
		release()
		close(allowFenceRelease)
		<-fenceDone
		t.Fatal("new isolated Xray probe started during protected core cutover")
	}
	close(allowFenceRelease)
	select {
	case <-fenceDone:
	case <-time.After(time.Second):
		t.Fatal("core cutover fence was not released")
	}
	release, ok := acquireIsolatedXrayProbe(context.Background())
	if !ok {
		t.Fatal("isolated Xray probes never resumed after cutover")
	}
	release()
}

func TestExclusiveXrayCutoverCancellationReleasesPartiallyHeldSlots(t *testing.T) {
	activeRelease, ok := acquireIsolatedXrayProbe(context.Background())
	if !ok {
		t.Fatal("cannot acquire isolated worker for cancellation fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if release, obtained := acquireExclusiveIsolatedXrayProbe(ctx); obtained {
		release()
		activeRelease()
		t.Fatal("core fence ignored its timeout while a probe was active")
	}
	activeRelease()
	first, ok := acquireIsolatedXrayProbe(context.Background())
	if !ok {
		t.Fatal("canceled core fence leaked first global slot")
	}
	second, ok := acquireIsolatedXrayProbe(context.Background())
	if !ok {
		first()
		t.Fatal("canceled core fence leaked second global slot")
	}
	second()
	first()
}
