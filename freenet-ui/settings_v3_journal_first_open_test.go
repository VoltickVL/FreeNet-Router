package main

import (
	"os"
	"strings"
	"testing"
)

func TestSettingsV3JournalRefreshesAfterDataLoad(t *testing.T) {
	data, err := os.ReadFile("web/settings-v3.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	want := `renderJournal(data.events || [], '#fn3JournalFull');`
	if strings.Count(source, want) != 1 {
		t.Fatalf("settings v3 must refresh the full journal after authoritative data load")
	}
}
