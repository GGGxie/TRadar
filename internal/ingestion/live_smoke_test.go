package ingestion

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/storage"
)

// TestLiveGitHubAndHackerNewsSmoke is opt-in because it contacts the two
// documented source APIs. It keeps the request count small and writes only to
// a temporary SQLite database.
func TestLiveGitHubAndHackerNewsSmoke(t *testing.T) {
	if os.Getenv("TRADAR_LIVE_SMOKE") != "1" {
		t.Skip("set TRADAR_LIVE_SMOKE=1 to contact the real source APIs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	path := filepath.Join(t.TempDir(), "live-smoke.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	keywords := []string{"agent framework"}
	discoveryDays, candidates := 30, 2
	feeds := []string{"SHOW"}
	itemsPerFeed := 1
	if err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		if _, updateErr := UpdateSourceConfig(ctx, tx, GitHub, 1, SourceConfigPatch{
			Keywords: &keywords, DiscoveryDays: &discoveryDays, CandidatesPerKeyword: &candidates,
		}, false, now); updateErr != nil {
			return updateErr
		}
		_, updateErr := UpdateSourceConfig(ctx, tx, HackerNews, 1, SourceConfigPatch{
			Feeds: &feeds, ItemsPerFeed: &itemsPerFeed,
		}, false, now)
		return updateErr
	}); err != nil {
		db.Close()
		t.Fatal(err)
	}

	github := createLiveRun(t, ctx, db, GitHub, now)
	hackerNews := createLiveRun(t, ctx, db, HackerNews, now)
	runner := NewRunner(db, time.Now)
	if err = runner.Run(ctx, github.ID, NewGitHubExecutor(db, nil, "", time.Now)); err != nil {
		db.Close()
		t.Fatalf("GitHub live run: %v", err)
	}
	if err = runner.Run(ctx, hackerNews.ID, NewHackerNewsExecutor(db, nil, time.Now)); err != nil {
		db.Close()
		t.Fatalf("Hacker News live run: %v", err)
	}
	github = requireLiveRun(t, ctx, db, github.ID)
	hackerNews = requireLiveRun(t, ctx, db, hackerNews.ID)
	for _, run := range []IngestionRun{github, hackerNews} {
		if run.Status != StatusSucceeded && run.Status != StatusPartial {
			db.Close()
			t.Fatalf("%s live status=%s errors=%+v", run.Source, run.Status, run.Errors)
		}
		if run.AcceptedItems < 1 || run.ObservationsWritten < 1 {
			db.Close()
			t.Fatalf("%s imported=%d observations=%d", run.Source, run.AcceptedItems, run.ObservationsWritten)
		}
		t.Logf("%s status=%s requests=%d accepted=%d observations=%d", run.Source, run.Status, run.RequestsMade, run.AcceptedItems, run.ObservationsWritten)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var projectID string
	if err = db.QueryRowContext(ctx, `SELECT project_id FROM source_records WHERE source='GITHUB' ORDER BY fetched_at DESC LIMIT 1`).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	project, err := catalog.GetProject(ctx, db, projectID)
	if err != nil || project.ID != projectID || project.CanonicalURL == "" {
		t.Fatalf("persisted project=%+v err=%v", project, err)
	}
	var sourceCount, observationCount int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM source_records WHERE source IN ('GITHUB','HACKER_NEWS')`).Scan(&sourceCount); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM observations`).Scan(&observationCount); err != nil {
		t.Fatal(err)
	}
	if sourceCount < 2 || observationCount < 2 {
		t.Fatalf("persisted sources=%d observations=%d", sourceCount, observationCount)
	}
}

func createLiveRun(t *testing.T, ctx context.Context, db *sql.DB, source RemoteSource, now time.Time) IngestionRun {
	t.Helper()
	var run IngestionRun
	if err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var createErr error
		run, createErr = CreateRun(ctx, tx, CreateRunInput{Source: source, Trigger: TriggerManual}, now)
		return createErr
	}); err != nil {
		t.Fatal(err)
	}
	return run
}

func requireLiveRun(t *testing.T, ctx context.Context, db *sql.DB, id string) IngestionRun {
	t.Helper()
	run, err := GetRun(ctx, db, id)
	if err != nil {
		t.Fatal(err)
	}
	return run
}
