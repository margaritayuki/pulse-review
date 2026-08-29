package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestFrontendRegressionContracts(t *testing.T) {
	contents, err := os.ReadFile("public/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(contents)

	checks := []string{
		`id="live-team"`,
		`location.protocol !== 'file:'`,
		`id:'backend',name:'Backend-команда'`,
		`id:'mobile',name:'Mobile-команда'`,
		`const previewTeams = (config.routes || []).map`,
		`src="./pulse-review-mark.svg"`,
		`volumeRange.value==='week'?7:14`,
		`.rd-dashboard-card-bars { min-height:200px;`,
		`rd-dashboard-card-title rd-widget-title`,
		`renderSelectedTeam();`,
		`.rd-controls[hidden] { display:none !important; }`,
		`.rd-info-wrap:hover .rd-info-popover`,
		`.rd-info-wrap:focus-within .rd-info-popover`,
		`current.key===header.dataset.sortKey&&current.direction==='desc'?'asc':'desc'`,
		`rd-team-table-scroll`,
		`<option value="week" selected>Последняя неделя</option>`,
		`transform="rotate(-45`,
		`rd-line-axis-weekend`,
		`class="rd-person-filter"`,
		`id="rd-update-progress"`,
		`rd-custom-period`,
		`id="rd-update-cancel"`,
		`activeReportController?.abort()`,
		`<option value="two_weeks">Последние 2 недели</option>`,
		`['week','two_weeks','current_month','previous_month']`,
		`id="rd-overall-chart"`,
		`id="rd-team-analytics"`,
		`aria-pressed="${active}">${escapeHtml(item.name)}</button>`,
		`name:'Изменено файлов'`,
		`maximum/minimum>=10`,
		`buildChartScale(visibleLines)`,
		`rd-metric-toggle`,
		`rd-chart-expand`,
		`width:min(1902px,calc(100vw - 48px))`,
		`Math.round(container.clientWidth || 640)`,
		`Math.max(minimumY,Math.min(maximumY`,
		`data-zero-y="${top+plotHeight}"`,
		`background-position:right 12px center`,
		`aria-label="Как работает персональный сигнал"`,
		`input.addEventListener('blur',saveAnalyticsRule)`,
		`sumSeries(team.people,team.name)`,
		`/api/work-volume?from=`,
		`data-view="overview"`,
		`data-view="dashboard" aria-selected="false">Ревью</button>`,
		`id="rd-dashboard-overall-chart"`,
		`id="rd-dashboard-periods"`,
		`id="rd-dashboard-cards"`,
		`<strong>Расшифровка</strong>`,
		`data-dashboard-view="line"`,
		`data-dashboard-view="bar"`,
		`dashboardAvailableGroups()`,
		`removeZeroDashboardPeriods`,
		`visibleDashboardMetrics`,
		`rd-dashboard-card-total { color:inherit; font:inherit; }`,
		`const fixed=[0,1,3,5,7,10]`,
		`class="rd-dashboard-period-total"`,
		`class="rd-dashboard-period-metric"`,
		`const savedTokenMask = '••••••••••••'`,
	}
	for _, fragment := range checks {
		if !strings.Contains(page, fragment) {
			t.Errorf("expected frontend to contain %q", fragment)
		}
	}
	if count := strings.Count(page, `id="live-team"`); count != 1 {
		t.Errorf("team filter must be shared and unique, got %d copies", count)
	}
	if strings.Contains(page, `id="rd-volume-person"`) {
		t.Error("employee filter must not return to the dynamics view")
	}
	if strings.Contains(page, `id="rd-volume-filter"`) {
		t.Error("employee filter chip must not return to the dynamics view")
	}
	if strings.Contains(page, `Ревью команды`) {
		t.Error("review heading must stay concise")
	}
	if strings.Contains(page, `id="rd-save-analytics"`) {
		t.Error("analytics settings must save without a separate button")
	}
	if strings.Contains(page, `class="rd-info-popover" role="tooltip" hidden`) {
		t.Error("help popovers must not require click-controlled hidden state")
	}
	if count := strings.Count(page, `['backend'`); count != 3 {
		t.Errorf("Backend mock must contain 3 employees, got %d", count)
	}
	if count := strings.Count(page, `['mobile'`); count != 15 {
		t.Errorf("Mobile mock must contain 15 employees, got %d", count)
	}
	if strings.Contains(page, `id="rd-mr-bars"`) || strings.Contains(page, `id="rd-lines-bars"`) {
		t.Error("legacy split dynamics charts must be removed")
	}
	if strings.Contains(page, `Реальные значения на единой нелинейной шкале`) || strings.Contains(page, `Количество, реальная нелинейная шкала`) {
		t.Error("unrequested chart captions must stay absent")
	}
	if strings.Contains(page, `text-decoration:line-through`) {
		t.Error("hidden metric labels must not be struck through")
	}
	if strings.Contains(page, `preserveAspectRatio="none"`) {
		t.Error("chart text must not stretch with the SVG")
	}
	if count := strings.Count(page, `<h1>Ревью</h1>`); count != 1 {
		t.Errorf("review page must remain present exactly once, got %d", count)
	}
}

func TestConfigReturnsTeamsUsedByFrontendFilters(t *testing.T) {
	app := newTestApplication(t, fixtureGitLab(t))
	want := []route{
		{ID: "backend", Name: "Backend", Members: []string{"alice"}, ReviewSignal: "approval"},
		{ID: "mobile", Name: "Mobile", Members: []string{"bob"}, ReviewSignal: "like"},
	}
	writeFixture(t, app.root, "routes.json", want)

	request := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	response := httptest.NewRecorder()
	app.handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
	var config struct {
		Routes []route `json:"routes"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Routes) != 2 || config.Routes[0].Name != "Backend" || config.Routes[1].Name != "Mobile" {
		t.Fatalf("configured teams were not returned to the frontend: %#v", config.Routes)
	}
}

func TestConfigSavesRenamedTeam(t *testing.T) {
	app := newTestApplication(t, fixtureGitLab(t))
	writeFixture(t, app.root, "groups.json", []string{"acme"})
	writeFixture(t, app.root, "projects.json", []string{"acme/project"})
	writeFixture(t, app.root, "routes.json", []route{{ID: "backend", Name: "Backend", Members: []string{"alice"}, ReviewSignal: "approval"}})

	body := `{"routes":[{"id":"backend","name":"Backend Platform","members":["alice"],"reviewSignal":"approval"}],"groups":["acme"],"projects":["acme/project"]}`
	request := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	app.handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("rename failed with %d: %s", response.Code, response.Body.String())
	}

	routes, err := app.readRoutes()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Name != "Backend Platform" {
		t.Fatalf("renamed team was not persisted: %#v", routes)
	}
}
