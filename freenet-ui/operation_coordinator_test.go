package main

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestOperationCoordinatorSameTargetJoinsLeader(t *testing.T) {
	var c operationCoordinator
	leader, isLeader, conflict := c.begin("quick", "rotate")
	if leader == nil || !isLeader || conflict != nil {
		t.Fatalf("unexpected leader result: leader=%v isLeader=%v conflict=%v", leader, isLeader, conflict)
	}
	follower, followerLeader, followerConflict := c.begin("quick", "rotate")
	if followerLeader || followerConflict != nil || follower != leader {
		t.Fatalf("same target did not join active operation")
	}

	want := actionResult{Action: "rotate", OperationID: leader.state.ID, Success: true, Message: "ok"}
	c.finish(leader, http.StatusOK, want, true, want.Message, "")
	status, payload, ok := c.wait(context.Background(), follower)
	if !ok || status != http.StatusOK {
		t.Fatalf("wait status=%d ok=%v", status, ok)
	}
	got, ok := payload.(actionResult)
	if !ok || got.OperationID != want.OperationID || !got.Success {
		t.Fatalf("unexpected follower payload: %#v", payload)
	}
}

func TestOperationCoordinatorDifferentTargetConflictsWithMetadata(t *testing.T) {
	var c operationCoordinator
	leader, isLeader, _ := c.begin("provider", "0123456789abcdef")
	if !isLeader {
		t.Fatal("first operation must be leader")
	}
	_, secondLeader, conflict := c.begin("quick", "de")
	if secondLeader || conflict == nil {
		t.Fatal("different target must conflict")
	}
	if conflict.ID != leader.state.ID || conflict.Kind != "provider" || conflict.Target != "0123456789abcdef" || conflict.State != "running" {
		t.Fatalf("unexpected conflict metadata: %#v", conflict)
	}
	c.finish(leader, http.StatusBadGateway, actionResult{Success: false, Error: "failed"}, false, "", "failed")
}

func TestOperationCoordinatorSnapshotTracksTerminalState(t *testing.T) {
	var c operationCoordinator
	op, _, _ := c.begin("quick", "update")
	state, active, ok := c.snapshot()
	if !ok || !active || state.ID != op.state.ID || state.State != "running" {
		t.Fatalf("unexpected active snapshot: %#v active=%v ok=%v", state, active, ok)
	}
	c.finish(op, http.StatusOK, actionResult{Success: true}, true, "updated", "")
	state, active, ok = c.snapshot()
	if !ok || active || state.State != "success" || state.Result != "SUCCESS" || state.FinishedAt == "" {
		t.Fatalf("unexpected terminal snapshot: %#v active=%v ok=%v", state, active, ok)
	}
	if _, err := time.Parse(time.RFC3339Nano, state.StartedAt); err != nil {
		t.Fatalf("invalid started_at: %v", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, state.FinishedAt); err != nil {
		t.Fatalf("invalid finished_at: %v", err)
	}
}
