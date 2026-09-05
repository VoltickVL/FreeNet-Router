package main

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestDNSPathTraceStartupBudgetIsRouterSafe(t *testing.T) {
	if dnsTraceStartupTimeout < 5*time.Second {
		t.Fatalf("startup window too short for router runtime: %s", dnsTraceStartupTimeout)
	}
	if dnsTraceProcessTimeout < dnsTraceStartupTimeout+3*time.Second {
		t.Fatalf("process timeout %s does not leave enough time after startup %s", dnsTraceProcessTimeout, dnsTraceStartupTimeout)
	}
}

func TestWaitTraceTCPPortDetectsEarlyProcessExit(t *testing.T) {
	done := make(chan error, 1)
	done <- errors.New("xray exited")
	err := waitTraceTCPPort(1, time.Now().Add(time.Second), done)
	if err == nil || err.Error() != "temporary Xray trace process exited before listeners became ready" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWaitTraceTCPPortAcceptsReadyLoopbackListener(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	done := make(chan error, 1)
	if err := waitTraceTCPPort(port, time.Now().Add(time.Second), done); err != nil {
		t.Fatalf("ready listener was not accepted: %v", err)
	}
}
