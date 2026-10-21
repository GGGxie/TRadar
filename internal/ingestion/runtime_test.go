package ingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestRuntimeExecutesQueuedRunToTerminalState(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 15, 17, 0, 0, 0, time.UTC)
	server := emptyHackerNewsServer(t)
	defer server.Close()
	target, _ := url.Parse(server.URL)
	runtime := NewRuntime(db, RuntimeConfig{
		HackerNewsClient: &http.Client{Transport: &rewriteTransport{target: target}},
		Now:              func() time.Time { return now },
		ScheduleInterval: time.Hour,
	})
	ctx, cancel := context.WithCancel(context.Background())
	runtime.Start(ctx)
	t.Cleanup(func() { cancel(); runtime.Wait() })
	run := createQueuedRun(t, db, HackerNews, now)
	if err := runtime.Enqueue(run); err != nil {
		t.Fatal(err)
	}
	stored := waitForTerminalRun(t, db, run.ID)
	if stored.Status != StatusSucceededEmpty || stored.RequestsMade != 3 {
		t.Fatalf("runtime run=%+v", stored)
	}
}

func TestRuntimeSchedulesDueSourceOnceAndExecutesIt(t *testing.T) {
	db := openTestDB(t)
	configuredAt := time.Date(2026, 10, 15, 18, 0, 0, 0, time.UTC)
	enableSchedule(t, db, HackerNews, 1, configuredAt)
	now := configuredAt.Add(72 * time.Hour)
	server := emptyHackerNewsServer(t)
	defer server.Close()
	target, _ := url.Parse(server.URL)
	runtime := NewRuntime(db, RuntimeConfig{
		HackerNewsClient: &http.Client{Transport: &rewriteTransport{target: target}},
		Now:              func() time.Time { return now },
		ScheduleInterval: 10 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	runtime.Start(ctx)
	t.Cleanup(func() { cancel(); runtime.Wait() })
	deadline := time.Now().Add(2 * time.Second)
	for {
		runs, _, err := ListRuns(context.Background(), db, RunFilter{Source: HackerNews, Page: 1, PageSize: 20})
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) == 1 && isTerminal(runs[0].Status) {
			if runs[0].Trigger != TriggerScheduled || runs[0].Status != StatusSucceededEmpty {
				t.Fatalf("scheduled run=%+v", runs[0])
			}
			break
		}
		if len(runs) > 1 {
			t.Fatalf("scheduler created %d runs", len(runs))
		}
		if time.Now().After(deadline) {
			t.Fatalf("scheduled run did not finish: %+v", runs)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func emptyHackerNewsServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path == "" {
			t.Errorf("unexpected HN request: %s %s", request.Method, request.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]int{})
	}))
}

func waitForTerminalRun(t *testing.T, db *sql.DB, id string) IngestionRun {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		run, err := GetRun(context.Background(), db, id)
		if err != nil {
			t.Fatal(err)
		}
		if isTerminal(run.Status) {
			return run
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not finish: %+v", run)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
