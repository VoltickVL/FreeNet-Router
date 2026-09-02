package main

import (
	"strings"
	"testing"
)

func TestSetupFinalizeUIContract(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, required := range []string{
		`id="planFinalizeBtn"`,
		`id="applyFinalizeBtn"`,
		`id="setupFinalizePlan"`,
		`id="setupFinalizeNotice"`,
		"Проверить готовность",
		"Завершить настройку",
		"loadSetupFinalizePlan",
		"applySetupFinalize",
		"setup_finalize=1",
		"operation:'finalize'",
		"setup_finalize_plan",
		"p.ready&&p.mutation==='NONE'&&!p.setup_complete",
		"ОСНОВНАЯ ОШИБКА:",
		"ОТКАТ:",
		"Не повторяйте вслепую",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("в UI финального мастера отсутствует контракт %q", required)
		}
	}
}

func TestSetupFinalizeApplyIsLocallyGatedByFreshPlan(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	if !strings.Contains(ui, "busy||!setupFinalizePlanReady||setupFinalizeComplete") {
		t.Fatal("кнопка завершения должна быть заблокирована без свежего готового плана")
	}
	if !strings.Contains(ui, "resetSetupFinalizePlan()") {
		t.Fatal("изменения provider/ISP/DNS должны инвалидировать старый финальный план")
	}
}

func TestSetupFinalizeUIUsesOnlyStructuredAPI(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	start := strings.Index(ui, "async function loadSetupFinalizePlan")
	end := strings.Index(ui[start:], "async function act(action)")
	if start < 0 || end < 0 {
		t.Fatal("функции финального мастера не найдены")
	}
	body := ui[start : start+end]
	if strings.Contains(body, "command:") || strings.Contains(body, "shell") || strings.Contains(body, "exec") {
		t.Fatal("финальный UI не должен содержать поверхность произвольных команд")
	}
	if !strings.Contains(body, "/api/network-profile/plan?setup_finalize=1") || !strings.Contains(body, "/api/network-profile/apply") {
		t.Fatal("финальный UI должен использовать только allowlisted plan/apply API")
	}
}
