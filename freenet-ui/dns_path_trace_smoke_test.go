package main

import "testing"

func TestDNSPathTraceResponseSmoke(t *testing.T) {
	trace := dnsPathTraceResponse{Success: true, Host: "ipleak.net", DNSMode: "xkeen", Mutation: "NONE"}
	if !trace.Success || trace.Host != "ipleak.net" || trace.DNSMode != "xkeen" || trace.Mutation != "NONE" {
		t.Fatalf("unexpected trace response: %+v", trace)
	}
}
