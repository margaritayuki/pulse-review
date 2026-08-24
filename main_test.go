package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestApplication(t *testing.T, handler http.Handler) *application {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &application{
		root: root, gitlabURL: "http://gitlab.test", gitlabToken: "test-token", httpClient: &http.Client{Transport: handlerTransport{handler: handler}},
		cache: map[string]cachedReport{}, progress: map[string]progress{},
	}
}

type handlerTransport struct{ handler http.Handler }

func (transport handlerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	transport.handler.ServeHTTP(recorder, request)
	return recorder.Result(), nil
}

func writeFixture(t *testing.T, root, name string, value any) {
	t.Helper()
	if err := writeJSONAtomic(filepath.Join(root, "data", name), value); err != nil {
		t.Fatal(err)
	}
}

func fixtureGitLab(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "test-token" {
			t.Errorf("missing GitLab token")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v4/users":
			username := r.URL.Query().Get("username")
			users := map[string]user{
				"alice": {ID: 1, Name: "Alice", Username: "alice"},
				"bob":   {ID: 2, Name: "Bob", Username: "bob"},
				"carol": {ID: 3, Name: "Carol", Username: "carol"},
			}
			if item, ok := users[username]; ok {
				_ = json.NewEncoder(w).Encode([]user{item})
			} else {
				_ = json.NewEncoder(w).Encode([]user{})
			}
		case "/api/v4/groups/acme/merge_requests":
			_ = json.NewEncoder(w).Encode([]mergeRequest{
				{ProjectID: 10, IID: 7, CreatedAt: "2026-08-10T00:00:00Z", State: "opened", Author: user{ID: 1, Name: "Alice", Username: "alice"}},
				{ProjectID: 10, IID: 8, CreatedAt: "2026-07-01T00:00:00Z", State: "merged", Author: user{ID: 2, Name: "Bob", Username: "bob"}},
			})
		case "/api/v4/projects/10/merge_requests/7/notes":
			_ = json.NewEncoder(w).Encode([]note{
				{System: true, Body: "approved this merge request", CreatedAt: "2026-08-11T10:00:00Z", Author: user{ID: 2, Name: "Bob", Username: "bob"}},
				{System: true, Body: "approved this merge request", CreatedAt: "2026-08-11T11:00:00Z", Author: user{ID: 2, Name: "Bob", Username: "bob"}},
				{System: false, Body: "review", CreatedAt: "2026-08-12T10:00:00Z", Author: user{ID: 2, Name: "Bob", Username: "bob"}},
				{System: false, Body: "self", CreatedAt: "2026-08-12T10:00:00Z", Author: user{ID: 1, Name: "Alice", Username: "alice"}},
			})
		case "/api/v4/projects/10/merge_requests/7/award_emoji":
			bob := user{ID: 2, Name: "Bob", Username: "bob"}
			alice := user{ID: 1, Name: "Alice", Username: "alice"}
			_ = json.NewEncoder(w).Encode([]award{
				{Name: "thumbsup", CreatedAt: "2026-08-13T10:00:00Z", User: &bob},
				{Name: "thumbsup", CreatedAt: "2026-08-13T10:00:00Z", User: &alice},
			})
		case "/api/v4/projects/10/merge_requests/8/notes":
			_ = json.NewEncoder(w).Encode([]note{{System: false, Body: "boundary", CreatedAt: "2026-08-31T23:59:59Z", Author: user{ID: 3, Name: "Carol", Username: "carol"}}})
		case "/api/v4/projects/10/merge_requests/8/award_emoji":
			_ = json.NewEncoder(w).Encode([]award{})
		default:
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
		}
	})
}

func TestBuildReportRules(t *testing.T) {
	app := newTestApplication(t, fixtureGitLab(t))
	writeFixture(t, app.root, "groups.json", []string{"acme"})
	writeFixture(t, app.root, "projects.json", []string{})
	writeFixture(t, app.root, "routes.json", []route{
		{ID: "approval-team", Name: "Approval", Members: []string{"alice", "bob", "carol"}, ReviewSignal: "approval"},
		{ID: "either-team", Name: "Either", Members: []string{"bob"}, ReviewSignal: "either"},
	})

	got, err := app.buildReport(context.Background(), "2026-08-01", "2026-08-31", "all", "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Totals.Approvals != 1 || got.Totals.Comments != 2 || got.Totals.Members != 3 {
		t.Fatalf("unexpected totals: %+v", got.Totals)
	}
	byUser := map[string]reviewer{}
	for _, item := range got.Reviewers {
		byUser[item.Username] = item
	}
	if byUser["alice"].CreatedMergeRequests != 1 || byUser["alice"].OpenedMergeRequests != 1 || byUser["alice"].ReceivedComments != 1 {
		t.Fatalf("unexpected Alice metrics: %+v", byUser["alice"])
	}
	if byUser["bob"].Approvals != 1 || byUser["bob"].Comments != 1 {
		t.Fatalf("unexpected Bob metrics: %+v", byUser["bob"])
	}
	if byUser["carol"].Comments != 1 || byUser["carol"].CreatedMergeRequests != 0 {
		t.Fatalf("unexpected Carol metrics: %+v", byUser["carol"])
	}
	if len(got.Teams) != 2 || got.Teams[1].Totals.Approvals != 1 {
		t.Fatalf("unexpected team report: %+v", got.Teams)
	}

	state := app.progress["job-1"]
	if state.Phase != "complete" || state.Total == nil || *state.Total != 2 || state.Processed != 2 {
		t.Fatalf("unexpected progress: %+v", state)
	}
	_, err = app.buildReport(context.Background(), "2026-08-01", "2026-08-31", "all", "job-2")
	if err != nil || !app.progress["job-2"].Cached {
		t.Fatalf("cache was not reported: %+v, %v", app.progress["job-2"], err)
	}
}

func TestEitherDeduplicatesApprovalAndLike(t *testing.T) {
	app := newTestApplication(t, fixtureGitLab(t))
	writeFixture(t, app.root, "groups.json", []string{"acme"})
	writeFixture(t, app.root, "projects.json", []string{})
	writeFixture(t, app.root, "routes.json", []route{{ID: "team", Name: "Team", Members: []string{"bob"}, ReviewSignal: "either"}})
	got, err := app.buildReport(context.Background(), "2026-08-01", "2026-08-31", "team", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Reviewers) != 1 || got.Reviewers[0].Approvals != 1 {
		t.Fatalf("approval and like must be one event: %+v", got.Reviewers)
	}
	if got.Teams != nil {
		t.Fatalf("specific-team report must serialize teams as null")
	}
}

func TestNormalizeSource(t *testing.T) {
	cases := map[string]string{
		" https://gitlab.example.com/group/project.git/ ": "group/project",
		"/group/project.git":                              "group/project",
		"group/project/":                                  "group/project",
	}
	for input, want := range cases {
		if got := normalizeSource(input); got != want {
			t.Errorf("normalizeSource(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestInRangeIsInclusiveAndUTC(t *testing.T) {
	from, to, err := parseRange("2026-08-01", "2026-08-31")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"2026-08-01T00:00:00Z", "2026-08-31T23:59:59Z", "2026-08-01T03:00:00+03:00"} {
		if !inRange(value, from, to) {
			t.Errorf("expected %s in range", value)
		}
	}
	if inRange("2026-09-01T00:00:00Z", from, to) {
		t.Error("end must be inclusive only through 23:59:59Z")
	}
}

func TestLoadEnvFilePrecedence(t *testing.T) {
	t.Setenv("PULSE_TEST_VALUE", "process")
	path := filepath.Join(t.TempDir(), ".env.local")
	if err := os.WriteFile(path, []byte("PULSE_TEST_VALUE=file\r\nEMPTY=\r\n# comment\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("PULSE_TEST_VALUE"); got != "file" {
		t.Fatalf("env file must override process env, got %q", got)
	}
}

func TestDirectHTTPClientIgnoresProxyEnvironment(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://proxy.invalid:3128")
	transport, ok := directHTTPClient().Transport.(*http.Transport)
	if !ok {
		t.Fatal("direct client must use an explicit HTTP transport")
	}
	if transport.Proxy != nil {
		t.Fatal("direct client must ignore proxy environment variables")
	}
}

func TestCacheExpiryShape(t *testing.T) {
	app := newTestApplication(t, fixtureGitLab(t))
	app.cache["expired"] = cachedReport{CreatedAt: time.Now().Add(-reportCacheTTL - time.Second)}
	if time.Since(app.cache["expired"].CreatedAt) <= reportCacheTTL {
		t.Fatal("test setup did not create expired entry")
	}
}

func TestHTTPCompatibilityShapes(t *testing.T) {
	t.Setenv("MATTERMOST_WEBHOOK_URL", "")
	app := newTestApplication(t, fixtureGitLab(t))

	progressRequest := httptest.NewRequest(http.MethodGet, "/api/progress?id=unknown", nil)
	progressResponse := httptest.NewRecorder()
	app.handler().ServeHTTP(progressResponse, progressRequest)
	if got := progressResponse.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content type %q", got)
	}
	var state map[string]any
	if err := json.Unmarshal(progressResponse.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if state["phase"] != "searching" || state["processed"] != float64(0) || state["total"] != nil {
		t.Fatalf("unexpected default progress: %#v", state)
	}

	configRequest := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	configResponse := httptest.NewRecorder()
	app.handler().ServeHTTP(configResponse, configRequest)
	var config map[string]any
	if err := json.Unmarshal(configResponse.Body.Bytes(), &config); err != nil {
		t.Fatal(err)
	}
	if config["mattermostConfigured"] != false {
		t.Fatalf("unexpected config: %#v", config)
	}
	if projects, ok := config["projects"].([]any); !ok || len(projects) != 0 {
		t.Fatalf("projects must be an empty array: %#v", config["projects"])
	}

	staticRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	staticResponse := httptest.NewRecorder()
	app.handler().ServeHTTP(staticResponse, staticRequest)
	if staticResponse.Code != http.StatusOK || !strings.Contains(staticResponse.Body.String(), "Pulse Review") {
		t.Fatalf("static frontend was not served: %d", staticResponse.Code)
	}
}
