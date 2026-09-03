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
		`id="setupFinalizePlanDetails"`,
		`id="setupFinalizeNotice"`,
		`id="finalizeSection"`,
		`id="setupCompleteBanner"`,
		`id="installScenario"`,
		`id="setupState"`,
		`id="installScenarioHint"`,
		"Тип установки",
		"Состояние настройки",
		"Действующий роутер",
		"Новая установка",
		"Существующие XKeen/Xray распознаны и сохранены",
		"Проверить готовность",
		"Завершить настройку",
		"renderInstallScenario",
		"renderSetupVisibility",
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

func TestCompletedSetupShowsFactsAndLeavesWizardMode(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	for _, required := range []string{
		"finalize.hidden=complete",
		"banner.hidden=!complete",
		"Настройка завершена. Это обычный режим Control Center; повторно проходить мастер не требуется.",
		"if(lastStatus&&lastStatus.setup_complete){renderInstallScenario(lastStatus);return}",
		"updateNetworkHint(s);renderInstallScenario(s)",
		"state.textContent=p&&p.setup_complete?'● Настройка завершена':'● Настройка не завершена'",
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("completed setup state missing %q", required)
		}
	}
	for _, stale := range []string{`<div id="installScenario" class="value">Определяем…</div>`, `<div id="setupState" class="value">Проверяем…</div>`} {
		if strings.Contains(ui, stale) {
			t.Fatalf("completed setup UI still ships stale placeholder %q", stale)
		}
	}
}

func TestInstallScenarioIsInformationalOnly(t *testing.T) {
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(data)
	if strings.Contains(ui, `name="install_scenario"`) || strings.Contains(ui, `id="installScenarioSelect"`) {
		t.Fatal("сценарий установки не должен выбираться пользователем")
	}
	if !strings.Contains(ui, "p.install_scenario==='existing_stack'") || !strings.Contains(ui, "p.install_scenario==='fresh_entware'") {
		t.Fatal("UI должен отображать только автоматически определённые сценарии")
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
