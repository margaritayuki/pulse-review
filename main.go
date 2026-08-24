package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const reportCacheTTL = 15 * time.Minute

//go:embed public/*
var publicFiles embed.FS

type route struct {
	ID           any      `json:"id"`
	Name         string   `json:"name"`
	Channel      string   `json:"channel"`
	Members      []string `json:"members"`
	Frequency    string   `json:"frequency"`
	Time         string   `json:"time"`
	Enabled      bool     `json:"enabled"`
	ReviewSignal string   `json:"reviewSignal"`
	Groups       []string `json:"groups,omitempty"`
	Projects     []string `json:"projects,omitempty"`
}

type user struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

type mergeRequest struct {
	ProjectID int    `json:"project_id"`
	IID       int    `json:"iid"`
	CreatedAt string `json:"created_at"`
	State     string `json:"state"`
	Author    user   `json:"author"`
}

type note struct {
	System    bool   `json:"system"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	Author    user   `json:"author"`
}

type award struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	User      *user  `json:"user"`
}

type reviewer struct {
	ID                   int    `json:"id"`
	Name                 string `json:"name"`
	Username             string `json:"username"`
	Approvals            int    `json:"approvals"`
	Comments             int    `json:"comments"`
	ReceivedComments     int    `json:"receivedComments"`
	CreatedMergeRequests int    `json:"createdMergeRequests"`
	OpenedMergeRequests  int    `json:"openedMergeRequests"`
	ProfileURL           string `json:"profileUrl"`
	OpenMergeRequestsURL string `json:"openMergeRequestsUrl"`
}

type totals struct {
	Approvals int `json:"approvals"`
	Comments  int `json:"comments"`
	Active    int `json:"active"`
	Members   int `json:"members"`
}

type teamReport struct {
	ID           any        `json:"id"`
	Name         string     `json:"name"`
	Reviewers    []reviewer `json:"reviewers"`
	ReviewSignal string     `json:"reviewSignal"`
	Totals       totals     `json:"totals"`
}

type report struct {
	Project   string       `json:"project"`
	Projects  []string     `json:"projects"`
	From      string       `json:"from"`
	To        string       `json:"to"`
	Reviewers []reviewer   `json:"reviewers"`
	Teams     []teamReport `json:"teams"`
	Totals    totals       `json:"totals"`
}

type progress struct {
	Phase     string `json:"phase"`
	Processed int    `json:"processed"`
	Total     *int   `json:"total"`
	Cached    bool   `json:"cached,omitempty"`
	Error     string `json:"error,omitempty"`
}

type cachedReport struct {
	Report    report
	CreatedAt time.Time
}

type application struct {
	root           string
	gitlabURL      string
	gitlabToken    string
	port           int
	legacyProjects []string
	httpClient     *http.Client

	cacheMu    sync.Mutex
	cache      map[string]cachedReport
	progressMu sync.Mutex
	progress   map[string]progress
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	if err := loadEnvFile(filepath.Join(root, ".env.local")); err != nil {
		log.Fatal(err)
	}
	gitlabURL, ok := os.LookupEnv("GITLAB_URL")
	if !ok {
		log.Fatal("GITLAB_URL is not set")
	}
	gitlabToken, ok := os.LookupEnv("GITLAB_TOKEN")
	if !ok {
		log.Fatal("GITLAB_TOKEN is not set")
	}
	port, err := strconv.Atoi(envOr("PORT", "4567"))
	if err != nil {
		log.Fatalf("invalid PORT: %v", err)
	}
	legacy := envOr("GITLAB_PROJECTS", envOr("GITLAB_PROJECT_ID", ""))
	app := &application{
		root: root, gitlabURL: strings.TrimSuffix(gitlabURL, "/"), gitlabToken: gitlabToken, port: port,
		legacyProjects: splitList(legacy), httpClient: &http.Client{}, cache: map[string]cachedReport{}, progress: map[string]progress{},
	}
	server := &http.Server{Addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), Handler: app.handler()}
	stopped := make(chan os.Signal, 1)
	signal.Notify(stopped, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stopped
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()
	fmt.Printf("Pulse Review: http://127.0.0.1:%d\n", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func (a *application) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/report", a.handleReport)
	mux.HandleFunc("/api/progress", a.handleProgress)
	mux.HandleFunc("/api/config", a.handleConfig)
	mux.HandleFunc("/api/send", a.handleSend)
	static, _ := fs.Sub(publicFiles, "public")
	mux.Handle("/", http.FileServer(http.FS(static)))
	return mux
}

func loadEnvFile(path string) error {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if found && value != "" {
			_ = os.Setenv(key, value)
		}
	}
	return nil
}

func envOr(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
func splitList(value string) []string {
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func (a *application) dataPath(name string) string { return filepath.Join(a.root, "data", name) }
func normalizeSource(value string) string {
	text := strings.TrimSpace(value)
	if parsed, err := url.Parse(text); err == nil && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")) {
		text = parsed.Path
	}
	text = strings.TrimPrefix(text, "/")
	text = strings.TrimSuffix(text, "/")
	text = strings.TrimSuffix(text, ".git")
	return text
}

func uniqueNormalized(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeSource(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func readJSONFile[T any](path string, fallback T) (T, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fallback, nil
	}
	if err != nil {
		return fallback, err
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return fallback, err
	}
	return value, nil
}

func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pulse-review-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(data)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func defaultRoutes() []route {
	return []route{{ID: float64(1), Name: "Backend-команда", Channel: "~team-backend", Members: []string{}, Frequency: "weekdays", Time: "10:00", Enabled: true, ReviewSignal: "approval"}}
}
func (a *application) readRoutes() ([]route, error) {
	return readJSONFile(a.dataPath("routes.json"), defaultRoutes())
}
func (a *application) configuredProjects() ([]string, error) {
	values, err := readJSONFile(a.dataPath("projects.json"), a.legacyProjects)
	return uniqueNormalized(values), err
}
func (a *application) configuredGroups() ([]string, error) {
	if _, err := os.Stat(a.dataPath("groups.json")); err == nil {
		values, err := readJSONFile(a.dataPath("groups.json"), []string{})
		return uniqueNormalized(values), err
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	routes, err := a.readRoutes()
	if err != nil {
		return nil, err
	}
	values := []string{}
	for _, item := range routes {
		values = append(values, item.Groups...)
	}
	return uniqueNormalized(values), nil
}

func jsonResponse(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
func errorResponse(w http.ResponseWriter, err error) {
	jsonResponse(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}

func (a *application) gitlabGet(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.gitlabURL+"/api/v4"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("PRIVATE-TOKEN", a.gitlabToken)
	res, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, res.Body)
		return fmt.Errorf("GitLab API: %d", res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(target)
}

func (a *application) gitlabList(ctx context.Context, path string, target func() any, appendPage func(any)) error {
	for page := 1; ; page++ {
		separator := "?"
		if strings.Contains(path, "?") {
			separator = "&"
		}
		pageTarget := target()
		if err := a.gitlabGet(ctx, fmt.Sprintf("%s%sper_page=100&page=%d", path, separator, page), pageTarget); err != nil {
			return err
		}
		length := 0
		switch items := pageTarget.(type) {
		case *[]mergeRequest:
			length = len(*items)
		case *[]note:
			length = len(*items)
		case *[]award:
			length = len(*items)
		}
		appendPage(pageTarget)
		if length < 100 {
			return nil
		}
	}
}

func (a *application) listMRs(ctx context.Context, path string) ([]mergeRequest, error) {
	var all []mergeRequest
	err := a.gitlabList(ctx, path, func() any { return &[]mergeRequest{} }, func(page any) { all = append(all, *page.(*[]mergeRequest)...) })
	return all, err
}
func (a *application) listNotes(ctx context.Context, path string) ([]note, error) {
	var all []note
	err := a.gitlabList(ctx, path, func() any { return &[]note{} }, func(page any) { all = append(all, *page.(*[]note)...) })
	return all, err
}
func (a *application) listAwards(ctx context.Context, path string) ([]award, error) {
	var all []award
	err := a.gitlabList(ctx, path, func() any { return &[]award{} }, func(page any) { all = append(all, *page.(*[]award)...) })
	return all, err
}

type mrDetails struct {
	mr     mergeRequest
	notes  []note
	awards []award
	err    error
}
type eventPair struct{ projectID, iid int }
type reviewEvents struct {
	approvals map[eventPair]struct{}
	likes     map[eventPair]struct{}
}

func parseRange(from, to string) (time.Time, time.Time, error) {
	start, err := time.Parse(time.RFC3339, from+"T00:00:00Z")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := time.Parse(time.RFC3339, to+"T23:59:59Z")
	return start, end, err
}
func inRange(value string, from, to time.Time) bool {
	parsed, err := time.Parse(time.RFC3339, value)
	return err == nil && !parsed.Before(from) && !parsed.After(to)
}
func routeID(value any) string { return fmt.Sprint(value) }
func reviewMode(item *route) string {
	if item != nil && (item.ReviewSignal == "approval" || item.ReviewSignal == "like" || item.ReviewSignal == "either") {
		return item.ReviewSignal
	}
	return "approval"
}

func (a *application) updateProgress(id string, mutate func(progress) progress) {
	if id == "" {
		return
	}
	a.progressMu.Lock()
	defer a.progressMu.Unlock()
	a.progress[id] = mutate(a.progress[id])
}

func (a *application) configVersion() string {
	parts := []string{}
	for _, name := range []string{"routes.json", "groups.json", "projects.json"} {
		info, err := os.Stat(a.dataPath(name))
		if err != nil {
			parts = append(parts, "0")
		} else {
			parts = append(parts, strconv.FormatInt(info.ModTime().Unix(), 10))
		}
	}
	return strings.Join(parts, ":")
}

func (a *application) buildReport(ctx context.Context, fromValue, toValue, selectedTeam, progressID string) (report, error) {
	a.updateProgress(progressID, func(p progress) progress { return progress{Phase: "searching", Processed: 0, Total: nil} })
	key := strings.Join([]string{fromValue, toValue, selectedTeam, a.configVersion()}, "|")
	a.cacheMu.Lock()
	cached, found := a.cache[key]
	a.cacheMu.Unlock()
	if found && time.Since(cached.CreatedAt) < reportCacheTTL {
		a.updateProgress(progressID, func(p progress) progress { p.Phase = "complete"; p.Cached = true; return p })
		return cached.Report, nil
	}
	from, to, err := parseRange(fromValue, toValue)
	if err != nil {
		return report{}, err
	}
	projects, err := a.configuredProjects()
	if err != nil {
		return report{}, err
	}
	groups, err := a.configuredGroups()
	if err != nil {
		return report{}, err
	}
	routes, err := a.readRoutes()
	if err != nil {
		return report{}, err
	}
	var selected *route
	if selectedTeam != "all" {
		for i := range routes {
			if routeID(routes[i].ID) == selectedTeam {
				selected = &routes[i]
				break
			}
		}
		if selected == nil {
			return report{}, errors.New("Команда не найдена")
		}
	}
	selectedRoutes := routes
	if selected != nil {
		selectedRoutes = []route{*selected}
	}
	members := map[string]bool{}
	for _, item := range selectedRoutes {
		for _, member := range item.Members {
			members[strings.ToLower(member)] = true
		}
	}
	if len(groups) == 0 && len(projects) == 0 {
		return report{}, errors.New("Не указаны источники GitLab")
	}
	people := map[int]*reviewer{}
	ensurePerson := func(item user) *reviewer {
		if existing := people[item.ID]; existing != nil {
			return existing
		}
		username := item.Username
		person := &reviewer{ID: item.ID, Name: item.Name, Username: username, ProfileURL: a.gitlabURL + "/" + url.QueryEscape(username), OpenMergeRequestsURL: a.gitlabURL + "/dashboard/merge_requests?scope=all&state=opened&author_username=" + url.QueryEscape(username)}
		people[item.ID] = person
		return person
	}
	missing := []string{}
	for member := range members {
		var users []user
		if err := a.gitlabGet(ctx, "/users?username="+url.QueryEscape(member), &users); err != nil {
			return report{}, err
		}
		found := false
		for _, item := range users {
			if strings.EqualFold(item.Username, member) {
				ensurePerson(item)
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, member)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return report{}, fmt.Errorf("Не найдены пользователи GitLab: %s", strings.Join(missing, ", "))
	}
	allMRs := []mergeRequest{}
	for _, group := range groups {
		items, err := a.listMRs(ctx, "/groups/"+url.QueryEscape(group)+"/merge_requests?scope=all&state=all&include_subgroups=true&updated_after="+url.QueryEscape(from.Format(time.RFC3339)))
		if err != nil {
			return report{}, err
		}
		allMRs = append(allMRs, items...)
	}
	for _, project := range projects {
		items, err := a.listMRs(ctx, "/projects/"+url.QueryEscape(project)+"/merge_requests?scope=all&state=all&updated_after="+url.QueryEscape(from.Format(time.RFC3339)))
		if err != nil {
			return report{}, err
		}
		allMRs = append(allMRs, items...)
	}
	seenMR := map[eventPair]bool{}
	uniqueMRs := []mergeRequest{}
	for _, mr := range allMRs {
		pair := eventPair{mr.ProjectID, mr.IID}
		if !seenMR[pair] {
			seenMR[pair] = true
			uniqueMRs = append(uniqueMRs, mr)
		}
	}
	totalMR := len(uniqueMRs)
	a.updateProgress(progressID, func(p progress) progress {
		p.Phase = "processing"
		p.Processed = 0
		p.Total = &totalMR
		return p
	})
	jobs := make(chan mergeRequest)
	results := make(chan mrDetails)
	workers := 6
	if len(uniqueMRs) < workers {
		workers = len(uniqueMRs)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for mr := range jobs {
				notes, err := a.listNotes(ctx, fmt.Sprintf("/projects/%d/merge_requests/%d/notes?sort=asc", mr.ProjectID, mr.IID))
				if err != nil {
					results <- mrDetails{err: err}
					continue
				}
				awards, err := a.listAwards(ctx, fmt.Sprintf("/projects/%d/merge_requests/%d/award_emoji", mr.ProjectID, mr.IID))
				results <- mrDetails{mr: mr, notes: notes, awards: awards, err: err}
			}
		}()
	}
	go func() {
		for _, mr := range uniqueMRs {
			jobs <- mr
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	events := map[int]*reviewEvents{}
	getEvents := func(id int) *reviewEvents {
		if events[id] == nil {
			events[id] = &reviewEvents{approvals: map[eventPair]struct{}{}, likes: map[eventPair]struct{}{}}
		}
		return events[id]
	}
	for detail := range results {
		if detail.err != nil {
			return report{}, detail.err
		}
		mr := detail.mr
		authorID := mr.Author.ID
		if inRange(mr.CreatedAt, from, to) {
			author := ensurePerson(mr.Author)
			author.CreatedMergeRequests++
			if mr.State == "opened" {
				author.OpenedMergeRequests++
			}
		}
		for _, item := range detail.notes {
			if item.System {
				if item.Body == "approved this merge request" && item.Author.ID != authorID && inRange(item.CreatedAt, from, to) {
					ensurePerson(item.Author)
					getEvents(item.Author.ID).approvals[eventPair{mr.ProjectID, mr.IID}] = struct{}{}
				}
				continue
			}
			if item.Author.ID == authorID || !inRange(item.CreatedAt, from, to) {
				continue
			}
			ensurePerson(item.Author).Comments++
			ensurePerson(mr.Author).ReceivedComments++
		}
		for _, item := range detail.awards {
			if item.Name == "thumbsup" && item.User != nil && item.User.ID != authorID && inRange(item.CreatedAt, from, to) {
				ensurePerson(*item.User)
				getEvents(item.User.ID).likes[eventPair{mr.ProjectID, mr.IID}] = struct{}{}
			}
		}
		a.updateProgress(progressID, func(p progress) progress { p.Processed++; return p })
	}
	apply := func(source *reviewer, mode string) reviewer {
		value := *source
		event := getEvents(value.ID)
		switch mode {
		case "like":
			value.Approvals = len(event.likes)
		case "either":
			union := map[eventPair]struct{}{}
			for p := range event.approvals {
				union[p] = struct{}{}
			}
			for p := range event.likes {
				union[p] = struct{}{}
			}
			value.Approvals = len(union)
		default:
			value.Approvals = len(event.approvals)
		}
		return value
	}
	allReviewers := make([]reviewer, 0, len(people))
	for _, person := range people {
		allReviewers = append(allReviewers, apply(person, "approval"))
	}
	sortReviewers(allReviewers)
	reviewers := append([]reviewer(nil), allReviewers...)
	if selected != nil {
		filtered := []reviewer{}
		for _, person := range allReviewers {
			if len(members) == 0 || members[strings.ToLower(person.Username)] {
				source := people[person.ID]
				filtered = append(filtered, apply(source, reviewMode(selected)))
			}
		}
		reviewers = filtered
		sortReviewers(reviewers)
	}
	var teams []teamReport
	if selectedTeam == "all" {
		teams = make([]teamReport, 0, len(routes))
		for i := range routes {
			item := &routes[i]
			usernames := map[string]bool{}
			for _, name := range item.Members {
				usernames[strings.ToLower(name)] = true
			}
			teamReviewers := []reviewer{}
			for _, person := range allReviewers {
				if len(usernames) == 0 || usernames[strings.ToLower(person.Username)] {
					teamReviewers = append(teamReviewers, apply(people[person.ID], reviewMode(item)))
				}
			}
			sortReviewers(teamReviewers)
			teams = append(teams, teamReport{ID: item.ID, Name: item.Name, Reviewers: teamReviewers, ReviewSignal: reviewMode(item), Totals: calculateTotals(teamReviewers)})
		}
	}
	result := report{Project: "Все команды", Projects: projects, From: fromValue, To: toValue, Reviewers: reviewers, Teams: teams, Totals: calculateTotals(reviewers)}
	if selected != nil {
		result.Project = selected.Name
	}
	a.cacheMu.Lock()
	a.cache[key] = cachedReport{Report: result, CreatedAt: time.Now()}
	a.cacheMu.Unlock()
	a.updateProgress(progressID, func(p progress) progress {
		p.Phase = "complete"
		p.Processed = totalMR
		p.Total = &totalMR
		return p
	})
	return result, nil
}

func sortReviewers(items []reviewer) {
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Approvals+items[i].Comments+items[i].CreatedMergeRequests > items[j].Approvals+items[j].Comments+items[j].CreatedMergeRequests
	})
}
func calculateTotals(items []reviewer) totals {
	result := totals{Members: len(items)}
	for _, item := range items {
		result.Approvals += item.Approvals
		result.Comments += item.Comments
		if item.Approvals > 0 || item.Comments > 0 {
			result.Active++
		}
	}
	return result
}

func (a *application) handleReport(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	team := query.Get("team")
	if team == "" {
		team = "all"
	}
	job := query.Get("job")
	result, err := a.buildReport(r.Context(), query.Get("from"), query.Get("to"), team, job)
	if err != nil {
		a.updateProgress(job, func(p progress) progress { p.Phase = "error"; p.Error = err.Error(); return p })
		errorResponse(w, err)
		return
	}
	jsonResponse(w, http.StatusOK, result)
}
func (a *application) handleProgress(w http.ResponseWriter, r *http.Request) {
	a.progressMu.Lock()
	value, ok := a.progress[r.URL.Query().Get("id")]
	a.progressMu.Unlock()
	if !ok {
		value = progress{Phase: "searching", Processed: 0, Total: nil}
	}
	jsonResponse(w, http.StatusOK, value)
}

type configPayload struct {
	Routes   []route   `json:"routes"`
	Sources  *[]string `json:"sources"`
	Groups   []string  `json:"groups"`
	Projects []string  `json:"projects"`
}

func (a *application) classifySource(ctx context.Context, source string, knownGroups, knownProjects map[string]bool) (bool, error) {
	if knownGroups[source] {
		return true, nil
	}
	if knownProjects[source] {
		return false, nil
	}
	encoded := url.QueryEscape(source)
	var raw map[string]any
	err := a.gitlabGet(ctx, "/groups/"+encoded, &raw)
	if err == nil {
		return true, nil
	}
	if err.Error() != "GitLab API: 404" {
		return false, err
	}
	if err := a.gitlabGet(ctx, "/projects/"+encoded, &raw); err != nil {
		return false, err
	}
	return false, nil
}
func (a *application) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		routes, err := a.readRoutes()
		if err != nil {
			errorResponse(w, err)
			return
		}
		groups, err := a.configuredGroups()
		if err != nil {
			errorResponse(w, err)
			return
		}
		projects, err := a.configuredProjects()
		if err != nil {
			errorResponse(w, err)
			return
		}
		jsonResponse(w, http.StatusOK, map[string]any{"routes": routes, "groups": groups, "projects": projects, "mattermostConfigured": os.Getenv("MATTERMOST_WEBHOOK_URL") != ""})
		return
	}
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		errorResponse(w, err)
		return
	}
	payload := configPayload{}
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &payload.Routes); err != nil {
			errorResponse(w, err)
			return
		}
	} else if err := json.Unmarshal(raw, &payload); err != nil {
		errorResponse(w, err)
		return
	}
	oldGroups, _ := a.configuredGroups()
	oldProjects, _ := a.configuredProjects()
	groups := uniqueNormalized(payload.Groups)
	projects := uniqueNormalized(payload.Projects)
	if payload.Sources != nil {
		groups = []string{}
		projects = []string{}
		knownGroups := map[string]bool{}
		knownProjects := map[string]bool{}
		for _, v := range oldGroups {
			knownGroups[v] = true
		}
		for _, v := range oldProjects {
			knownProjects[v] = true
		}
		for _, source := range uniqueNormalized(*payload.Sources) {
			isGroup, err := a.classifySource(r.Context(), source, knownGroups, knownProjects)
			if err != nil {
				errorResponse(w, err)
				return
			}
			if isGroup {
				groups = append(groups, source)
			} else {
				projects = append(projects, source)
			}
		}
	}
	for i := range payload.Routes {
		payload.Routes[i].Groups = nil
		payload.Routes[i].Projects = nil
	}
	if err := writeJSONAtomic(a.dataPath("routes.json"), payload.Routes); err != nil {
		errorResponse(w, err)
		return
	}
	if err := writeJSONAtomic(a.dataPath("groups.json"), groups); err != nil {
		errorResponse(w, err)
		return
	}
	if err := writeJSONAtomic(a.dataPath("projects.json"), projects); err != nil {
		errorResponse(w, err)
		return
	}
	if !sameStrings(groups, oldGroups) || !sameStrings(projects, oldProjects) {
		a.cacheMu.Lock()
		a.cache = map[string]cachedReport{}
		a.cacheMu.Unlock()
	}
	sources := append(append([]string{}, groups...), projects...)
	jsonResponse(w, http.StatusOK, map[string]any{"saved": true, "routes": payload.Routes, "groups": groups, "projects": projects, "sources": sources})
}
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	aa := append([]string{}, a...)
	bb := append([]string{}, b...)
	sort.Strings(aa)
	sort.Strings(bb)
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func (a *application) handleSend(w http.ResponseWriter, r *http.Request) {
	webhook := os.Getenv("MATTERMOST_WEBHOOK_URL")
	if webhook == "" {
		errorResponse(w, errors.New("Mattermost webhook ещё не настроен"))
		return
	}
	var body struct {
		Route route `json:"route"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		errorResponse(w, err)
		return
	}
	if !body.Route.Enabled {
		errorResponse(w, errors.New("Оповещения для этого канала выключены"))
		return
	}
	date := time.Now().AddDate(0, 0, -1)
	day := date.Format("2006-01-02")
	result, err := a.buildReport(r.Context(), day, day, "all", "")
	if err != nil {
		errorResponse(w, err)
		return
	}
	lines := []string{"### Ревью за " + date.Format("02.01.2006"), "| Сотрудник | Approvals | Комментарии | Создано MR | Комментариев к MR | Открыто MR |", "|---|---:|---:|---:|---:|---:|"}
	for _, x := range result.Reviewers {
		lines = append(lines, fmt.Sprintf("| %s | %d | %d | %d | %d | %d |", x.Name, x.Approvals, x.Comments, x.CreatedMergeRequests, x.ReceivedComments, x.OpenedMergeRequests))
	}
	encoded, _ := json.Marshal(map[string]string{"channel": body.Route.Channel, "text": strings.Join(lines, "\n")})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, webhook, bytes.NewReader(encoded))
	if err != nil {
		errorResponse(w, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.httpClient.Do(req)
	if err != nil {
		errorResponse(w, err)
		return
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		errorResponse(w, fmt.Errorf("Mattermost: %d", res.StatusCode))
		return
	}
	jsonResponse(w, http.StatusOK, map[string]bool{"sent": true})
}
