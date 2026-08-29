package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

var errDiffStatsNotSupported = errors.New("GitLab не поддерживает diffStatsSummary")

type diffStats struct {
	Additions int
	Deletions int
	Files     int
	Complete  bool
	Source    string
}

type mrDiffStatsProvider interface {
	Stats(context.Context, mergeRequest) (diffStats, error)
}

type volumePolicy interface {
	ChangedLines(additions, deletions int) int
	Median([]int) int
}

type defaultVolumePolicy struct{}

func (defaultVolumePolicy) ChangedLines(additions, deletions int) int { return additions + deletions }
func (defaultVolumePolicy) Median(values []int) int {
	if len(values) == 0 {
		return 0
	}
	items := append([]int(nil), values...)
	sort.Ints(items)
	middle := len(items) / 2
	if len(items)%2 == 1 {
		return items[middle]
	}
	return (items[middle-1] + items[middle]) / 2
}

type graphQLDiffStatsProvider struct{ app *application }

type graphQLResponse struct {
	Data struct {
		Project *struct {
			MergeRequest *struct {
				DiffStatsSummary *struct {
					Additions int `json:"additions"`
					Deletions int `json:"deletions"`
					Changes   int `json:"changes"`
					FileCount int `json:"fileCount"`
				} `json:"diffStatsSummary"`
			} `json:"mergeRequest"`
		} `json:"project"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func projectPathFromMR(item mergeRequest) (string, error) {
	parsed, err := url.Parse(item.WebURL)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("MR %d/%d: GitLab не вернул web_url", item.ProjectID, item.IID)
	}
	path := strings.Trim(parsed.Path, "/")
	project, _, found := strings.Cut(path, "/-/merge_requests/")
	if !found || project == "" {
		return "", fmt.Errorf("MR %d/%d: невозможно определить проект", item.ProjectID, item.IID)
	}
	return project, nil
}

func (provider graphQLDiffStatsProvider) Stats(ctx context.Context, item mergeRequest) (diffStats, error) {
	project, err := projectPathFromMR(item)
	if err != nil {
		return diffStats{}, err
	}
	query := `query PulseReviewDiffStats($project: ID!, $iid: String!) { project(fullPath: $project) { mergeRequest(iid: $iid) { diffStatsSummary { additions deletions changes fileCount } } } }`
	var result graphQLResponse
	if err := provider.app.gitlabGraphQL(ctx, query, map[string]any{"project": project, "iid": fmt.Sprint(item.IID)}, &result); err != nil {
		return diffStats{}, err
	}
	if len(result.Errors) > 0 {
		messages := make([]string, 0, len(result.Errors))
		unsupported := false
		for _, item := range result.Errors {
			messages = append(messages, item.Message)
			if strings.Contains(strings.ToLower(item.Message), "diffstatssummary") {
				unsupported = true
			}
		}
		if unsupported {
			return diffStats{}, errDiffStatsNotSupported
		}
		return diffStats{}, fmt.Errorf("GitLab GraphQL: %s", strings.Join(messages, "; "))
	}
	if result.Data.Project == nil || result.Data.Project.MergeRequest == nil || result.Data.Project.MergeRequest.DiffStatsSummary == nil {
		return diffStats{}, errDiffStatsNotSupported
	}
	stats := result.Data.Project.MergeRequest.DiffStatsSummary
	return diffStats{Additions: stats.Additions, Deletions: stats.Deletions, Files: stats.FileCount, Complete: true, Source: "graphql"}, nil
}

type restDiff struct {
	Diff      string `json:"diff"`
	Collapsed bool   `json:"collapsed"`
	TooLarge  bool   `json:"too_large"`
}

type restDiffStatsProvider struct{ app *application }

func countUnifiedDiff(value string) (additions, deletions int) {
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, "+"):
			additions++
		case strings.HasPrefix(line, "-"):
			deletions++
		}
	}
	return additions, deletions
}

func (provider restDiffStatsProvider) Stats(ctx context.Context, item mergeRequest) (diffStats, error) {
	result := diffStats{Complete: true, Source: "rest"}
	for page := 1; ; page++ {
		var diffs []restDiff
		path := fmt.Sprintf("/projects/%d/merge_requests/%d/diffs?per_page=100&page=%d", item.ProjectID, item.IID, page)
		if err := provider.app.gitlabGet(ctx, path, &diffs); err != nil {
			return diffStats{}, err
		}
		result.Files += len(diffs)
		for _, file := range diffs {
			if file.Collapsed || file.TooLarge {
				result.Complete = false
			}
			added, deleted := countUnifiedDiff(file.Diff)
			result.Additions += added
			result.Deletions += deleted
		}
		if len(diffs) < 100 {
			return result, nil
		}
	}
}

type fallbackDiffStatsProvider struct{ primary, fallback mrDiffStatsProvider }

func (provider fallbackDiffStatsProvider) Stats(ctx context.Context, item mergeRequest) (diffStats, error) {
	stats, err := provider.primary.Stats(ctx, item)
	if err == nil {
		return stats, nil
	}
	if !errors.Is(err, errDiffStatsNotSupported) {
		return diffStats{}, err
	}
	return provider.fallback.Stats(ctx, item)
}

func (a *application) gitlabGraphQL(ctx context.Context, query string, variables map[string]any, target any) error {
	a.connectionMu.RLock()
	gitlabURL, gitlabToken := a.gitlabURL, a.gitlabToken
	a.connectionMu.RUnlock()
	if gitlabURL == "" || gitlabToken == "" {
		return errors.New("GitLab ещё не настроен — откройте раздел «Настройки»")
	}
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, gitlabURL+"/api/graphql", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", gitlabToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, res.Body)
		return fmt.Errorf("GitLab GraphQL: %d", res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(target)
}

type volumePoint struct {
	Period                  string `json:"period"`
	Label                   string `json:"label"`
	MergedMRs               int    `json:"mergedMRs"`
	Additions               int    `json:"additions"`
	Deletions               int    `json:"deletions"`
	ChangedLines            int    `json:"changedLines"`
	ChangedFiles            int    `json:"changedFiles"`
	MedianChangedLinesPerMR int    `json:"medianChangedLinesPerMR"`
	IncompleteMRs           int    `json:"incompleteMRs"`
	sizes                   []int
}

type volumePerson struct {
	Name     string        `json:"name"`
	Username string        `json:"username"`
	Series   []volumePoint `json:"series"`
	Daily    []volumePoint `json:"daily"`
	Total    volumePoint   `json:"total"`
}

type workVolumeTeam struct {
	ID     any            `json:"id"`
	Name   string         `json:"name"`
	Series []volumePoint  `json:"series"`
	Daily  []volumePoint  `json:"daily"`
	People []volumePerson `json:"people"`
}

type workVolumeResponse struct {
	From      string           `json:"from"`
	To        string           `json:"to"`
	Overall   []volumePoint    `json:"overall"`
	Daily     []volumePoint    `json:"daily"`
	Teams     []workVolumeTeam `json:"teams"`
	Collected int              `json:"collectedMRs"`
}

type cachedWorkVolume struct {
	Value     workVolumeResponse
	CreatedAt time.Time
}

type collectedMR struct {
	MR    mergeRequest
	Stats diffStats
}

func volumeBucket(value, from, to time.Time) (key, label string) {
	days := int(to.Sub(from).Hours()/24) + 1
	if days <= 31 {
		return value.Format("2006-01-02"), value.Format("02.01")
	}
	if days <= 93 {
		offset := int(value.Sub(from).Hours()/24) / 7
		start := from.AddDate(0, 0, offset*7)
		end := start.AddDate(0, 0, 6)
		if end.After(to) {
			end = to
		}
		label := start.Format("02") + "–" + end.Format("02.01")
		if start.Month() != end.Month() {
			label = start.Format("02.01") + "–" + end.Format("02.01")
		}
		return start.Format("2006-01-02"), label
	}
	return value.Format("2006-01"), value.Format("01'06")
}

func emptyVolumeSeries(from, to time.Time) []volumePoint {
	days := int(to.Sub(from).Hours()/24) + 1
	result := []volumePoint{}
	for cursor := from; !cursor.After(to); {
		key, label := volumeBucket(cursor, from, to)
		result = append(result, volumePoint{Period: key, Label: label})
		if days <= 31 {
			cursor = cursor.AddDate(0, 0, 1)
		} else if days <= 93 {
			cursor = cursor.AddDate(0, 0, 7)
		} else {
			cursor = time.Date(cursor.Year(), cursor.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		}
	}
	return result
}

// emptyDailyVolumeSeries returns one point for every calendar day in the
// inclusive range. Unlike Series, Daily never changes granularity with the
// length of the requested range. Consumers can therefore regroup it locally
// into weeks, months or quarters without another GitLab request.
func emptyDailyVolumeSeries(from, to time.Time) []volumePoint {
	result := []volumePoint{}
	for cursor := dayStartUTC(from); !cursor.After(dayStartUTC(to)); cursor = cursor.AddDate(0, 0, 1) {
		result = append(result, volumePoint{Period: cursor.Format("2006-01-02"), Label: cursor.Format("02.01")})
	}
	return result
}

func dayStartUTC(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func aggregateDailyVolume(records []collectedMR, from, to time.Time, policy volumePolicy) []volumePoint {
	return aggregateVolumeSeries(emptyDailyVolumeSeries(from, to), records, policy, func(merged time.Time) string {
		return merged.UTC().Format("2006-01-02")
	})
}

func aggregateVolume(records []collectedMR, from, to time.Time, policy volumePolicy) []volumePoint {
	return aggregateVolumeSeries(emptyVolumeSeries(from, to), records, policy, func(merged time.Time) string {
		key, _ := volumeBucket(merged.UTC(), from, to)
		return key
	})
}

func aggregateVolumeSeries(result []volumePoint, records []collectedMR, policy volumePolicy, bucketKey func(time.Time) string) []volumePoint {
	indexes := make(map[string]int, len(result))
	for index := range result {
		indexes[result[index].Period] = index
	}
	for _, record := range records {
		merged, err := time.Parse(time.RFC3339, record.MR.MergedAt)
		if err != nil {
			continue
		}
		index, ok := indexes[bucketKey(merged)]
		if !ok {
			continue
		}
		point := &result[index]
		point.MergedMRs++
		point.Additions += record.Stats.Additions
		point.Deletions += record.Stats.Deletions
		point.ChangedFiles += record.Stats.Files
		point.sizes = append(point.sizes, policy.ChangedLines(record.Stats.Additions, record.Stats.Deletions))
		if !record.Stats.Complete {
			point.IncompleteMRs++
		}
	}
	for index := range result {
		result[index].ChangedLines = policy.ChangedLines(result[index].Additions, result[index].Deletions)
		result[index].MedianChangedLinesPerMR = policy.Median(result[index].sizes)
		result[index].sizes = nil
	}
	return result
}

func totalVolume(series []volumePoint, records []collectedMR, policy volumePolicy) volumePoint {
	result := volumePoint{Label: "Итого"}
	sizes := []int{}
	for _, point := range series {
		result.MergedMRs += point.MergedMRs
		result.Additions += point.Additions
		result.Deletions += point.Deletions
		result.ChangedFiles += point.ChangedFiles
		result.IncompleteMRs += point.IncompleteMRs
	}
	for _, record := range records {
		sizes = append(sizes, policy.ChangedLines(record.Stats.Additions, record.Stats.Deletions))
	}
	result.ChangedLines = policy.ChangedLines(result.Additions, result.Deletions)
	result.MedianChangedLinesPerMR = policy.Median(sizes)
	return result
}

func (a *application) listMergedMRs(ctx context.Context, from time.Time) ([]mergeRequest, error) {
	groups, err := a.configuredGroups()
	if err != nil {
		return nil, err
	}
	projects, err := a.configuredProjects()
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 && len(projects) == 0 {
		return nil, errors.New("Не указаны источники GitLab")
	}
	all := []mergeRequest{}
	for _, group := range groups {
		items, err := a.listMRs(ctx, "/groups/"+url.QueryEscape(group)+"/merge_requests?scope=all&state=merged&include_subgroups=true&updated_after="+url.QueryEscape(from.Format(time.RFC3339)))
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
	}
	for _, project := range projects {
		items, err := a.listMRs(ctx, "/projects/"+url.QueryEscape(project)+"/merge_requests?scope=all&state=merged&updated_after="+url.QueryEscape(from.Format(time.RFC3339)))
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
	}
	seen := map[eventPair]bool{}
	unique := make([]mergeRequest, 0, len(all))
	for _, item := range all {
		key := eventPair{item.ProjectID, item.IID}
		if !seen[key] {
			seen[key] = true
			unique = append(unique, item)
		}
	}
	return unique, nil
}

func collectDiffStats(ctx context.Context, items []mergeRequest, provider mrDiffStatsProvider) ([]collectedMR, error) {
	jobs := make(chan mergeRequest)
	results := make(chan collectedMR)
	errorsChannel := make(chan error, 1)
	workers := 6
	if len(items) < workers {
		workers = len(items)
	}
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for item := range jobs {
				stats, err := provider.Stats(ctx, item)
				if err != nil {
					select {
					case errorsChannel <- err:
					default:
					}
					continue
				}
				results <- collectedMR{MR: item, Stats: stats}
			}
		}()
	}
	go func() {
		for _, item := range items {
			jobs <- item
		}
		close(jobs)
		group.Wait()
		close(results)
	}()
	collected := make([]collectedMR, 0, len(items))
	for item := range results {
		collected = append(collected, item)
	}
	select {
	case err := <-errorsChannel:
		return nil, err
	default:
	}
	return collected, nil
}

func (a *application) buildWorkVolume(ctx context.Context, fromValue, toValue string) (workVolumeResponse, error) {
	from, to, err := parseRange(fromValue, toValue)
	if err != nil {
		return workVolumeResponse{}, err
	}
	key := strings.Join([]string{fromValue, toValue, a.configVersion()}, "|")
	a.volumeCacheMu.Lock()
	if a.volumeCache == nil {
		a.volumeCache = map[string]cachedWorkVolume{}
	}
	cached, found := a.volumeCache[key]
	a.volumeCacheMu.Unlock()
	if found && time.Since(cached.CreatedAt) < reportCacheTTL {
		return cached.Value, nil
	}
	items, err := a.listMergedMRs(ctx, from)
	if err != nil {
		return workVolumeResponse{}, err
	}
	filtered := items[:0]
	for _, item := range items {
		if item.MergedAt != "" && inRange(item.MergedAt, from, to) {
			filtered = append(filtered, item)
		}
	}
	provider := fallbackDiffStatsProvider{primary: graphQLDiffStatsProvider{app: a}, fallback: restDiffStatsProvider{app: a}}
	records, err := collectDiffStats(ctx, filtered, provider)
	if err != nil {
		return workVolumeResponse{}, err
	}
	routes, err := a.readRoutes()
	if err != nil {
		return workVolumeResponse{}, err
	}
	policy := defaultVolumePolicy{}
	allowedUsers := map[string]bool{}
	includeEveryAuthor := false
	for _, route := range routes {
		if len(route.Members) == 0 {
			includeEveryAuthor = true
		}
		for _, username := range route.Members {
			allowedUsers[strings.ToLower(username)] = true
		}
	}
	overallRecords := records
	if !includeEveryAuthor {
		overallRecords = nil
		for _, record := range records {
			if allowedUsers[strings.ToLower(record.MR.Author.Username)] {
				overallRecords = append(overallRecords, record)
			}
		}
	}
	response := workVolumeResponse{
		From:      fromValue,
		To:        toValue,
		Overall:   aggregateVolume(overallRecords, from, to, policy),
		Daily:     aggregateDailyVolume(overallRecords, from, to, policy),
		Collected: len(records),
		Teams:     make([]workVolumeTeam, 0, len(routes)),
	}
	for _, route := range routes {
		members := map[string]bool{}
		for _, username := range route.Members {
			members[strings.ToLower(username)] = true
		}
		teamRecords := []collectedMR{}
		byUsername := map[string][]collectedMR{}
		names := map[string]string{}
		for username := range members {
			names[username] = username
		}
		for _, record := range records {
			username := strings.ToLower(record.MR.Author.Username)
			if len(members) > 0 && !members[username] {
				continue
			}
			teamRecords = append(teamRecords, record)
			byUsername[username] = append(byUsername[username], record)
			if record.MR.Author.Name != "" {
				names[username] = record.MR.Author.Name
			}
		}
		usernames := make([]string, 0, len(names))
		for username := range names {
			usernames = append(usernames, username)
		}
		sort.Strings(usernames)
		team := workVolumeTeam{
			ID:     route.ID,
			Name:   route.Name,
			Series: aggregateVolume(teamRecords, from, to, policy),
			Daily:  aggregateDailyVolume(teamRecords, from, to, policy),
			People: make([]volumePerson, 0, len(usernames)),
		}
		for _, username := range usernames {
			series := aggregateVolume(byUsername[username], from, to, policy)
			team.People = append(team.People, volumePerson{
				Name:     names[username],
				Username: username,
				Series:   series,
				Daily:    aggregateDailyVolume(byUsername[username], from, to, policy),
				Total:    totalVolume(series, byUsername[username], policy),
			})
		}
		response.Teams = append(response.Teams, team)
	}
	a.volumeCacheMu.Lock()
	a.volumeCache[key] = cachedWorkVolume{Value: response, CreatedAt: time.Now()}
	a.volumeCacheMu.Unlock()
	return response, nil
}

func (a *application) handleWorkVolume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonResponse(w, http.StatusMethodNotAllowed, map[string]string{"error": "Метод не поддерживается"})
		return
	}
	result, err := a.buildWorkVolume(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		errorResponse(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, result)
}
