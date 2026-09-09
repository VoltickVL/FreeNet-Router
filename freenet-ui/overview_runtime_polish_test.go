package main

import (
	"os"
	"strings"
	"testing"
)

func TestOverviewRuntimePolishContract(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	mustContain := []string{
		"FreeNetFinalRuntimePolish",
		"fn-best-alternative",
		"Лучший из вариантов",
		"fn-clean-flag",
		"flagSVG",
		"fn-long-value",
		"Сервер вручную",
		"Всё работает",
		"vpn-current-panel .best-v4-pill{grid-template-columns:20px minmax(0,1fr)!important",
		".flag-icon.fn-clean-flag:before,.flag-icon.fn-clean-flag:after{content:none!important",
		"hr: '<rect width=\"30\" height=\"6.667\" fill=\"#ff0000\"",
		"sk: '<rect width=\"30\" height=\"6.667\" fill=\"#fff\"",
		"ru: '<rect width=\"30\" height=\"6.667\" fill=\"#fff\"",
		"dk: '<rect width=\"30\" height=\"20\" fill=\"#c8102e\"",
		"no: '<rect width=\"30\" height=\"20\" fill=\"#ba0c2f\"",
	}
	for _, needle := range mustContain {
		if !strings.Contains(s, needle) {
			t.Fatalf("runtime Overview polish contract missing %q", needle)
		}
	}
}
