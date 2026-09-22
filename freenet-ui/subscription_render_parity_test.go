package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubscriptionApprovedRenderParityContracts(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest("GET", "/accepted-ux.js", nil)
	w := httptest.NewRecorder()
	a.handleIndex(w, req)
	if w.Code != 200 {
		t.Fatalf("accepted UX asset status = %d", w.Code)
	}
	asset := w.Body.String()

	for _, want := range []string{
		"Активна",
		"Последнее обновление",
		"Общий журнал",
		"Открыть журнал подписки",
		"openFreeNetJournal",
		`body:has([data-page-view="subscription"].active) #pageTitle`,
		`body:has([data-page-view="subscription"].active) .footer`,
		"https://••••••••••••••••••••",
		"Вручную",
		"shellSVG('save')",
		"shellSVG('refresh')",
		"История хранится на роутере и одинакова для всех браузеров.",
		"input.type = 'password'",
		"input.autocomplete = 'new-password'",
	} {
		if !strings.Contains(asset, want) {
			t.Fatalf("subscription render contract missing %q", want)
		}
	}

	for _, forbidden := range []string{
		"sessionStorage.getItem(subscriptionHistoryKey)",
		"localStorage",
		"subscriptionHistoryKey",
		"fnSubscriptionHistoryToggle",
		"fnSubscriptionInfoUpdate",
		"MutationObserver",
	} {
		if strings.Contains(asset, forbidden) {
			t.Fatalf("subscription render contract contains forbidden %q", forbidden)
		}
	}
}
