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
		`<option value="two_weeks">Последние 2 недели</option>`,
		`['week','two_weeks','current_month','previous_month']`,
		`id="rd-overall-chart"`,
		`id="rd-team-analytics"`,
		`aria-pressed="${active}">${escapeHtml(item.name)}</button>`,
		`name:'Изменено файлов'`,
		`const independentScales = lines.length > 2`,
		`sumSeries(team.people,team.name)`,
		`/api/work-volume?from=`,
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
