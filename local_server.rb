require "webrick"
require "net/http"
require "json"
require "uri"
require "time"
require "date"
require "fileutils"
require "set"

ROOT = File.expand_path(__dir__)
ENV_FILE = File.join(ROOT, ".env.local")
DATA_DIR = File.join(ROOT, "data")
ROUTES_FILE = File.join(DATA_DIR, "routes.json")
File.readlines(ENV_FILE, chomp: true).each do |line|
  next if line.empty? || line.start_with?("#") || !line.include?("=")
  key, value = line.split("=", 2); ENV[key] = value unless value.to_s.empty?
end if File.exist?(ENV_FILE)
GITLAB_URL = ENV.fetch("GITLAB_URL").sub(%r{/$}, "")
GITLAB_TOKEN = ENV.fetch("GITLAB_TOKEN")
APP_PORT = Integer(ENV.fetch("PORT", "4567"))
PROJECTS = ENV.fetch("GITLAB_PROJECTS", ENV.fetch("GITLAB_PROJECT_ID", "")).split(",").map(&:strip).reject(&:empty?)
PROJECTS_FILE = File.join(DATA_DIR, "projects.json")
GROUPS_FILE = File.join(DATA_DIR, "groups.json")
REPORT_CACHE = {}
REPORT_CACHE_MUTEX = Mutex.new
REPORT_CACHE_TTL = 15 * 60
REPORT_PROGRESS = {}
REPORT_PROGRESS_MUTEX = Mutex.new
def update_progress(id, values)
  return if id.to_s.empty?
  REPORT_PROGRESS_MUTEX.synchronize { REPORT_PROGRESS[id] = (REPORT_PROGRESS[id] || {}).merge(values) }
end
def normalize_project(value)
  text = value.to_s.strip
  text = URI(text).path if text.match?(%r{^https?://}i)
  text.sub(%r{^/+}, "").sub(%r{/+$}, "").sub(/\.git$/, "")
rescue URI::InvalidURIError
  text
end
def normalize_group(value); normalize_project(value); end
def configured_projects; (File.exist?(PROJECTS_FILE) ? JSON.parse(File.read(PROJECTS_FILE)) : PROJECTS).map { |item| normalize_project(item) }.uniq; end
def configured_groups
  source = if File.exist?(GROUPS_FILE)
    JSON.parse(File.read(GROUPS_FILE))
  else
    read_routes.flat_map { |route| route["groups"] || [] }
  end
  source.map { |item| normalize_group(item) }.reject(&:empty?).uniq
end

def gitlab_request(path)
  uri = URI("#{GITLAB_URL}/api/v4#{path}"); http = Net::HTTP.new(uri.host, uri.port, nil); http.use_ssl = uri.scheme == "https"
  request = Net::HTTP::Get.new(uri); request["PRIVATE-TOKEN"] = GITLAB_TOKEN; response = http.request(request)
  raise "GitLab API: #{response.code}" unless response.is_a?(Net::HTTPSuccess)
  JSON.parse(response.body)
end

def gitlab_list(path)
  result = []; page = 1
  loop do
    separator = path.include?("?") ? "&" : "?"; items = gitlab_request("#{path}#{separator}per_page=100&page=#{page}"); result.concat(items)
    break if items.length < 100
    page += 1
  end
  result
end

def classify_source(source, known_groups, known_projects)
  return :group if known_groups.include?(source)
  return :project if known_projects.include?(source)
  encoded = URI.encode_www_form_component(source)
  gitlab_request("/groups/#{encoded}")
  :group
rescue => error
  raise error unless error.message == "GitLab API: 404"
  gitlab_request("/projects/#{encoded}")
  :project
end

def in_range?(value, from_time, to_time)
  value && (time = Time.parse(value)) >= from_time && time <= to_time
end

def build_report(from_value, to_value, selected_team = "all", progress_id = nil)
  update_progress(progress_id, { phase: "searching", processed: 0, total: nil })
  config_version = [ROUTES_FILE, GROUPS_FILE, PROJECTS_FILE].map { |path| File.exist?(path) ? File.mtime(path).to_i : 0 }.join(":")
  cache_key = [from_value, to_value, selected_team, config_version].join("|")
  cached = REPORT_CACHE_MUTEX.synchronize { REPORT_CACHE[cache_key] }
  if cached && Time.now.to_i - cached[:created_at] < REPORT_CACHE_TTL
    update_progress(progress_id, { phase: "complete", cached: true })
    return cached[:report]
  end
  from_time = Time.parse("#{from_value}T00:00:00Z"); to_time = Time.parse("#{to_value}T23:59:59Z")
  available_projects = configured_projects
  groups = configured_groups
  routes = read_routes
  team = selected_team == "all" ? nil : routes.find { |route| route["id"].to_s == selected_team.to_s }
  raise "Команда не найдена" if selected_team != "all" && !team
  selected_routes = team ? [team] : routes
  projects = available_projects
  member_usernames = selected_routes.flat_map { |route| route["members"] || [] }.map(&:downcase).uniq
  raise "Не указаны источники GitLab" if groups.empty? && projects.empty?
  people = {}; ensure_person = ->(user) do
    username = user["username"]
    people[user["id"]] ||= { id: user["id"], name: user["name"], username: username, approvals: 0, comments: 0, receivedComments: 0, createdMergeRequests: 0, openedMergeRequests: 0,
      profileUrl: "#{GITLAB_URL}/#{URI.encode_www_form_component(username)}",
      openMergeRequestsUrl: "#{GITLAB_URL}/dashboard/merge_requests?scope=all&state=opened&author_username=#{URI.encode_www_form_component(username)}" }
  end
  missing_members = []
  member_usernames.each do |username|
    users = gitlab_request("/users?username=#{URI.encode_www_form_component(username)}")
    user = users.find { |item| item["username"].downcase == username }
    user ? ensure_person.call(user) : missing_members << username
  end
  raise "Не найдены пользователи GitLab: #{missing_members.join(', ')}" unless missing_members.empty?
  merge_requests = []
  groups.each do |group|
    encoded_group = URI.encode_www_form_component(group)
    query = "/groups/#{encoded_group}/merge_requests?scope=all&state=all&include_subgroups=true&updated_after=#{URI.encode_www_form_component(from_time.iso8601)}"
    merge_requests.concat(gitlab_list(query))
  end
  projects.each do |project|
    encoded = URI.encode_www_form_component(project)
    query = "/projects/#{encoded}/merge_requests?scope=all&state=all&updated_after=#{URI.encode_www_form_component(from_time.iso8601)}"
    merge_requests.concat(gitlab_list(query))
  end
  unique_merge_requests = merge_requests.uniq { |mr| [mr["project_id"], mr["iid"]] }
  update_progress(progress_id, { phase: "processing", processed: 0, total: unique_merge_requests.length })
  jobs = Queue.new; unique_merge_requests.each { |mr| jobs << mr }
  details = Queue.new
  [6, unique_merge_requests.length].min.times.map do
    Thread.new do
      loop do
        mr = jobs.pop(true) rescue nil
        break unless mr
        encoded = mr["project_id"]
        notes = gitlab_list("/projects/#{encoded}/merge_requests/#{mr["iid"]}/notes?sort=asc")
        awards = gitlab_list("/projects/#{encoded}/merge_requests/#{mr["iid"]}/award_emoji")
        details << [mr, notes, awards]
        REPORT_PROGRESS_MUTEX.synchronize do
          progress = REPORT_PROGRESS[progress_id]
          progress[:processed] += 1 if progress_id && progress
        end
      end
    end
  end.each(&:join)
  review_events = Hash.new { |hash, user_id| hash[user_id] = { approval: Set.new, like: Set.new } }
  until details.empty?
      mr, notes, awards = details.pop
      author_id = mr.dig("author", "id")
      if in_range?(mr["created_at"], from_time, to_time)
        author = ensure_person.call(mr["author"])
        author[:createdMergeRequests] += 1
        author[:openedMergeRequests] += 1 if mr["state"] == "opened"
      end
      notes.each do |note|
        user = note["author"]
        if note["system"]
          next unless note["body"] == "approved this merge request" && user["id"] != author_id && in_range?(note["created_at"], from_time, to_time)
          pair = [mr["project_id"], mr["iid"]]
          ensure_person.call(user)
          review_events[user["id"]][:approval].add(pair)
          next
        end
        next if user["id"] == author_id || !in_range?(note["created_at"], from_time, to_time)
        ensure_person.call(user)[:comments] += 1
        ensure_person.call(mr["author"])[:receivedComments] += 1
      end
      awards.each do |award|
        user = award["user"]
        next unless award["name"] == "thumbsup" && user && user["id"] != author_id && in_range?(award["created_at"], from_time, to_time)
        ensure_person.call(user)
        review_events[user["id"]][:like].add([mr["project_id"], mr["iid"]])
      end
  end
  review_mode = ->(route) do
    value = route && route["reviewSignal"]
    %w[approval like either].include?(value) ? value : "approval"
  end
  apply_review_metric = ->(person, mode) do
    events = review_events[person[:id]]
    count = case mode
    when "like" then events[:like].length
    when "either" then (events[:approval] | events[:like]).length
    else events[:approval].length
    end
    person.merge(approvals: count)
  end
  all_reviewers = people.values.map { |person| apply_review_metric.call(person, "approval") }.sort_by { |person| -(person[:approvals] + person[:comments] + person[:createdMergeRequests]) }
  reviewers = all_reviewers.dup
  if team
    reviewers.select! { |person| member_usernames.include?(person[:username].downcase) } unless member_usernames.empty?
    reviewers = reviewers.map { |person| apply_review_metric.call(person, review_mode.call(team)) }
    reviewers.sort_by! { |person| -(person[:approvals] + person[:comments] + person[:createdMergeRequests]) }
  end
  team_reports = if selected_team == "all"
    routes.map do |route|
      usernames = (route["members"] || []).map(&:downcase)
      team_reviewers = usernames.empty? ? all_reviewers : all_reviewers.select { |person| usernames.include?(person[:username].downcase) }
      team_reviewers = team_reviewers.map { |person| apply_review_metric.call(person, review_mode.call(route)) }
      team_reviewers.sort_by! { |person| -(person[:approvals] + person[:comments] + person[:createdMergeRequests]) }
      { id: route["id"], name: route["name"], reviewers: team_reviewers,
        reviewSignal: review_mode.call(route),
        totals: { approvals: team_reviewers.sum { |x| x[:approvals] }, comments: team_reviewers.sum { |x| x[:comments] }, active: team_reviewers.count { |x| x[:approvals] > 0 || x[:comments] > 0 }, members: team_reviewers.length } }
    end
  end
  report = { project: team ? team["name"] : "Все команды", projects: available_projects, from: from_value, to: to_value, reviewers: reviewers, teams: team_reports,
    totals: { approvals: reviewers.sum { |x| x[:approvals] }, comments: reviewers.sum { |x| x[:comments] }, active: reviewers.count { |x| x[:approvals] > 0 || x[:comments] > 0 }, members: reviewers.length } }
  REPORT_CACHE_MUTEX.synchronize { REPORT_CACHE[cache_key] = { report: report, created_at: Time.now.to_i } }
  update_progress(progress_id, { phase: "complete", processed: unique_merge_requests.length, total: unique_merge_requests.length })
  report
end

def default_routes
  [{ id: 1, name: "Backend-команда", channel: "~team-backend", members: [], frequency: "weekdays", time: "10:00", enabled: true, reviewSignal: "approval" }]
end
def read_routes; File.exist?(ROUTES_FILE) ? JSON.parse(File.read(ROUTES_FILE)) : default_routes; end
def json_response(response, body, status = 200); response.status = status; response["Content-Type"] = "application/json; charset=utf-8"; response.body = JSON.generate(body); end

server = WEBrick::HTTPServer.new(Port: APP_PORT, BindAddress: "127.0.0.1", DocumentRoot: File.join(ROOT, "public"), AccessLog: [], Logger: WEBrick::Log.new($stderr, WEBrick::Log::WARN))
server.mount_proc("/api/report") do |req, res|
  progress_id = req.query["job"]
  json_response(res, build_report(req.query["from"], req.query["to"], req.query["team"] || "all", progress_id))
rescue
  update_progress(progress_id, { phase: "error", error: $!.message })
  json_response(res, { error: $!.message }, 500)
end
server.mount_proc("/api/progress") do |req, res|
  progress = REPORT_PROGRESS_MUTEX.synchronize { REPORT_PROGRESS[req.query["id"]] || { phase: "searching", processed: 0, total: nil } }
  json_response(res, progress)
end
server.mount_proc "/api/config" do |req, res|
  if req.request_method == "POST"
    FileUtils.mkdir_p(DATA_DIR); payload = JSON.parse(req.body); routes = payload.is_a?(Array) ? payload : payload["routes"]
    old_groups = configured_groups
    old_projects = configured_projects
    if payload.key?("sources")
      sources = (payload["sources"] || []).map { |item| normalize_project(item) }.reject(&:empty?).uniq
      groups, projects = sources.partition { |source| classify_source(source, old_groups, old_projects) == :group }
    else
      groups = (payload["groups"] || []).map { |item| normalize_group(item) }.reject(&:empty?).uniq
      projects = (payload["projects"] || []).map { |item| normalize_project(item) }.reject(&:empty?).uniq
    end
    routes.each { |route| route.delete("groups"); route.delete("projects") }
    File.write(ROUTES_FILE, JSON.pretty_generate(routes)); File.write(GROUPS_FILE, JSON.pretty_generate(groups)); File.write(PROJECTS_FILE, JSON.pretty_generate(projects))
    REPORT_CACHE_MUTEX.synchronize { REPORT_CACHE.clear } if groups.sort != old_groups.sort || projects.sort != old_projects.sort
    json_response(res, { saved: true, routes: routes, groups: groups, projects: projects, sources: groups + projects })
  else
    json_response(res, { routes: read_routes, groups: configured_groups, projects: configured_projects, mattermostConfigured: !ENV["MATTERMOST_WEBHOOK_URL"].to_s.empty? })
  end
rescue
  json_response(res, { error: $!.message }, 500)
end
server.mount_proc "/api/send" do |req, res|
  begin
    raise "Mattermost webhook ещё не настроен" if ENV["MATTERMOST_WEBHOOK_URL"].to_s.empty?
    route = JSON.parse(req.body)["route"]; raise "Оповещения для этого канала выключены" unless route["enabled"]
    date = Date.today - 1; report = build_report(date.iso8601, date.iso8601, "all")
    rows = report[:reviewers].map { |x| "| #{x[:name]} | #{x[:approvals]} | #{x[:comments]} | #{x[:createdMergeRequests]} | #{x[:receivedComments]} | #{x[:openedMergeRequests]} |" }
    text = (["### Ревью за #{date.strftime("%d.%m.%Y")}", "| Сотрудник | Approvals | Комментарии | Создано MR | Комментариев к MR | Открыто MR |", "|---|---:|---:|---:|---:|---:|"] + rows).join("\n")
    uri = URI(ENV["MATTERMOST_WEBHOOK_URL"]); http = Net::HTTP.new(uri.host, uri.port, nil); http.use_ssl = uri.scheme == "https"
    post = Net::HTTP::Post.new(uri); post["Content-Type"] = "application/json"; post.body = JSON.generate({ channel: route["channel"], text: text }); result = http.request(post)
    raise "Mattermost: #{result.code}" unless result.is_a?(Net::HTTPSuccess); json_response(res, { sent: true })
  rescue
    json_response(res, { error: $!.message }, 500)
  end
end
trap("INT") { server.shutdown }; trap("TERM") { server.shutdown }; puts "Pulse Review: http://127.0.0.1:#{APP_PORT}"; server.start
