package ingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/fault"
	"tradar/internal/storage"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestDefaultSourceConfigsAndValidatedUpdates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 4, 0, 0, 0, time.UTC)

	views, total, err := ListSourceViews(ctx, db, SourceViewFilter{Page: 1, PageSize: 20}, Credentials{})
	if err != nil || total != 3 || len(views) != 3 {
		t.Fatalf("default sources: total=%d views=%+v err=%v", total, views, err)
	}
	if views[0].Source != GitHub || !views[0].Config.IsEnabled || views[0].Config.IsScheduleEnabled || views[0].Config.IntervalMinutes != 60 {
		t.Fatalf("github defaults: %+v", views[0])
	}
	if len(views[0].Config.Keywords) != 3 || views[0].Config.DiscoveryDays != 180 || views[0].Config.CandidatesPerKeyword != 10 || views[0].Config.RequestBudgetPerHour != 60 {
		t.Fatalf("github discovery defaults: %+v", views[0].Config)
	}
	if views[1].Source != HackerNews || len(views[1].Config.Feeds) != 3 || views[1].Config.ItemsPerFeed != 20 {
		t.Fatalf("hn defaults: %+v", views[1])
	}
	if views[2].Source != ProductHunt || views[2].Config.IsEnabled || views[2].Availability != AvailabilityDisabled {
		t.Fatalf("product hunt defaults: %+v", views[2])
	}

	interval := 30
	keywords := []string{"agent", "observability"}
	var updated SourceConfig
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var updateErr error
		updated, updateErr = UpdateSourceConfig(ctx, tx, GitHub, 1, SourceConfigPatch{
			IntervalMinutes: &interval,
			Keywords:        &keywords,
		}, false, now)
		return updateErr
	})
	if err != nil || updated.Revision != 2 || updated.IntervalMinutes != 30 || len(updated.Keywords) != 2 {
		t.Fatalf("valid update: %+v err=%v", updated, err)
	}

	budget := 61
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, updateErr := UpdateSourceConfig(ctx, tx, GitHub, 2, SourceConfigPatch{RequestBudgetPerHour: &budget}, false, now)
		return updateErr
	})
	assertFaultCode(t, err, "VALIDATION_ERROR")

	items := 10
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, updateErr := UpdateSourceConfig(ctx, tx, GitHub, 2, SourceConfigPatch{ItemsPerFeed: &items}, false, now)
		return updateErr
	})
	assertFaultCode(t, err, "VALIDATION_ERROR")
}

func TestRunCreationBusyRulesAndRestartRecovery(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 5, 0, 0, 0, time.UTC)
	var first IngestionRun
	err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var createErr error
		first, createErr = CreateRun(ctx, tx, CreateRunInput{Source: GitHub, Trigger: TriggerManual}, now)
		return createErr
	})
	if err != nil || first.Status != StatusQueued || first.ConfigRevision != 1 || first.WindowStart == nil || first.WindowEnd != catalog.Timestamp(now) {
		t.Fatalf("created run: %+v err=%v", first, err)
	}

	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, createErr := CreateRun(ctx, tx, CreateRunInput{Source: GitHub, Trigger: TriggerManual}, now.Add(time.Minute))
		return createErr
	})
	assertFaultCode(t, err, "SOURCE_BUSY")

	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, createErr := CreateRun(ctx, tx, CreateRunInput{Source: ProductHunt, Trigger: TriggerManual}, now)
		return createErr
	})
	assertFaultCode(t, err, "SOURCE_DISABLED")

	enabled := true
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, updateErr := UpdateSourceConfig(ctx, tx, ProductHunt, 1, SourceConfigPatch{IsEnabled: &enabled}, false, now)
		return updateErr
	})
	if err != nil {
		t.Fatal(err)
	}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, createErr := CreateRun(ctx, tx, CreateRunInput{Source: ProductHunt, Trigger: TriggerManual}, now)
		return createErr
	})
	assertFaultCode(t, err, "SOURCE_CREDENTIALS_REQUIRED")

	interruptedAt := now.Add(10 * time.Minute)
	count, err := RecoverInterrupted(ctx, db, interruptedAt)
	if err != nil || count != 1 {
		t.Fatalf("recover: count=%d err=%v", count, err)
	}
	recovered, err := GetRun(ctx, db, first.ID)
	if err != nil || recovered.Status != StatusInterrupted || recovered.FinishedAt == nil || *recovered.FinishedAt != catalog.Timestamp(interruptedAt) {
		t.Fatalf("recovered run: %+v err=%v", recovered, err)
	}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, createErr := CreateRun(ctx, tx, CreateRunInput{Source: GitHub, Trigger: TriggerManual}, interruptedAt)
		return createErr
	})
	if err != nil {
		t.Fatalf("new intent after recovery: %v", err)
	}
}

func TestRecoveryAfterDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "radar.db")
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 5, 30, 0, 0, time.UTC)
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	run := createQueuedRun(t, db, HackerNews, now)
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if changed, recoverErr := RecoverInterrupted(ctx, db, now.Add(time.Minute)); recoverErr != nil || changed != 1 {
		t.Fatalf("restart recovery changed=%d err=%v", changed, recoverErr)
	}
	stored, err := GetRun(ctx, db, run.ID)
	if err != nil || stored.Status != StatusInterrupted {
		t.Fatalf("run after reopen: %+v err=%v", stored, err)
	}
}

func TestBatchDataAndCheckpointCommitOrRollbackTogether(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 6, 0, 0, 0, time.UTC)
	run := createRunningRun(t, db, GitHub, now)
	repository := NewRepository(db)
	forced := errors.New("checkpoint write failed")

	_, err := repository.CommitBatch(ctx, run.ID, Batch{
		Key: "search:agent:page:1", Checkpoint: json.RawMessage(`{"page":1}`),
		Stats: BatchStats{FetchedItems: 1, AcceptedItems: 1, ObservationsWritten: 1},
	}, func(tx *sql.Tx) error {
		project, _, createErr := catalog.CreateProject(ctx, tx, catalog.CreateProjectInput{Name: "Rollback", CanonicalURL: "https://rollback.test"}, now)
		if createErr != nil {
			return createErr
		}
		source, _, createErr := catalog.UpsertSource(ctx, tx, catalog.UpsertSourceInput{
			ProjectID: project.ID, Source: "GITHUB", ExternalID: "repo:rollback", RecordType: "PROJECT",
			URL: "https://api.github.com/repositories/1", TargetURL: stringPointer("https://github.com/acme/rollback"), Title: "acme/rollback", FetchedAt: now,
		})
		if createErr != nil {
			return createErr
		}
		if _, createErr = catalog.AppendObservations(ctx, tx, run.ID, source.ID, []catalog.ObservationInput{{Metric: "STARS", Basis: "CUMULATIVE", Value: 1, ObservedAt: now, FetchedAt: now}}); createErr != nil {
			return createErr
		}
		return forced
	})
	if !errors.Is(err, forced) {
		t.Fatalf("forced failure: %v", err)
	}
	assertTableCount(t, db, "projects", 0)
	assertTableCount(t, db, "observations", 0)
	assertTableCount(t, db, "ingestion_batches", 0)
	assertTableCount(t, db, "ingestion_checkpoints", 0)

	var sourceID string
	batch := Batch{
		Key: "search:agent:page:1", Checkpoint: json.RawMessage(`{"page":1,"processedIds":["repo:1"]}`),
		Stats: BatchStats{FetchedItems: 1, AcceptedItems: 1, ObservationsWritten: 1},
	}
	committed, err := repository.CommitBatch(ctx, run.ID, batch, func(tx *sql.Tx) error {
		project, _, writeErr := catalog.CreateProject(ctx, tx, catalog.CreateProjectInput{Name: "Committed", CanonicalURL: "https://committed.test"}, now)
		if writeErr != nil {
			return writeErr
		}
		source, _, writeErr := catalog.UpsertSource(ctx, tx, catalog.UpsertSourceInput{
			ProjectID: project.ID, Source: "GITHUB", ExternalID: "repo:1", RecordType: "PROJECT",
			URL: "https://api.github.com/repositories/1", TargetURL: stringPointer("https://github.com/acme/committed"), Title: "acme/committed", FetchedAt: now,
		})
		if writeErr != nil {
			return writeErr
		}
		sourceID = source.ID
		_, writeErr = catalog.AppendObservations(ctx, tx, run.ID, source.ID, []catalog.ObservationInput{{Metric: "STARS", Basis: "CUMULATIVE", Value: 7, ObservedAt: now, FetchedAt: now}})
		return writeErr
	})
	if err != nil || !committed || sourceID == "" {
		t.Fatalf("commit: committed=%v source=%q err=%v", committed, sourceID, err)
	}
	called := false
	replayed, err := repository.CommitBatch(ctx, run.ID, batch, func(*sql.Tx) error {
		called = true
		return nil
	})
	if err != nil || replayed || called {
		t.Fatalf("replay: committed=%v called=%v err=%v", replayed, called, err)
	}
	stored, err := GetRun(ctx, db, run.ID)
	if err != nil || stored.AcceptedItems != 1 || stored.ObservationsWritten != 1 {
		t.Fatalf("run counters after replay: %+v err=%v", stored, err)
	}
	checkpoint, err := GetCheckpoint(ctx, db, run.ID)
	if err != nil || checkpoint.BatchKey != batch.Key || string(checkpoint.Value) != string(batch.Checkpoint) {
		t.Fatalf("checkpoint: %+v err=%v", checkpoint, err)
	}
	_, err = repository.CommitBatch(ctx, run.ID, Batch{
		Key: "search:agent:page:2", Checkpoint: json.RawMessage(`{"page":2}`),
		Stats: BatchStats{FetchedItems: 1, AcceptedItems: 1},
	}, func(tx *sql.Tx) error {
		if _, _, writeErr := catalog.CreateProject(ctx, tx, catalog.CreateProjectInput{Name: "Second page", CanonicalURL: "https://second-page.test"}, now); writeErr != nil {
			return writeErr
		}
		return forced
	})
	if !errors.Is(err, forced) {
		t.Fatalf("second page failure: %v", err)
	}
	assertTableCount(t, db, "projects", 1)
	stored, err = GetRun(ctx, db, run.ID)
	if err != nil || stored.AcceptedItems != 1 {
		t.Fatalf("first page changed after second failed: %+v err=%v", stored, err)
	}
	checkpoint, err = GetCheckpoint(ctx, db, run.ID)
	if err != nil || checkpoint.BatchKey != batch.Key {
		t.Fatalf("checkpoint advanced after failed page: %+v err=%v", checkpoint, err)
	}

	different := batch
	different.Checkpoint = json.RawMessage(`{"page":2}`)
	_, err = repository.CommitBatch(ctx, run.ID, different, func(*sql.Tx) error { return nil })
	assertFaultCode(t, err, "IDEMPOTENCY_MISMATCH")
}

func TestRunnerKeepsSourceFailuresIndependent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 7, 0, 0, 0, time.UTC)
	github := createQueuedRun(t, db, GitHub, now)
	hn := createQueuedRun(t, db, HackerNews, now)
	runner := NewRunner(db, func() time.Time { return now.Add(time.Minute) })

	errorsByRun := runner.RunMany(ctx, map[string]Executor{
		github.ID: ExecutorFunc(func(context.Context, IngestionRun, *Repository) (ExecutionResult, error) {
			return ExecutionResult{RequestsMade: 1, Errors: []RunError{{Code: "NETWORK_ERROR", Message: "来源暂时不可达"}}}, errors.New("network down")
		}),
		hn.ID: ExecutorFunc(func(ctx context.Context, run IngestionRun, repository *Repository) (ExecutionResult, error) {
			_, err := repository.CommitBatch(ctx, run.ID, Batch{Key: "top:1", Checkpoint: json.RawMessage(`{"feed":"TOP"}`), Stats: BatchStats{FetchedItems: 1, AcceptedItems: 1}}, func(*sql.Tx) error { return nil })
			return ExecutionResult{RequestsMade: 1}, err
		}),
	})
	if errorsByRun[github.ID] == nil || errorsByRun[hn.ID] != nil {
		t.Fatalf("independent results: %+v", errorsByRun)
	}
	failed, err := GetRun(ctx, db, github.ID)
	if err != nil || failed.Status != StatusFailed || len(failed.Errors) != 1 {
		t.Fatalf("failed source: %+v err=%v", failed, err)
	}
	succeeded, err := GetRun(ctx, db, hn.ID)
	if err != nil || succeeded.Status != StatusSucceeded || succeeded.AcceptedItems != 1 {
		t.Fatalf("successful source: %+v err=%v", succeeded, err)
	}
}

func TestRunnerPersistsAndClearsSourceRetryTime(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 14, 0, 0, 0, time.UTC)
	retryAt := now.Add(20 * time.Minute)
	run := createQueuedRun(t, db, GitHub, now)
	runner := NewRunner(db, func() time.Time { return now })
	if err := runner.Run(ctx, run.ID, ExecutorFunc(func(context.Context, IngestionRun, *Repository) (ExecutionResult, error) {
		return ExecutionResult{NextRetryAt: &retryAt, Errors: []RunError{{Code: "RATE_LIMITED", Message: "来源配额暂不可用"}}}, nil
	})); err != nil {
		t.Fatal(err)
	}
	config, err := GetSourceConfig(ctx, db, GitHub)
	if err != nil || config.NextRetryAt == nil || *config.NextRetryAt != catalog.Timestamp(retryAt) {
		t.Fatalf("stored source retry=%v err=%v", config.NextRetryAt, err)
	}

	now = now.Add(time.Hour)
	next := createQueuedRun(t, db, GitHub, now)
	if err = runner.Run(ctx, next.ID, ExecutorFunc(func(context.Context, IngestionRun, *Repository) (ExecutionResult, error) {
		return ExecutionResult{}, nil
	})); err != nil {
		t.Fatal(err)
	}
	config, err = GetSourceConfig(ctx, db, GitHub)
	if err != nil || config.NextRetryAt != nil {
		t.Fatalf("successful run kept source retry=%v err=%v", config.NextRetryAt, err)
	}
}

func TestRunnerCancelsTaskAtConfiguredDeadline(t *testing.T) {
	db := openTestDB(t)
	run := createQueuedRun(t, db, HackerNews, time.Now())
	runner := NewRunner(db, time.Now)
	runner.taskTimeout = 10 * time.Millisecond
	started := time.Now()
	err := runner.Run(context.Background(), run.ID, ExecutorFunc(func(ctx context.Context, _ IngestionRun, _ *Repository) (ExecutionResult, error) {
		<-ctx.Done()
		return ExecutionResult{}, nil
	}))
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("deadline err=%v elapsed=%v", err, time.Since(started))
	}
	stored, readErr := GetRun(context.Background(), db, run.ID)
	if readErr != nil || stored.Status != StatusFailed || len(stored.Errors) != 1 || stored.Errors[0].Code != "COLLECT_FAILED" {
		t.Fatalf("deadline run=%+v err=%v", stored, readErr)
	}
}

func createQueuedRun(t *testing.T, db *sql.DB, source RemoteSource, now time.Time) IngestionRun {
	t.Helper()
	var run IngestionRun
	err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		run, err = CreateRun(context.Background(), tx, CreateRunInput{Source: source, Trigger: TriggerManual}, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func createRunningRun(t *testing.T, db *sql.DB, source RemoteSource, now time.Time) IngestionRun {
	t.Helper()
	run := createQueuedRun(t, db, source, now)
	err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		run, err = StartRun(context.Background(), tx, run.ID, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func assertFaultCode(t *testing.T, err error, code string) {
	t.Helper()
	var problem *fault.Error
	if !errors.As(err, &problem) || problem.Code != code {
		t.Fatalf("error=%v, want fault %s", err, code)
	}
}

func assertTableCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&got); err != nil || got != want {
		t.Fatalf("%s count=%d want=%d err=%v", table, got, want, err)
	}
}

func stringPointer(value string) *string { return &value }
