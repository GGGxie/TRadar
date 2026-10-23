package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/ingestion"
	"tradar/internal/storage"
)

func testServer(t *testing.T) http.Handler {
	t.Helper()
	handler, _ := testServerWithDB(t)
	return handler
}
func testServerWithDB(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, Config{}), db
}
func call(h http.Handler, method, path, body, key, rev string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost"+path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if rev != "" {
		r.Header.Set("If-Match", rev)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestProjectsHTTPIdempotencyAndVersion(t *testing.T) {
	h := testServer(t)
	body := `{"name":"测试雷达","canonicalUrl":"https://example.com/?utm_source=x"}`
	first := call(h, "POST", "/api/v1/projects", body, "intent-1", "")
	if first.Code != 201 {
		t.Fatalf("%d %s", first.Code, first.Body)
	}
	again := call(h, "POST", "/api/v1/projects", body, "intent-1", "")
	if again.Code != 201 || again.Body.String() != first.Body.String() {
		t.Fatalf("replay differs: %s", again.Body)
	}
	mismatch := call(h, "POST", "/api/v1/projects", `{"name":"other","canonicalUrl":"https://example.org"}`, "intent-1", "")
	if mismatch.Code != 422 {
		t.Fatalf("mismatched key: %d", mismatch.Code)
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		}
	}
	json.Unmarshal(first.Body.Bytes(), &created)
	path := "/api/v1/projects/" + created.Data.ID
	updated := call(h, "PATCH", path, `{"name":"已更新"}`, "", `"1"`)
	if updated.Code != 200 || updated.Header().Get("ETag") != `"2"` {
		t.Fatalf("update: %d %s", updated.Code, updated.Body)
	}
	stale := call(h, "PATCH", path, `{"name":"不应覆盖"}`, "", `"1"`)
	if stale.Code != 412 {
		t.Fatalf("stale=%d", stale.Code)
	}
	missing := call(h, "PATCH", path, `{"name":"missing"}`, "", "")
	if missing.Code != 428 {
		t.Fatalf("missing=%d", missing.Code)
	}
	list := call(h, "GET", "/api/v1/projects?pageSize=1", "", "", "")
	var page struct {
		Data       []json.RawMessage
		Pagination struct{ TotalItems int }
	}
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil || list.Code != 200 || len(page.Data) != 1 || page.Pagination.TotalItems != 1 {
		t.Fatalf("list %s %v", list.Body, err)
	}
}
func TestConcurrentRetryCreatesOneProject(t *testing.T) {
	h := testServer(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := call(h, "POST", "/api/v1/projects", `{"name":"one","canonicalUrl":"https://example.com"}`, "same", "")
			if w.Code != 201 && w.Code != 409 {
				t.Errorf("code=%d %s", w.Code, w.Body)
			}
		}()
	}
	wg.Wait()
	w := call(h, "GET", "/api/v1/projects", "", "", "")
	var page struct{ Pagination struct{ TotalItems int } }
	json.Unmarshal(w.Body.Bytes(), &page)
	if page.Pagination.TotalItems != 1 {
		t.Fatalf("created %d", page.Pagination.TotalItems)
	}
}
func TestHTTPBoundaries(t *testing.T) {
	h := testServer(t)
	cases := []struct {
		method, path, body, origin, host, typ string
		want                                  int
	}{
		{"POST", "/api/v1/projects", `{}`, "https://evil.example", "localhost", "application/json", 403},
		{"GET", "/api/v1/projects", "", "", "evil.example", "", 403},
		{"POST", "/api/v1/projects", `{}`, "", "localhost", "text/plain", 415},
		{"POST", "/api/v1/projects", `{"name":"ok","canonicalUrl":"https://e.test","unknown":true}`, "", "localhost", "application/json", 400},
		{"POST", "/api/v1/projects", `{"name":"ok","canonicalUrl":"https://e.test"} {}`, "", "localhost", "application/json", 400},
		{"POST", "/api/v1/projects", `null`, "", "localhost", "application/json", 400},
		{"GET", "/api/v1/projects?page=-1", "", "", "localhost", "", 422},
		{"GET", "/api/v1/projects/missing", "", "", "localhost", "", 404},
		{"GET", "/api/v1/missing", "", "", "localhost", "", 404},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, "http://localhost"+tc.path, bytes.NewBufferString(tc.body))
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Content-Type", tc.typ)
		r.Header.Set("Idempotency-Key", "test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s %s host=%s body=%s got %d want %d: %s", tc.method, tc.path, tc.host, tc.body, w.Code, tc.want, w.Body)
		}
	}
}

func TestGuardRequiresAccessTokenAndRejectsCrossSiteWithoutOrigin(t *testing.T) {
	_, db := testServerWithDB(t)
	const token = "0123456789abcdef0123456789abcdef"
	protected := New(db, Config{AccessToken: token})

	for name, tc := range map[string]struct {
		authorization string
		want          int
	}{
		"missing": {"", http.StatusUnauthorized},
		"wrong":   {"Bearer wrong", http.StatusUnauthorized},
		"exact":   {"Bearer " + token, http.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://localhost/healthz", nil)
			request.Header.Set("Authorization", tc.authorization)
			response := httptest.NewRecorder()
			protected.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body)
			}
		})
	}

	crossSite := httptest.NewRequest(http.MethodPost, "http://localhost/api/v1/projects", strings.NewReader(`{"name":"cross-site","canonicalUrl":"https://example.com"}`))
	crossSite.Header.Set("Content-Type", "application/json")
	crossSite.Header.Set("Idempotency-Key", "cross-site")
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	response := httptest.NewRecorder()
	New(db, Config{}).ServeHTTP(response, crossSite)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-site status=%d body=%s", response.Code, response.Body)
	}
}

func TestCreateReturnsRevisionAndLimitsPayload(t *testing.T) {
	h := testServer(t)
	created := call(h, "POST", "/api/v1/projects", `{"name":"v","canonicalUrl":"https://version.test"}`, "version-intent", "")
	if created.Header().Get("ETag") != `"1"` {
		t.Fatalf("missing creation revision: %v", created.Header())
	}
	replay := call(h, "POST", "/api/v1/projects", `{"name":"v","canonicalUrl":"https://version.test"}`, "version-intent", "")
	if replay.Header().Get("ETag") != created.Header().Get("ETag") {
		t.Fatal("replay changed revision")
	}
	oversized := `{"name":"x","canonicalUrl":"https://limit.test","summary":"` + string(bytes.Repeat([]byte("a"), 1<<20)) + `"}`
	result := call(h, "POST", "/api/v1/projects", oversized, "oversized", "")
	if result.Code != 413 {
		t.Fatalf("oversized: %d", result.Code)
	}
}

func TestSecurityHeadersApplyToEveryResponse(t *testing.T) {
	h := testServer(t)
	response := call(h, "GET", "/healthz", "", "", "")
	for name, want := range map[string]string{
		"Content-Security-Policy": "frame-ancestors 'none'",
		"Permissions-Policy":      "camera=(), microphone=(), geolocation=()",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
	} {
		if got := response.Header().Get(name); !strings.Contains(got, want) {
			t.Errorf("%s=%q, want it to contain %q", name, got, want)
		}
	}
}

func TestEvidenceHTTPIsImmutablePrivateByDefaultAndRetrySafe(t *testing.T) {
	h := testServer(t)
	projectResponse := call(h, "POST", "/api/v1/projects", `{"name":"证据项目","canonicalUrl":"https://evidence.test"}`, "evidence-project", "")
	var project struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(projectResponse.Body.Bytes(), &project); err != nil || projectResponse.Code != 201 {
		t.Fatalf("project: %d %s %v", projectResponse.Code, projectResponse.Body, err)
	}
	path := "/api/v1/projects/" + project.Data.ID + "/evidence"
	body := `{"url":"https://evidence.test/pricing?utm_source=note","title":"定价页","excerpt":"官方页面列出了每月套餐。","kind":"OFFICIAL_PRICING"}`
	created := call(h, "POST", path, body, "evidence-intent", "")
	if created.Code != 201 {
		t.Fatalf("create evidence: %d %s", created.Code, created.Body)
	}
	replayed := call(h, "POST", path, body, "evidence-intent", "")
	if replayed.Code != 201 || replayed.Body.String() != created.Body.String() {
		t.Fatalf("replay differs: %d %s", replayed.Code, replayed.Body)
	}
	var item struct {
		Data struct {
			URL            string   `json:"url"`
			IsPublic       bool     `json:"isPublic"`
			ObservationIDs []string `json:"observationIds"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil || item.Data.IsPublic || item.Data.URL != "https://evidence.test/pricing" || item.Data.ObservationIDs == nil {
		t.Fatalf("created evidence: %+v %v", item.Data, err)
	}
	listed := call(h, "GET", path+"?page=1&pageSize=10", "", "", "")
	var page struct {
		Data       []json.RawMessage `json:"data"`
		Pagination struct {
			TotalItems int `json:"totalItems"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &page); err != nil || listed.Code != 200 || len(page.Data) != 1 || page.Pagination.TotalItems != 1 {
		t.Fatalf("list evidence: %d %s %v", listed.Code, listed.Body, err)
	}
	metric := call(h, "POST", path, `{"url":"https://evidence.test/stars","title":"stars","excerpt":"999","kind":"METRIC_OBSERVATION"}`, "fake-metric", "")
	if metric.Code != 422 {
		t.Fatalf("manual metric accepted: %d %s", metric.Code, metric.Body)
	}
	forgedTime := call(h, "POST", path, `{"url":"https://evidence.test/x","title":"x","excerpt":"x","kind":"PUBLIC_EXCERPT","collectedAt":"2020-01-01T00:00:00Z"}`, "forged-time", "")
	if forgedTime.Code != 400 {
		t.Fatalf("client collectedAt accepted: %d %s", forgedTime.Code, forgedTime.Body)
	}
}

func TestCatalogSurvivesApplicationRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "radar.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	handler := New(db, Config{})
	created := call(handler, "POST", "/api/v1/projects", `{"name":"Restart Proof","canonicalUrl":"https://restart.test"}`, "restart-proof", "")
	if created.Code != http.StatusCreated {
		t.Fatalf("create before restart: %d %s", created.Code, created.Body)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	handler = New(db, Config{})
	listed := call(handler, "GET", "/api/v1/projects?q=Restart", "", "", "")
	var page struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if err = json.Unmarshal(listed.Body.Bytes(), &page); err != nil || listed.Code != http.StatusOK || len(page.Data) != 1 || page.Data[0].Name != "Restart Proof" {
		t.Fatalf("after restart: %d %s %v", listed.Code, listed.Body, err)
	}
}

func TestSourceAndObservationHTTPFollowCurrentAssociation(t *testing.T) {
	handler, db := testServerWithDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	var first, second catalog.Project
	var source catalog.SourceRecord
	err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		first, _, err = catalog.CreateProject(ctx, tx, catalog.CreateProjectInput{Name: "First", CanonicalURL: "https://first-http.test"}, now)
		if err != nil {
			return err
		}
		second, _, err = catalog.CreateProject(ctx, tx, catalog.CreateProjectInput{Name: "Second", CanonicalURL: "https://second-http.test"}, now)
		if err != nil {
			return err
		}
		source, _, err = catalog.UpsertSource(ctx, tx, catalog.UpsertSourceInput{
			ProjectID: first.ID, Source: "HACKER_NEWS", ExternalID: "item:808", RecordType: "DISCUSSION",
			URL: "https://news.ycombinator.com/item?id=808", Title: "Show HN", FetchedAt: now,
		})
		if err != nil {
			return err
		}
		_, err = catalog.AppendObservations(ctx, tx, "run-http", source.ID, []catalog.ObservationInput{{
			Metric: "POINTS", Basis: "CUMULATIVE", Value: 18, ObservedAt: now, FetchedAt: now,
		}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sources := call(handler, "GET", "/api/v1/projects/"+first.ID+"/sources", "", "", "")
	if sources.Code != http.StatusOK || !strings.Contains(sources.Body.String(), source.ID) {
		t.Fatalf("sources: %d %s", sources.Code, sources.Body)
	}
	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)
	observationsPath := "/api/v1/projects/" + first.ID + "/observations?from=" + from + "&to=" + to
	observations := call(handler, "GET", observationsPath, "", "", "")
	if observations.Code != http.StatusOK || !strings.Contains(observations.Body.String(), `"value":18`) {
		t.Fatalf("observations: %d %s", observations.Code, observations.Body)
	}
	missingRevision := call(handler, "PATCH", "/api/v1/source-records/"+source.ID, `{"projectId":"`+second.ID+`","reason":"纠正项目归属"}`, "", "")
	if missingRevision.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing revision: %d %s", missingRevision.Code, missingRevision.Body)
	}
	moved := call(handler, "PATCH", "/api/v1/source-records/"+source.ID, `{"projectId":"`+second.ID+`","reason":"纠正项目归属"}`, "", `"1"`)
	if moved.Code != http.StatusOK || moved.Header().Get("ETag") != `"2"` {
		t.Fatalf("move source: %d %s", moved.Code, moved.Body)
	}
	oldSources := call(handler, "GET", "/api/v1/projects/"+first.ID+"/sources", "", "", "")
	newSources := call(handler, "GET", "/api/v1/projects/"+second.ID+"/sources", "", "", "")
	if strings.Contains(oldSources.Body.String(), source.ID) || !strings.Contains(newSources.Body.String(), source.ID) {
		t.Fatalf("association lists old=%s new=%s", oldSources.Body, newSources.Body)
	}
	movedObservations := call(handler, "GET", "/api/v1/projects/"+second.ID+"/observations?from="+from+"&to="+to, "", "", "")
	if movedObservations.Code != http.StatusOK || !strings.Contains(movedObservations.Body.String(), `"value":18`) {
		t.Fatalf("moved observations: %d %s", movedObservations.Code, movedObservations.Body)
	}
	tooWide := call(handler, "GET", "/api/v1/projects/"+second.ID+"/observations?from=2026-01-01T00:00:00Z&to=2026-10-14T00:00:00Z", "", "", "")
	if tooWide.Code != 422 {
		t.Fatalf("wide window accepted: %d %s", tooWide.Code, tooWide.Body)
	}
}

func TestSourceSettingsAndIngestionRunHTTPContracts(t *testing.T) {
	handler := testServer(t)

	sources := call(handler, http.MethodGet, "/api/v1/sources", "", "", "")
	if sources.Code != http.StatusOK {
		t.Fatalf("list sources: %d %s", sources.Code, sources.Body)
	}
	var sourcePage struct {
		Data []struct {
			Source       string `json:"source"`
			Availability string `json:"availability"`
			Config       struct {
				IsEnabled        bool     `json:"isEnabled"`
				Keywords         []string `json:"keywords"`
				TrackedSourceIDs []string `json:"trackedSourceRecordIds"`
			} `json:"config"`
		} `json:"data"`
		Pagination struct {
			TotalItems int `json:"totalItems"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(sources.Body.Bytes(), &sourcePage); err != nil || len(sourcePage.Data) != 3 || sourcePage.Pagination.TotalItems != 3 {
		t.Fatalf("source response: %+v err=%v", sourcePage, err)
	}
	if sourcePage.Data[0].Source != "GITHUB" || !sourcePage.Data[0].Config.IsEnabled || sourcePage.Data[0].Config.Keywords == nil || sourcePage.Data[0].Config.TrackedSourceIDs == nil {
		t.Fatalf("github source shape: %+v", sourcePage.Data[0])
	}
	if sourcePage.Data[2].Availability != "DISABLED" {
		t.Fatalf("product hunt availability: %+v", sourcePage.Data[2])
	}

	missingRevision := call(handler, http.MethodPatch, "/api/v1/sources/GITHUB", `{"intervalMinutes":30}`, "", "")
	if missingRevision.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing source revision: %d %s", missingRevision.Code, missingRevision.Body)
	}
	crossSource := call(handler, http.MethodPatch, "/api/v1/sources/GITHUB", `{"itemsPerFeed":10}`, "", `"1"`)
	if crossSource.Code != http.StatusUnprocessableEntity {
		t.Fatalf("cross source field: %d %s", crossSource.Code, crossSource.Body)
	}
	updated := call(handler, http.MethodPatch, "/api/v1/sources/GITHUB", `{"intervalMinutes":30}`, "", `"1"`)
	if updated.Code != http.StatusOK || updated.Header().Get("ETag") != `"2"` || !strings.Contains(updated.Body.String(), `"intervalMinutes":30`) {
		t.Fatalf("updated source: %d %s %v", updated.Code, updated.Body, updated.Header())
	}

	body := `{"source":"GITHUB"}`
	created := call(handler, http.MethodPost, "/api/v1/ingestion-runs", body, "collect-github", "")
	if created.Code != http.StatusAccepted || created.Header().Get("Location") == "" {
		t.Fatalf("create run: %d %s %v", created.Code, created.Body, created.Header())
	}
	replayed := call(handler, http.MethodPost, "/api/v1/ingestion-runs", body, "collect-github", "")
	if replayed.Code != http.StatusAccepted || replayed.Body.String() != created.Body.String() || replayed.Header().Get("Location") != created.Header().Get("Location") {
		t.Fatalf("run replay: %d %s %v", replayed.Code, replayed.Body, replayed.Header())
	}
	mismatch := call(handler, http.MethodPost, "/api/v1/ingestion-runs", `{"source":"HACKER_NEWS"}`, "collect-github", "")
	if mismatch.Code != http.StatusUnprocessableEntity || !strings.Contains(mismatch.Body.String(), `"IDEMPOTENCY_MISMATCH"`) {
		t.Fatalf("run mismatch: %d %s", mismatch.Code, mismatch.Body)
	}
	busy := call(handler, http.MethodPost, "/api/v1/ingestion-runs", body, "new-github-intent", "")
	if busy.Code != http.StatusConflict || !strings.Contains(busy.Body.String(), `"SOURCE_BUSY"`) {
		t.Fatalf("source busy: %d %s", busy.Code, busy.Body)
	}

	location := created.Header().Get("Location")
	read := call(handler, http.MethodGet, location, "", "", "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"status":"QUEUED"`) || !strings.Contains(read.Body.String(), `"configRevision":2`) {
		t.Fatalf("read run: %d %s", read.Code, read.Body)
	}
	listed := call(handler, http.MethodGet, "/api/v1/ingestion-runs?source=GITHUB&status=QUEUED", "", "", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"totalItems":1`) {
		t.Fatalf("list runs: %d %s", listed.Code, listed.Body)
	}
	invalidFilter := call(handler, http.MethodGet, "/api/v1/ingestion-runs?status=SUCCESS", "", "", "")
	if invalidFilter.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid run filter: %d %s", invalidFilter.Code, invalidFilter.Body)
	}
}

func TestConcurrentIngestionRunReplayCreatesOneTask(t *testing.T) {
	handler := testServer(t)
	const attempts = 8
	responses := make(chan *httptest.ResponseRecorder, attempts)
	var group sync.WaitGroup
	for range attempts {
		group.Add(1)
		go func() {
			defer group.Done()
			responses <- call(handler, http.MethodPost, "/api/v1/ingestion-runs", `{"source":"HACKER_NEWS"}`, "same-collection-intent", "")
		}()
	}
	group.Wait()
	close(responses)
	var firstBody, firstLocation string
	for item := range responses {
		if item.Code == http.StatusConflict {
			if !strings.Contains(item.Body.String(), `"IDEMPOTENCY_IN_PROGRESS"`) {
				t.Fatalf("unexpected conflict: %s", item.Body)
			}
			continue
		}
		if item.Code != http.StatusAccepted {
			t.Fatalf("concurrent create: %d %s", item.Code, item.Body)
		}
		if firstBody == "" {
			firstBody = item.Body.String()
			firstLocation = item.Header().Get("Location")
		}
		if item.Body.String() != firstBody || item.Header().Get("Location") != firstLocation {
			t.Fatalf("concurrent replay differs: %s %v", item.Body, item.Header())
		}
	}
	if firstBody == "" {
		t.Fatal("no concurrent request created the task")
	}
	replay := call(handler, http.MethodPost, "/api/v1/ingestion-runs", `{"source":"HACKER_NEWS"}`, "same-collection-intent", "")
	if replay.Code != http.StatusAccepted || replay.Body.String() != firstBody || replay.Header().Get("Location") != firstLocation {
		t.Fatalf("completed replay: %d %s %v", replay.Code, replay.Body, replay.Header())
	}
	listed := call(handler, http.MethodGet, "/api/v1/ingestion-runs?source=HACKER_NEWS", "", "", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"totalItems":1`) {
		t.Fatalf("concurrent task count: %d %s", listed.Code, listed.Body)
	}
}

func TestNewIngestionRunIsQueuedOnceForRuntime(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var queued []ingestion.IngestionRun
	handler := New(db, Config{QueueIngestionRun: func(run ingestion.IngestionRun) error {
		queued = append(queued, run)
		return nil
	}})
	body := `{"source":"HACKER_NEWS"}`
	created := call(handler, http.MethodPost, "/api/v1/ingestion-runs", body, "runtime-queue", "")
	replayed := call(handler, http.MethodPost, "/api/v1/ingestion-runs", body, "runtime-queue", "")
	if created.Code != http.StatusAccepted || replayed.Code != http.StatusAccepted || len(queued) != 1 || queued[0].Source != ingestion.HackerNews || queued[0].Status != ingestion.StatusQueued {
		t.Fatalf("created=%d replayed=%d queued=%+v", created.Code, replayed.Code, queued)
	}
}

func TestManualGitHubRunReturnsRateLimitBeforeQueueing(t *testing.T) {
	handler, db := testServerWithDB(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	var existing ingestion.IngestionRun
	if err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var createErr error
		existing, createErr = ingestion.CreateRun(ctx, tx, ingestion.CreateRunInput{
			Source: ingestion.GitHub, Trigger: ingestion.TriggerManual,
		}, now)
		return createErr
	}); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if _, err := ingestion.ClaimRequestBudget(ctx, db, existing.ID, ingestion.GitHub, ingestion.RequestSearch, ingestion.BudgetPolicy{HourlyLimit: 60, SearchMinuteLimit: 10}, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE ingestion_runs SET status='INTERRUPTED',finished_at=? WHERE id=?`, now.Format(time.RFC3339Nano), existing.ID); err != nil {
		t.Fatal(err)
	}

	response := call(handler, http.MethodPost, "/api/v1/ingestion-runs", `{"source":"GITHUB"}`, "budget-exhausted", "")
	if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), `"SOURCE_RATE_LIMITED"`) {
		t.Fatalf("rate limit response: %d %s", response.Code, response.Body)
	}
	if response.Header().Get("Retry-After") == "" {
		t.Fatalf("missing Retry-After: %v", response.Header())
	}
	var queued, claims int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM ingestion_runs WHERE status='QUEUED'`).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("queued=%d err=%v", queued, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM request_budget_claims WHERE source='GITHUB'`).Scan(&claims); err != nil || claims != 10 {
		t.Fatalf("claims=%d err=%v", claims, err)
	}
}

func TestRadarHTTPValidatesWindowAndAsOf(t *testing.T) {
	handler := testServer(t)
	empty := call(handler, http.MethodGet, "/api/v1/radar?window=7d", "", "", "")
	if empty.Code != http.StatusOK || !strings.Contains(empty.Body.String(), `"totalItems":0`) {
		t.Fatalf("empty radar: %d %s", empty.Code, empty.Body)
	}
	invalidWindow := call(handler, http.MethodGet, "/api/v1/radar?window=30d", "", "", "")
	if invalidWindow.Code != http.StatusUnprocessableEntity || !strings.Contains(invalidWindow.Body.String(), `"window"`) {
		t.Fatalf("invalid window: %d %s", invalidWindow.Code, invalidWindow.Body)
	}
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	invalidTime := call(handler, http.MethodGet, "/api/v1/radar?asOf="+future, "", "", "")
	if invalidTime.Code != http.StatusUnprocessableEntity || !strings.Contains(invalidTime.Body.String(), `"asOf"`) {
		t.Fatalf("future asOf: %d %s", invalidTime.Code, invalidTime.Body)
	}
	missing := call(handler, http.MethodGet, "/api/v1/projects/missing/trend", "", "", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing trend: %d %s", missing.Code, missing.Body)
	}
}

func TestRadarHTTPOrdersAccumulatingProjectsByCurrentAttention(t *testing.T) {
	handler, db := testServerWithDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	seed := func(name string, value int64, firstSeen time.Time) {
		t.Helper()
		err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
			project, _, err := catalog.CreateProject(ctx, tx, catalog.CreateProjectInput{Name: name, CanonicalURL: "https://github.com/acme/" + name}, firstSeen)
			if err != nil {
				return err
			}
			source, _, err := catalog.UpsertSource(ctx, tx, catalog.UpsertSourceInput{
				ProjectID: project.ID, Source: "GITHUB", ExternalID: "repo:" + name, RecordType: "PROJECT",
				URL: "https://api.github.com/repos/acme/" + name, Title: "acme/" + name, FetchedAt: now,
			})
			if err != nil {
				return err
			}
			_, err = catalog.AppendObservations(ctx, tx, "run-attention", source.ID, []catalog.ObservationInput{
				{Metric: "STARS", Basis: "CUMULATIVE", Value: value, ObservedAt: now, FetchedAt: now},
			})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	seed("quiet-newest", 0, now.Add(-time.Minute))
	seed("popular-oldest", 173000, now.Add(-time.Hour))
	seed("middle", 400, now.Add(-30*time.Minute))

	response := call(handler, http.MethodGet, "/api/v1/radar?asOf="+now.Add(time.Minute).Format(time.RFC3339Nano), "", "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("radar: %d %s", response.Code, response.Body)
	}
	var body struct {
		Data []struct {
			Project struct {
				Name string `json:"name"`
			} `json:"project"`
			Trend struct {
				Score     *float64 `json:"score"`
				Attention struct {
					Index   *float64 `json:"index"`
					Metrics []struct {
						Source string `json:"source"`
						Metric string `json:"metric"`
						Value  int64  `json:"value"`
					} `json:"metrics"`
				} `json:"attention"`
			} `json:"trend"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := []string{"popular-oldest", "middle", "quiet-newest"}
	for index, item := range body.Data {
		if item.Project.Name != want[index] || item.Trend.Score != nil {
			t.Fatalf("order[%d]=%s score=%v want=%s", index, item.Project.Name, item.Trend.Score, want[index])
		}
	}
	top := body.Data[0].Trend.Attention
	if top.Index == nil || len(top.Metrics) != 1 || top.Metrics[0].Source != "GITHUB" || top.Metrics[0].Metric != "STARS" || top.Metrics[0].Value != 173000 {
		t.Fatalf("attention=%+v", top)
	}
}
