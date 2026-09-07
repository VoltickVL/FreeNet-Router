package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type operationState struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Target     string `json:"target"`
	State      string `json:"state"`
	Result     string `json:"result,omitempty"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
}

type operationStateResponse struct {
	Success   bool            `json:"success"`
	Active    bool            `json:"active"`
	Operation *operationState `json:"operation,omitempty"`
}

type coordinatedOperation struct {
	state   operationState
	done    chan struct{}
	status  int
	payload any
}

type operationCoordinator struct {
	mu     sync.Mutex
	seq    uint64
	active *coordinatedOperation
	last   *operationState
}

var vpnOperations operationCoordinator

func cloneOperationState(in operationState) operationState { return in }

func (c *operationCoordinator) begin(kind, target string) (*coordinatedOperation, bool, *operationState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if current := c.active; current != nil {
		state := cloneOperationState(current.state)
		if state.Kind == kind && state.Target == target {
			return current, false, nil
		}
		return nil, false, &state
	}
	seq := atomic.AddUint64(&c.seq, 1)
	now := time.Now().UTC()
	op := &coordinatedOperation{
		state: operationState{
			ID:        fmt.Sprintf("vpn-%d-%d", now.UnixNano(), seq),
			Kind:      kind,
			Target:    target,
			State:     "running",
			StartedAt: now.Format(time.RFC3339Nano),
		},
		done: make(chan struct{}),
	}
	c.active = op
	return op, true, nil
}

func (c *operationCoordinator) finish(op *coordinatedOperation, status int, payload any, success bool, message, errText string) {
	if op == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != op {
		return
	}
	op.status = status
	op.payload = payload
	op.state.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	op.state.Message = message
	op.state.Error = errText
	if success {
		op.state.State = "success"
		op.state.Result = "SUCCESS"
	} else {
		op.state.State = "failed"
		op.state.Result = "FAIL"
	}
	state := cloneOperationState(op.state)
	c.last = &state
	c.active = nil
	close(op.done)
}

func (c *operationCoordinator) wait(ctx context.Context, op *coordinatedOperation) (int, any, bool) {
	if op == nil {
		return http.StatusInternalServerError, nil, true
	}
	select {
	case <-op.done:
		c.mu.Lock()
		status, payload := op.status, op.payload
		c.mu.Unlock()
		if status == 0 {
			status = http.StatusInternalServerError
		}
		return status, payload, true
	case <-ctx.Done():
		return 0, nil, false
	}
}

func (c *operationCoordinator) snapshot() (operationState, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.active != nil {
		return cloneOperationState(c.active.state), true, true
	}
	if c.last != nil {
		return cloneOperationState(*c.last), false, true
	}
	return operationState{}, false, false
}

func operationConflictPayload(message string, current operationState) map[string]any {
	return map[string]any{
		"success":           false,
		"error":             message,
		"current_operation": current,
	}
}

func (a *app) handleOperationState(w http.ResponseWriter, _ *http.Request) {
	state, active, ok := vpnOperations.snapshot()
	if !ok {
		writeJSON(w, http.StatusOK, operationStateResponse{Success: true, Active: false})
		return
	}
	writeJSON(w, http.StatusOK, operationStateResponse{Success: true, Active: active, Operation: &state})
}
