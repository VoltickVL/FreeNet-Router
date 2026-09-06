package main

import (
	"reflect"
	"testing"
)

func TestKeeneticInterfaceQualifiedResolverSelectionIsSupported(t *testing.T) {
	config := "ip name-server 77.88.8.8 \"\" on GigabitEthernet0/Vlan5\n" +
		"ip name-server 77.88.8.1 \"\" on GigabitEthernet0/Vlan5\n"

	got, err := networkBridgeNativeResolverSelectionLines(config, "192.168.50.1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ip name-server 77.88.8.8 \"\" on GigabitEthernet0/Vlan5",
		"ip name-server 77.88.8.1 \"\" on GigabitEthernet0/Vlan5",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selection=%v want=%v", got, want)
	}
}

func TestInterfaceQualifiedYandexSelectionMatchesCanonicalTarget(t *testing.T) {
	qualified := []string{
		"ip name-server 77.88.8.8 \"\" on GigabitEthernet0/Vlan5",
		"ip name-server 77.88.8.1 \"\" on GigabitEthernet0/Vlan5",
	}
	canonical := networkBridgeYandexBasicResolverLines()
	if !networkBridgeResolverSelectionsEqual(qualified, canonical) {
		t.Fatalf("qualified=%v canonical=%v must describe the same active resolver set", qualified, canonical)
	}
}

func TestInterfaceQualifiedResolverPresenceIsSemantic(t *testing.T) {
	config := "ip name-server 77.88.8.8 \"\" on GigabitEthernet0/Vlan5\n"
	if !networkBridgeResolverSelectionPresent(config, "ip name-server 77.88.8.8") {
		t.Fatal("Keenetic-qualified running-config line must satisfy compact canonical selection")
	}
}

func TestUnknownResolverQualifierStillFailsClosed(t *testing.T) {
	bad := []string{
		"ip name-server 77.88.8.8 on GigabitEthernet0/Vlan5",
		"ip name-server 77.88.8.8 \"custom\" on GigabitEthernet0/Vlan5",
		"ip name-server 77.88.8.8 \"\" via GigabitEthernet0/Vlan5",
		"ip name-server 77.88.8.8 \"\" on GigabitEthernet0/Vlan5;reboot",
	}
	for _, line := range bad {
		if networkBridgeResolverSelectionLineSupported(line) {
			t.Fatalf("unexpectedly accepted resolver syntax: %q", line)
		}
	}
}
