package ingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"tradar/internal/storage"
)

func TestProductHuntExecutorImportsPartialPagesAndMarksTruncation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC)
	run := createProductHuntRun(t, db, now, 2)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests++
		if request.Method != http.MethodPost || request.URL.Path != "/v2/api/graphql" || request.Header.Get("Authorization") != "Bearer ph-secret" || request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request method=%s path=%s headers=%v", request.Method, request.URL.Path, request.Header)
		}
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body.Query, "query ProductHuntPosts") || body.Variables["postedAfter"] != "2026-10-08T15:00:00.000000000Z" || body.Variables["postedBefore"] != "2026-10-15T15:00:00.000000000Z" || body.Variables["first"] != float64(20) {
			t.Errorf("graphql body=%+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Rate-Limit-Remaining", "6200")
		w.Header().Set("X-Rate-Limit-Reset", "900")
		if requests == 1 {
			json.NewEncoder(w).Encode(map[string]any{
				"data":   fixtureProductHuntPage(true, "cursor-1", productHuntNode("ph-1", "First", 42, 7)),
				"errors": []any{map[string]any{"message": "private provider detail", "path": []any{"posts", "edges", 1}}},
			})
			return
		}
		if body.Variables["after"] != "cursor-1" {
			t.Errorf("second cursor=%v", body.Variables["after"])
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": fixtureProductHuntPage(true, "cursor-2", productHuntNode("ph-2", "Second", 21, 3)),
		})
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	transport := &rewriteTransport{target: target}
	executor := NewProductHuntExecutor(db, &http.Client{Transport: transport}, "ph-secret", func() time.Time { return now })
	if err := NewRunner(db, func() time.Time { return now }).Run(ctx, run.ID, executor); err != nil {
		t.Fatal(err)
	}
	stored, err := GetRun(ctx, db, run.ID)
	if err != nil || stored.Status != StatusPartial || stored.RequestsMade != 2 || stored.AcceptedItems != 2 || stored.ObservationsWritten != 4 || !stored.IsTruncated || len(stored.Errors) != 1 || stored.Errors[0].Code != "GRAPHQL_PARTIAL" || strings.Contains(stored.Errors[0].Message, "private") {
		t.Fatalf("product hunt run=%+v err=%v", stored, err)
	}
	assertTableCount(t, db, "projects", 2)
	assertTableCount(t, db, "source_records", 2)
	var votes, comments int
	if err = db.QueryRow(`SELECT sum(CASE WHEN metric='VOTES' THEN value ELSE 0 END),sum(CASE WHEN metric='COMMENTS' THEN value ELSE 0 END) FROM observations WHERE run_id=?`, run.ID).Scan(&votes, &comments); err != nil || votes != 63 || comments != 10 {
		t.Fatalf("metrics votes=%d comments=%d err=%v", votes, comments, err)
	}
	budget, active, err := readEffectiveBudgetState(ctx, db, ProductHunt, now)
	if err != nil || !active || budget.RemainingRequests != 6200 || !budget.ResetAt.Equal(now.Add(15*time.Minute)) {
		t.Fatalf("complexity budget=%+v active=%v err=%v", budget, active, err)
	}
	for _, request := range transport.requests() {
		if request.URL.Scheme != "https" || request.URL.Host != "api.producthunt.com" {
			t.Fatalf("non-fixed Product Hunt target: %s", request.URL)
		}
	}
}

func TestProductHuntExecutorRejectsGraphQLFailureWithoutInventingData(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 15, 16, 0, 0, 0, time.UTC)
	run := createProductHuntRun(t, db, now, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": nil, "errors": []any{map[string]any{"message": "resolver failed with secret"}}})
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	executor := NewProductHuntExecutor(db, &http.Client{Transport: &rewriteTransport{target: target}}, "ph-secret", func() time.Time { return now })
	if err := NewRunner(db, func() time.Time { return now }).Run(context.Background(), run.ID, executor); err != nil {
		t.Fatal(err)
	}
	stored, err := GetRun(context.Background(), db, run.ID)
	if err != nil || stored.Status != StatusFailed || len(stored.Errors) != 1 || stored.Errors[0].Code != "GRAPHQL_ERROR" || strings.Contains(stored.Errors[0].Message, "secret") {
		t.Fatalf("graphql failure=%+v err=%v", stored, err)
	}
	assertTableCount(t, db, "projects", 0)
}

func createProductHuntRun(t *testing.T, db *sql.DB, now time.Time, maxPages int) IngestionRun {
	t.Helper()
	enabled := true
	err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		_, updateErr := UpdateSourceConfig(context.Background(), tx, ProductHunt, 1, SourceConfigPatch{IsEnabled: &enabled, MaxPages: &maxPages}, true, now)
		return updateErr
	})
	if err != nil {
		t.Fatal(err)
	}
	var run IngestionRun
	err = storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var createErr error
		run, createErr = CreateRun(context.Background(), tx, CreateRunInput{Source: ProductHunt, Trigger: TriggerManual, HasCredentials: true}, now)
		return createErr
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func fixtureProductHuntPage(hasNext bool, endCursor string, nodes ...map[string]any) map[string]any {
	edges := make([]map[string]any, 0, len(nodes))
	for index, node := range nodes {
		edges = append(edges, map[string]any{"cursor": endCursor + "-item-" + string(rune('a'+index)), "node": node})
	}
	return map[string]any{"posts": map[string]any{
		"totalCount": len(nodes), "pageInfo": map[string]any{"hasNextPage": hasNext, "endCursor": endCursor}, "edges": edges,
	}}
}

func productHuntNode(id, name string, votes, comments int) map[string]any {
	return map[string]any{
		"id": id, "name": name, "tagline": name + " tagline", "description": name + " description",
		"url":       "https://www.producthunt.com/posts/" + strings.ToLower(name),
		"website":   "https://" + strings.ToLower(name) + ".example/product?ref=producthunt",
		"createdAt": "2026-10-14T08:00:00Z", "featuredAt": "2026-10-15T00:00:00Z",
		"votesCount": votes, "commentsCount": comments,
	}
}
