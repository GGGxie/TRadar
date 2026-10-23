package ingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/storage"
)

type rewriteTransport struct {
	target *url.URL
	mu     sync.Mutex
	seen   []*http.Request
}

func (t *rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copyRequest := request.Clone(request.Context())
	copyRequest.URL = cloneURL(request.URL)
	t.mu.Lock()
	t.seen = append(t.seen, copyRequest)
	t.mu.Unlock()
	forward := request.Clone(request.Context())
	forward.URL = cloneURL(request.URL)
	forward.URL.Scheme = t.target.Scheme
	forward.URL.Host = t.target.Host
	forward.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(forward)
}

func (t *rewriteTransport) requests() []*http.Request {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*http.Request(nil), t.seen...)
}

func cloneURL(value *url.URL) *url.URL {
	copyValue := *value
	return &copyValue
}

func TestGitHubExecutorDiscoversRenamesAndKeepsCumulativeWhenHistoryFails(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	phase := 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/search/repositories":
			name := "acme/radar"
			stars := 120
			if phase >= 2 {
				name = "acme/tradar"
				stars = 150
			}
			if phase == 4 || !strings.Contains(request.URL.Query().Get("q"), "agent") || !strings.Contains(request.URL.Query().Get("q"), "created:") {
				json.NewEncoder(w).Encode(map[string]any{"total_count": 0, "incomplete_results": false, "items": []any{}})
				return
			}
			item := githubFixtureRepository(name, stars)
			json.NewEncoder(w).Encode(map[string]any{"total_count": 2, "incomplete_results": phase == 5, "items": []any{item, item}})
		case request.URL.Path == "/repos/acme/tradar/stargazers/history" || (phase == 1 && request.URL.Path == "/repos/acme/radar/stargazers/history"):
			// 第 1 轮：新发现候选占用空闲历史名额；第 2 轮起由跟踪记录读取。
			if phase == 2 {
				w.WriteHeader(http.StatusUnprocessableEntity)
				w.Write([]byte(`{"message":"history unavailable"}`))
				return
			}
			json.NewEncoder(w).Encode([]any{map[string]any{"week": 1791763200, "total": 28, "days": []int{1, 2, 3, 4, 5, 6, 7}}})
		case request.URL.Path == "/repos/acme/tradar":
			json.NewEncoder(w).Encode(githubFixtureRepository("acme/tradar", 175))
		default:
			t.Fatalf("unexpected GitHub request: %s?%s", request.URL.Path, request.URL.RawQuery)
		}
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	transport := &rewriteTransport{target: target}
	client := &http.Client{Transport: transport}
	executor := NewGitHubExecutor(db, client, "", func() time.Time { return now })
	runner := NewRunner(db, func() time.Time { return now })

	first := createQueuedRun(t, db, GitHub, now)
	if err := runner.Run(ctx, first.ID, executor); err != nil {
		t.Fatal(err)
	}
	firstResult, err := GetRun(ctx, db, first.ID)
	if err != nil || firstResult.Status != StatusSucceeded || firstResult.AcceptedItems != 1 || firstResult.ObservationsWritten != 9 || firstResult.RequestsMade != 7 {
		t.Fatalf("first github run: %+v err=%v", firstResult, err)
	}
	var source catalog.SourceRecord
	if err = db.QueryRow(`SELECT id,project_id,source,external_id,record_type,url,target_url,title,excerpt,published_at,fetched_at,revision FROM source_records WHERE source='GITHUB'`).Scan(
		&source.ID, &source.ProjectID, &source.Source, &source.ExternalID, &source.RecordType, &source.URL,
		&source.TargetURL, &source.Title, &source.Excerpt, &source.PublishedAt, &source.FetchedAt, &source.Revision,
	); err != nil || source.ExternalID != "4242" || source.Title != "acme/radar" {
		t.Fatalf("github source: %+v err=%v", source, err)
	}
	tracked := []string{source.ID}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, updateErr := UpdateSourceConfig(ctx, tx, GitHub, 1, SourceConfigPatch{TrackedSourceRecordIDs: &tracked}, true, now)
		return updateErr
	})
	if err != nil {
		t.Fatal(err)
	}

	phase = 2
	now = now.Add(time.Hour)
	second := createQueuedRun(t, db, GitHub, now)
	if err = runner.Run(ctx, second.ID, executor); err != nil {
		t.Fatal(err)
	}
	secondResult, err := GetRun(ctx, db, second.ID)
	if err != nil || secondResult.Status != StatusPartial || secondResult.ObservationsWritten != 2 || len(secondResult.Errors) != 1 || secondResult.Errors[0].Code != "HISTORY_UNAVAILABLE" {
		t.Fatalf("history failure run: %+v err=%v", secondResult, err)
	}
	assertTableCount(t, db, "projects", 1)
	assertTableCount(t, db, "source_records", 1)
	if err = db.QueryRow(`SELECT title,revision FROM source_records WHERE id=?`, source.ID).Scan(&source.Title, &source.Revision); err != nil || source.Title != "acme/tradar" || source.Revision != 2 {
		t.Fatalf("renamed source: %+v err=%v", source, err)
	}
	var cumulative int
	if err = db.QueryRow(`SELECT count(*) FROM observations WHERE run_id=? AND basis='CUMULATIVE'`, second.ID).Scan(&cumulative); err != nil || cumulative != 2 {
		t.Fatalf("cumulative after history failure=%d err=%v", cumulative, err)
	}

	phase = 3
	now = now.Add(time.Hour)
	third := createQueuedRun(t, db, GitHub, now)
	if err = runner.Run(ctx, third.ID, executor); err != nil {
		t.Fatal(err)
	}
	thirdResult, err := GetRun(ctx, db, third.ID)
	if err != nil || thirdResult.Status != StatusSucceeded || thirdResult.ObservationsWritten != 9 {
		t.Fatalf("history success run: %+v err=%v", thirdResult, err)
	}
	var dailyCount, dailySum int
	if err = db.QueryRow(`SELECT count(*),sum(value) FROM observations WHERE run_id=? AND basis='SOURCE_DAILY'`, third.ID).Scan(&dailyCount, &dailySum); err != nil || dailyCount != 7 || dailySum != 28 {
		t.Fatalf("daily history count=%d sum=%d err=%v", dailyCount, dailySum, err)
	}
	var periodKey, providerPeriod string
	if err = db.QueryRow(`SELECT period_key,provider_period FROM observations WHERE run_id=? AND basis='SOURCE_DAILY' ORDER BY period_key LIMIT 1`, third.ID).Scan(&periodKey, &providerPeriod); err != nil || periodKey != "1791763200:0" || !strings.Contains(providerPeriod, `"dayIndex":0`) || !strings.Contains(providerPeriod, `"timeZone":"UNSPECIFIED"`) {
		t.Fatalf("provider period key=%q value=%q err=%v", periodKey, providerPeriod, err)
	}
	phase = 4
	now = now.Add(time.Hour)
	fourth := createQueuedRun(t, db, GitHub, now)
	if err = runner.Run(ctx, fourth.ID, executor); err != nil {
		t.Fatal(err)
	}
	fourthResult, err := GetRun(ctx, db, fourth.ID)
	if err != nil || fourthResult.Status != StatusSucceeded || fourthResult.AcceptedItems != 1 || fourthResult.ObservationsWritten != 9 || fourthResult.RequestsMade != 8 {
		t.Fatalf("tracked refresh run: %+v err=%v", fourthResult, err)
	}
	var refreshedStars int
	if err = db.QueryRow(`SELECT value FROM observations o JOIN source_records sr ON sr.id=o.source_record_id WHERE o.run_id=? AND o.metric='STARS' AND o.basis='CUMULATIVE'`, fourth.ID).Scan(&refreshedStars); err != nil || refreshedStars != 175 {
		t.Fatalf("tracked stars=%d err=%v", refreshedStars, err)
	}
	phase = 5
	now = now.Add(time.Hour)
	fifth := createQueuedRun(t, db, GitHub, now)
	if err = runner.Run(ctx, fifth.ID, executor); err != nil {
		t.Fatal(err)
	}
	fifthResult, err := GetRun(ctx, db, fifth.ID)
	if err != nil || fifthResult.Status != StatusPartial || !fifthResult.IsTruncated || len(fifthResult.Errors) != 1 || fifthResult.Errors[0].Code != "INCOMPLETE_RESULTS" {
		t.Fatalf("incomplete search run: %+v err=%v", fifthResult, err)
	}

	requests := transport.requests()
	if len(requests) == 0 {
		t.Fatal("no github requests captured")
	}
	for _, request := range requests {
		if request.URL.Scheme != "https" || request.URL.Host != "api.github.com" {
			t.Fatalf("non-fixed github target: %s", request.URL)
		}
		if request.Header.Get("Accept") != "application/vnd.github+json" || request.Header.Get("X-GitHub-Api-Version") != "2026-03-10" || request.Header.Get("Authorization") != "" {
			t.Fatalf("github headers: %v", request.Header)
		}
	}
	firstQuery := requests[0].URL.Query()
	if !strings.Contains(firstQuery.Get("q"), "created:>=2026-04-18") || firstQuery.Get("sort") != "stars" || firstQuery.Get("order") != "desc" || firstQuery.Get("per_page") != "5" {
		t.Fatalf("github discovery query: %s", requests[0].URL)
	}
	var sawUpdatedQuery bool
	for _, request := range requests {
		query := request.URL.Query()
		if strings.Contains(query.Get("q"), "pushed:>=") && query.Get("sort") == "updated" && query.Get("per_page") == "5" {
			sawUpdatedQuery = true
		}
	}
	if !sawUpdatedQuery {
		t.Fatal("missing recent-push GitHub discovery bucket")
	}
}

func TestGitHubExecutorGivesHistorySlotsToTrackedThenTopNewCandidates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	stars := map[string]int{"r10": 10, "r500": 500, "r300": 300, "r50": 50, "r40": 40, "r20": 20}
	var historyPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/search/repositories":
			if !strings.Contains(request.URL.Query().Get("q"), "agent") || !strings.Contains(request.URL.Query().Get("q"), "created:") {
				json.NewEncoder(w).Encode(map[string]any{"total_count": 0, "incomplete_results": false, "items": []any{}})
				return
			}
			items := []any{}
			for index, name := range []string{"r10", "r500", "r300", "r50", "r40", "r20"} {
				item := githubFixtureRepository("acme/"+name, stars[name])
				item["id"] = 100 + index
				items = append(items, item)
			}
			json.NewEncoder(w).Encode(map[string]any{"total_count": len(items), "incomplete_results": false, "items": items})
		case strings.HasSuffix(request.URL.Path, "/stargazers/history"):
			historyPaths = append(historyPaths, request.URL.Path)
			json.NewEncoder(w).Encode([]any{map[string]any{"week": 1791763200, "total": 7, "days": []int{1, 1, 1, 1, 1, 1, 1}}})
		case strings.HasPrefix(request.URL.Path, "/repos/acme/"):
			name := strings.TrimPrefix(request.URL.Path, "/repos/acme/")
			item := githubFixtureRepository("acme/"+name, stars[name])
			item["id"] = 101 // 只有 r500 会被跟踪
			json.NewEncoder(w).Encode(item)
		default:
			t.Fatalf("unexpected GitHub request: %s?%s", request.URL.Path, request.URL.RawQuery)
		}
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	client := &http.Client{Transport: &rewriteTransport{target: target}}
	executor := NewGitHubExecutor(db, client, "", func() time.Time { return now })
	runner := NewRunner(db, func() time.Time { return now })
	candidates := 12
	err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, updateErr := UpdateSourceConfig(ctx, tx, GitHub, 1, SourceConfigPatch{CandidatesPerKeyword: &candidates}, false, now)
		return updateErr
	})
	if err != nil {
		t.Fatal(err)
	}

	first := createQueuedRun(t, db, GitHub, now)
	if err = runner.Run(ctx, first.ID, executor); err != nil {
		t.Fatal(err)
	}
	firstResult, err := GetRun(ctx, db, first.ID)
	if err != nil || firstResult.Status != StatusSucceeded {
		t.Fatalf("first run: %+v err=%v", firstResult, err)
	}
	wantFirst := []string{"/repos/acme/r500/stargazers/history", "/repos/acme/r300/stargazers/history", "/repos/acme/r50/stargazers/history", "/repos/acme/r40/stargazers/history", "/repos/acme/r20/stargazers/history"}
	if strings.Join(historyPaths, ",") != strings.Join(wantFirst, ",") {
		t.Fatalf("first history requests=%v want=%v", historyPaths, wantFirst)
	}

	var trackedID string
	if err = db.QueryRow(`SELECT id FROM source_records WHERE source='GITHUB' AND external_id='101'`).Scan(&trackedID); err != nil {
		t.Fatal(err)
	}
	tracked := []string{trackedID}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, updateErr := UpdateSourceConfig(ctx, tx, GitHub, 2, SourceConfigPatch{TrackedSourceRecordIDs: &tracked}, false, now)
		return updateErr
	})
	if err != nil {
		t.Fatal(err)
	}
	historyPaths = nil
	now = now.Add(time.Hour)
	second := createQueuedRun(t, db, GitHub, now)
	if err = runner.Run(ctx, second.ID, executor); err != nil {
		t.Fatal(err)
	}
	// 跟踪仓库先占名额；剩余名额只给尚无日级历史的候选（r10）。
	wantSecond := []string{"/repos/acme/r500/stargazers/history", "/repos/acme/r10/stargazers/history"}
	if strings.Join(historyPaths, ",") != strings.Join(wantSecond, ",") {
		t.Fatalf("second history requests=%v want=%v", historyPaths, wantSecond)
	}
}

func githubFixtureRepository(fullName string, stars int) map[string]any {
	return map[string]any{
		"id": 4242, "full_name": fullName, "html_url": "https://github.com/" + fullName,
		"url": "https://api.github.com/repos/" + fullName, "description": "A trend radar",
		"created_at": "2026-04-20T10:00:00Z", "updated_at": "2026-10-15T02:00:00Z",
		"pushed_at": "2026-10-15T02:00:00Z", "stargazers_count": stars, "forks_count": 12,
	}
}

func TestGitHubExecutorStopsAfterAuthenticationFailure(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 4, 30, 0, 0, time.UTC)
	requests := 0
	const token = "super-secret-github-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests++
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"bad credentials for super-secret-github-token"}`))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	client := &http.Client{Transport: &rewriteTransport{target: target}}
	executor := NewGitHubExecutor(db, client, token, func() time.Time { return now })
	run := createQueuedRun(t, db, GitHub, now)
	if err := NewRunner(db, func() time.Time { return now }).Run(ctx, run.ID, executor); err != nil {
		t.Fatal(err)
	}
	stored, err := GetRun(ctx, db, run.ID)
	if err != nil || stored.Status != StatusFailed || stored.RequestsMade != 1 || len(stored.Errors) != 1 || stored.Errors[0].Code != "AUTH_FAILED" || requests != 1 {
		t.Fatalf("auth failure did not stop source: %+v requests=%d err=%v", stored, requests, err)
	}
	encoded, err := json.Marshal(stored)
	if err != nil || strings.Contains(string(encoded), token) || strings.Contains(string(encoded), "bad credentials") {
		t.Fatalf("secret or upstream body persisted: %s err=%v", encoded, err)
	}
}

func TestHackerNewsExecutorKeepsDiscussionIdentityAndSkipReasons(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 5, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v0/topstories.json":
			json.NewEncoder(w).Encode([]int{101, 102, 103, 104, 105, 106, 107, 108, 109})
		case "/v0/newstories.json":
			json.NewEncoder(w).Encode([]int{101})
		case "/v0/showstories.json":
			json.NewEncoder(w).Encode([]int{})
		case "/v0/item/101.json", "/v0/item/102.json":
			id, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/v0/item/"), ".json"))
			json.NewEncoder(w).Encode(map[string]any{"id": id, "type": "story", "title": "Shared project " + strconv.Itoa(id), "url": "https://shared.example/product?utm_source=hn", "score": 10 + id, "descendants": 2, "time": 1792036800, "extra_future_field": true})
		case "/v0/item/103.json":
			json.NewEncoder(w).Encode(map[string]any{"id": 103, "type": "story", "title": "Dead", "dead": true})
		case "/v0/item/104.json":
			json.NewEncoder(w).Encode(map[string]any{"id": 104, "deleted": true})
		case "/v0/item/105.json":
			w.Write([]byte("null"))
		case "/v0/item/106.json":
			json.NewEncoder(w).Encode(map[string]any{"id": 106, "type": "job", "title": "Job"})
		case "/v0/item/107.json":
			json.NewEncoder(w).Encode(map[string]any{"id": 107, "type": "story", "title": "Show HN without link", "score": 3, "descendants": 0, "time": 1792036801})
		case "/v0/item/108.json":
			json.NewEncoder(w).Encode(map[string]any{"id": 108, "type": "story", "title": "Unsafe external link", "url": "javascript:alert(1)", "score": 4, "descendants": 1, "time": 1792036802})
		case "/v0/item/109.json":
			json.NewEncoder(w).Encode(map[string]any{"id": 109, "type": "story", "title": "Metrics are unknown"})
		default:
			t.Fatalf("unexpected HN request: %s", request.URL)
		}
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	transport := &rewriteTransport{target: target}
	client := &http.Client{Transport: transport}
	executor := NewHackerNewsExecutor(db, client, func() time.Time { return now })
	runner := NewRunner(db, func() time.Time { return now })
	run := createQueuedRun(t, db, HackerNews, now)
	if err := runner.Run(ctx, run.ID, executor); err != nil {
		t.Fatal(err)
	}
	result, err := GetRun(ctx, db, run.ID)
	if err != nil || result.Status != StatusSucceeded || result.AcceptedItems != 5 || result.SkippedItems != 4 || result.ObservationsWritten != 8 || len(result.Errors) != 5 {
		t.Fatalf("hn result: %+v err=%v", result, err)
	}
	wantCodes := map[string]bool{"ITEM_DEAD": false, "ITEM_DELETED": false, "ITEM_NULL": false, "ITEM_NOT_STORY": false, "ITEM_URL_INVALID": false}
	for _, item := range result.Errors {
		if _, ok := wantCodes[item.Code]; ok {
			wantCodes[item.Code] = true
		}
	}
	for code, found := range wantCodes {
		if !found {
			t.Errorf("missing skip reason %s: %+v", code, result.Errors)
		}
	}
	assertTableCount(t, db, "projects", 4)
	assertTableCount(t, db, "source_records", 5)
	var sharedProjects int
	if err = db.QueryRow(`SELECT count(DISTINCT project_id) FROM source_records WHERE external_id IN ('101','102')`).Scan(&sharedProjects); err != nil || sharedProjects != 1 {
		t.Fatalf("shared HN target projects=%d err=%v", sharedProjects, err)
	}
	var noTarget int
	if err = db.QueryRow(`SELECT count(*) FROM source_records WHERE external_id IN ('107','108') AND target_url IS NULL`).Scan(&noTarget); err != nil || noTarget != 2 {
		t.Fatalf("HN safe fallback targets=%d err=%v", noTarget, err)
	}
	var unknownMetrics int
	if err = db.QueryRow(`SELECT count(*) FROM observations o JOIN source_records sr ON sr.id=o.source_record_id WHERE sr.external_id='109'`).Scan(&unknownMetrics); err != nil || unknownMetrics != 0 {
		t.Fatalf("missing HN metrics became observations=%d err=%v", unknownMetrics, err)
	}
	for _, request := range transport.requests() {
		if request.URL.Scheme != "https" || request.URL.Host != "hacker-news.firebaseio.com" {
			t.Fatalf("non-fixed HN target: %s", request.URL)
		}
	}
}

func TestEmptyRemoteResultsAreSuccessfulWithoutInventedProjects(t *testing.T) {
	t.Run("github", func(t *testing.T) {
		db := openTestDB(t)
		now := time.Date(2026, 10, 15, 6, 0, 0, 0, time.UTC)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/search/repositories" {
				t.Errorf("unexpected GitHub empty request: %s", request.URL)
				http.Error(w, "unexpected", http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"total_count": 0, "incomplete_results": false, "items": []any{}})
		}))
		defer server.Close()
		target, _ := url.Parse(server.URL)
		executor := NewGitHubExecutor(db, &http.Client{Transport: &rewriteTransport{target: target}}, "", func() time.Time { return now })
		run := createQueuedRun(t, db, GitHub, now)
		if err := NewRunner(db, func() time.Time { return now }).Run(context.Background(), run.ID, executor); err != nil {
			t.Fatal(err)
		}
		stored, err := GetRun(context.Background(), db, run.ID)
		if err != nil || stored.Status != StatusSucceededEmpty || stored.RequestsMade != 6 || stored.AcceptedItems != 0 || stored.ObservationsWritten != 0 {
			t.Fatalf("empty GitHub run: %+v err=%v", stored, err)
		}
		assertTableCount(t, db, "projects", 0)
	})

	t.Run("hacker news", func(t *testing.T) {
		db := openTestDB(t)
		now := time.Date(2026, 10, 15, 6, 30, 0, 0, time.UTC)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			if !strings.HasSuffix(request.URL.Path, "stories.json") {
				t.Errorf("unexpected HN empty request: %s", request.URL)
				http.Error(w, "unexpected", http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode([]int{})
		}))
		defer server.Close()
		target, _ := url.Parse(server.URL)
		executor := NewHackerNewsExecutor(db, &http.Client{Transport: &rewriteTransport{target: target}}, func() time.Time { return now })
		run := createQueuedRun(t, db, HackerNews, now)
		if err := NewRunner(db, func() time.Time { return now }).Run(context.Background(), run.ID, executor); err != nil {
			t.Fatal(err)
		}
		stored, err := GetRun(context.Background(), db, run.ID)
		if err != nil || stored.Status != StatusSucceededEmpty || stored.RequestsMade != 3 || stored.AcceptedItems != 0 || stored.ObservationsWritten != 0 {
			t.Fatalf("empty HN run: %+v err=%v", stored, err)
		}
		assertTableCount(t, db, "projects", 0)
	})
}
