package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	if len(got.Daily) != 7 || len(got.Teams[0].Daily) != 7 || len(got.Teams[0].People[0].Daily) != 7 {
		t.Fatalf("daily contract must be present at every aggregation level: %+v", got)
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
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"changedFiles":2`) || !strings.Contains(response.Body.String(), `"daily":[`) {
		t.Fatalf("unexpected response %d: %s", response.Code, response.Body.String())
	}
}

func TestWorkVolumeUsesWeeklyBucketsForTwoMonths(t *testing.T) {
	from := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.August, 28, 0, 0, 0, 0, time.UTC)
	series := emptyVolumeSeries(from, to)
	if len(series) != 9 {
		t.Fatalf("expected 9 weekly buckets, got %d: %#v", len(series), series)
	}
	if series[0].Period != "2026-07-01" || series[0].Label != "01–07.07" {
		t.Fatalf("unexpected first bucket: %#v", series[0])
	}
	if series[8].Period != "2026-08-26" || series[8].Label != "26–28.08" {
		t.Fatalf("unexpected final bucket: %#v", series[8])
	}
}

func TestEmptyDailyVolumeSeriesIsInclusiveAcrossCalendarBoundaries(t *testing.T) {
	tests := []struct {
		name        string
		from        time.Time
		to          time.Time
		wantPeriods []string
	}{
		{
			name:        "single day",
			from:        time.Date(2026, time.August, 28, 0, 0, 0, 0, time.UTC),
			to:          time.Date(2026, time.August, 28, 23, 59, 59, 0, time.UTC),
			wantPeriods: []string{"2026-08-28"},
		},
		{
			name:        "month boundary",
			from:        time.Date(2026, time.August, 30, 0, 0, 0, 0, time.UTC),
			to:          time.Date(2026, time.September, 2, 23, 59, 59, 0, time.UTC),
			wantPeriods: []string{"2026-08-30", "2026-08-31", "2026-09-01", "2026-09-02"},
		},
		{
			name:        "year boundary",
			from:        time.Date(2026, time.December, 30, 0, 0, 0, 0, time.UTC),
			to:          time.Date(2027, time.January, 2, 23, 59, 59, 0, time.UTC),
			wantPeriods: []string{"2026-12-30", "2026-12-31", "2027-01-01", "2027-01-02"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := emptyDailyVolumeSeries(tt.from, tt.to)
			if len(got) != len(tt.wantPeriods) {
				t.Fatalf("got %d daily points, want %d: %#v", len(got), len(tt.wantPeriods), got)
			}
			for index, want := range tt.wantPeriods {
				if got[index].Period != want {
					t.Fatalf("point %d period = %q, want %q", index, got[index].Period, want)
				}
			}
		})
	}
}

func TestAggregateDailyVolumePreservesZeroDaysAndExactMetrics(t *testing.T) {
	from := time.Date(2026, time.December, 30, 0, 0, 0, 0, time.UTC)
	to := time.Date(2027, time.January, 2, 23, 59, 59, 0, time.UTC)
	records := []collectedMR{
		{
			MR:    mergeRequest{MergedAt: "2026-12-30T10:00:00Z"},
			Stats: diffStats{Additions: 10, Deletions: 4, Files: 2, Complete: true},
		},
		{
			// A non-UTC offset is normalized to the UTC day used by from/to.
			MR:    mergeRequest{MergedAt: "2027-01-02T02:30:00+03:00"},
			Stats: diffStats{Additions: 20, Deletions: 5, Files: 3, Complete: false},
		},
	}

	got := aggregateDailyVolume(records, from, to, defaultVolumePolicy{})
	if len(got) != 4 {
		t.Fatalf("got %d points, want 4: %#v", len(got), got)
	}
	if got[0].MergedMRs != 1 || got[0].Additions != 10 || got[0].Deletions != 4 || got[0].ChangedLines != 14 || got[0].ChangedFiles != 2 || got[0].MedianChangedLinesPerMR != 14 {
		t.Fatalf("unexpected first day: %#v", got[0])
	}
	if got[1].MergedMRs != 0 || got[1].Additions != 0 || got[1].ChangedLines != 0 || got[1].ChangedFiles != 0 {
		t.Fatalf("zero day must be explicit: %#v", got[1])
	}
	if got[2].Period != "2027-01-01" || got[2].MergedMRs != 1 || got[2].IncompleteMRs != 1 || got[2].ChangedLines != 25 {
		t.Fatalf("offset timestamp must land on its UTC date: %#v", got[2])
	}
	if got[3].MergedMRs != 0 {
		t.Fatalf("inclusive final day must be present even when empty: %#v", got[3])
	}
}

func TestDailyContractKeepsLegacyAdaptiveSeries(t *testing.T) {
	from := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.August, 28, 23, 59, 59, 0, time.UTC)
	records := []collectedMR{{
		MR:    mergeRequest{MergedAt: "2026-08-28T23:59:59Z"},
		Stats: diffStats{Additions: 3, Deletions: 2, Files: 1, Complete: true},
	}}

	legacy := aggregateVolume(records, from, to, defaultVolumePolicy{})
	daily := aggregateDailyVolume(records, from, to, defaultVolumePolicy{})
	if len(legacy) != 9 {
		t.Fatalf("legacy series changed: got %d buckets", len(legacy))
	}
	if len(daily) != 59 {
		t.Fatalf("daily series must have one point per inclusive day, got %d", len(daily))
	}
	if daily[58].Period != "2026-08-28" || daily[58].MergedMRs != 1 || daily[58].ChangedLines != 5 {
		t.Fatalf("inclusive final-day metrics are wrong: %#v", daily[58])
	}
}
