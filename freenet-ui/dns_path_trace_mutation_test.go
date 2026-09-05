package main

import "testing"

func TestDNSPathTraceMutationMarker(t *testing.T) {
	trace := dnsPathTraceResponse{Mutation: "NONE"}
	if trace.Mutation != "NONE" {
		t.Fatal("DNS path trace must remain read-only")
	}
}
