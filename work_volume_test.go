package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCountUnifiedDiffIgnoresHeaders(t *testing.T) {
	diff := "--- a/file.go\n+++ b/file.go\n@@ -1,2 +1,3 @@\n-old\n+new\n+extra\n context\n"
	added, deleted := countUnifiedDiff(diff)
	if added != 2 || deleted != 1 {
		t.Fatalf("unexpected diff stats: +%d -%d", added, deleted)
	}
}

func TestFallbackProviderOnlyFallsBackWhenUnsupported(t *testing.T) {
	primary := stubDiffProvider{err: errDiffStatsNotSupported}
	fallback := stubDiffProvider{stats: diffStats{Additions: 7, Deletions: 2, Files: 1, Complete: true}}
	got, err := (fallbackDiffStatsProvider{primary: primary, fallback: fallback}).Stats(context.Background(), mergeRequest{})
	if err != nil || got.Additions != 7 {
		t.Fatalf("fallback was not used: %+v, %v", got, err)
	}

	temporary := stubDiffProvider{err: context.DeadlineExceeded}
	_, err = (fallbackDiffStatsProvider{primary: temporary, fallback: fallback}).Stats(context.Background(), mergeRequest{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("temporary error must not be hidden: %v", err)
	}
}

type stubDiffProvider struct {
	stats diffStats
	err   error
}

func (provider stubDiffProvider) Stats(context.Context, mergeRequest) (diffStats, error) {
	return provider.stats, provider.err
}

func fixtureWorkVolume(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "test-token" {
			t.Errorf("missing GitLab token")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/merge_requests") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode([]mergeRequest{
				{ProjectID: 10, IID: 7, State: "merged", MergedAt: "2026-08-23T10:00:00Z", UpdatedAt: "2026-08-23T10:00:00Z", WebURL: "https://gitlab.test/acme/app/-/merge_requests/7", Author: user{ID: 1, Name: "Alice", Username: "alice"}},
				{ProjectID: 10, IID: 8, State: "merged", MergedAt: "2026-08-25T10:00:00Z", UpdatedAt: "2026-08-25T10:00:00Z", WebURL: "https://gitlab.test/acme/app/-/merge_requests/8", Author: user{ID: 2, Name: "Bob", Username: "bob"}},
				{ProjectID: 10, IID: 9, State: "merged", MergedAt: "2026-08-10T10:00:00Z", UpdatedAt: "2026-08-10T10:00:00Z", WebURL: "https://gitlab.test/acme/app/-/merge_requests/9", Author: user{ID: 1, Name: "Alice", Username: "alice"}},
			})
		case r.URL.Path == "/api/graphql":
			var payload struct {
				Variables map[string]any `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Error(err)
			}
			iid := payload.Variables["iid"]
			stats := map[string]any{"additions": 10, "deletions": 4, "changes": 14, "fileCount": 2}
			if iid == "8" {
				stats = map[string]any{"additions": 20, "deletions": 5, "changes": 25, "fileCount": 3}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"project": map[string]any{"mergeRequest": map[string]any{"diffStatsSummary": stats}}}})
		default:
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
		}
	})
}

func TestBuildWorkVolumeUsesMergedAtAndConfiguredTeams(t *testing.T) {
	app := newTestApplication(t, fixtureWorkVolume(t))
	writeFixture(t, app.root, "projects.json", []string{"acme/app"})
	writeFixture(t, app.root, "groups.json", []string{})
	writeFixture(t, app.root, "routes.json", []route{
		{ID: "backend", Name: "Backend", Members: []string{"alice"}},
		{ID: "mobile", Name: "Mobile", Members: []string{"bob", "carol"}},
	})

	got, err := app.buildWorkVolume(context.Background(), "2026-08-22", "2026-08-28")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Overall) != 7 || got.Collected != 2 {
		t.Fatalf("unexpected range: periods=%d collected=%d", len(got.Overall), got.Collected)
	}
	if len(got.Teams) != 2 || len(got.Teams[0].People) != 1 || len(got.Teams[1].People) != 2 {
		t.Fatalf("configured teams were not preserved: %+v", got.Teams)
	}
	if got.Teams[0].People[0].Total.Additions != 10 || got.Teams[1].People[0].Total.Additions != 20 {
		t.Fatalf("unexpected person totals: %+v", got.Teams)
	}
	if got.Teams[1].People[1].Username != "carol" || got.Teams[1].People[1].Total.MergedMRs != 0 {
		t.Fatalf("member without MRs must remain visible: %+v", got.Teams[1].People[1])
	}
	if got.Overall[1].MergedMRs != 1 || got.Overall[3].MergedMRs != 1 {
		t.Fatalf("overall series must contain configured employees: %+v", got.Overall)
	}
}

func TestWorkVolumeHTTPAPI(t *testing.T) {
	app := newTestApplication(t, fixtureWorkVolume(t))
	writeFixture(t, app.root, "projects.json", []string{"acme/app"})
	writeFixture(t, app.root, "groups.json", []string{})
	writeFixture(t, app.root, "routes.json", []route{{ID: "backend", Name: "Backend", Members: []string{"alice"}}})
	request := httptest.NewRequest(http.MethodGet, "/api/work-volume?from=2026-08-22&to=2026-08-28", nil)
	response := httptest.NewRecorder()
	app.handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"changedFiles":2`) {
		t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
}
