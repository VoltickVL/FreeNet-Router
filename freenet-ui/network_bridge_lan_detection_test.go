package main

import "testing"

func TestNetworkBridgeLANIPv4FromIPOutputDoesNotHardcodeBr0(t *testing.T) {
	output := "2: eth0    inet 203.0.113.8/24 brd 203.0.113.255 scope global eth0\n" +
		"7: br-lan    inet 192.168.88.1/24 brd 192.168.88.255 scope global br-lan\n"
	got, err := networkBridgeLANIPv4FromIPOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if got != "192.168.88.1" {
		t.Fatalf("LAN IPv4=%q, want 192.168.88.1", got)
	}
}

func TestNetworkBridgeLANIPv4FromIPOutputPrefersBridgePrivateAddress(t *testing.T) {
	output := "3: wan0    inet 10.10.10.2/30 brd 10.10.10.3 scope global wan0\n" +
		"9: br2    inet 192.168.50.1/24 brd 192.168.50.255 scope global br2\n"
	got, err := networkBridgeLANIPv4FromIPOutput(output)
	if err != nil {
		t.Fatal(err)
	}
	if got != "192.168.50.1" {
		t.Fatalf("LAN IPv4=%q, want bridge address 192.168.50.1", got)
	}
}

func TestNetworkBridgeLANIPv4FromIPOutputRejectsPublicOnly(t *testing.T) {
	output := "2: eth0    inet 203.0.113.8/24 brd 203.0.113.255 scope global eth0\n"
	if got, err := networkBridgeLANIPv4FromIPOutput(output); err == nil {
		t.Fatalf("unexpected LAN IPv4=%q from public-only addresses", got)
	}
}
