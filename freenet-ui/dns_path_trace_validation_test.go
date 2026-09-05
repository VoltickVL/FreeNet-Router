package main

import (
	"reflect"
	"testing"
)

func TestDNSPathTraceUsesExactManagedSplitMirror(t *testing.T) {
	routing := traceTestRouting()
	dns := traceTestDNS()
	expected, err := expectedFreeNetManagedSplitDNS(routing)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dns, expected) {
		t.Fatal("trace fixture must be exact FreeNet-managed Split DNS")
	}
}
