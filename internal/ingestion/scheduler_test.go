package ingestion

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/storage"
)

func TestSchedulerCatchesUpOnceAndKeepsSourcesIndependent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	configuredAt := time.Date(2026, 10, 15, 11, 0, 0, 0, time.UTC)
	enableSchedule(t, db, GitHub, 1, configuredAt)
	enableSchedule(t, db, HackerNews, 1, configuredAt)
	scheduler := NewScheduler(db, Credentials{})
	afterDowntime := configuredAt.Add(72 * time.Hour)
	runs, err := scheduler.Tick(ctx, afterDowntime)
	if err != nil || len(runs) != 2 {
		t.Fatalf("catch-up runs=%+v err=%v", runs, err)
	}
	for _, run := range runs {
		if run.Trigger != TriggerScheduled || run.Status != StatusQueued {
			t.Fatalf("scheduled run: %+v", run)
		}
	}
	again, err := scheduler.Tick(ctx, afterDowntime)
	if err != nil || len(again) != 0 {
		t.Fatalf("same due instant duplicated: %+v err=%v", again, err)
	}
	for _, source := range []RemoteSource{GitHub, HackerNews} {
		config, configErr := GetSourceConfig(ctx, db, source)
		want := catalog.Timestamp(afterDowntime.Add(time.Hour))
		if configErr != nil || config.NextScheduledAt == nil || *config.NextScheduledAt != want {
			t.Fatalf("%s next schedule=%+v want=%s err=%v", source, config.NextScheduledAt, want, configErr)
		}
	}
}

func TestSchedulerMergesDueTriggerWhileSourceIsBusy(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	configuredAt := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	enableSchedule(t, db, GitHub, 1, configuredAt)
	manual := createQueuedRun(t, db, GitHub, configuredAt.Add(10*time.Minute))
	dueAt := configuredAt.Add(time.Hour)
	runs, err := NewScheduler(db, Credentials{}).Tick(ctx, dueAt)
	if err != nil || len(runs) != 0 {
		t.Fatalf("busy schedule created run: %+v err=%v", runs, err)
	}
	stored, err := GetRun(ctx, db, manual.ID)
	if err != nil || stored.Status != StatusQueued {
		t.Fatalf("manual run changed: %+v err=%v", stored, err)
	}
	config, err := GetSourceConfig(ctx, db, GitHub)
	if err != nil || config.NextScheduledAt == nil || *config.NextScheduledAt != catalog.Timestamp(dueAt.Add(time.Hour)) {
		t.Fatalf("busy next schedule: %+v err=%v", config.NextScheduledAt, err)
	}
}

func TestSchedulerDoesNotRunBeforePersistedRetryTime(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	configuredAt := time.Date(2026, 10, 15, 12, 30, 0, 0, time.UTC)
	enableSchedule(t, db, GitHub, 1, configuredAt)
	retryAt := configuredAt.Add(3 * time.Hour)
	setNextRetry(t, db, GitHub, retryAt)
	runs, err := NewScheduler(db, Credentials{}).Tick(ctx, configuredAt.Add(time.Hour))
	if err != nil || len(runs) != 0 {
		t.Fatalf("run created before retry time: %+v err=%v", runs, err)
	}
	config, err := GetSourceConfig(ctx, db, GitHub)
	if err != nil || config.NextScheduledAt == nil || *config.NextScheduledAt != catalog.Timestamp(retryAt) {
		t.Fatalf("next schedule=%v err=%v", config.NextScheduledAt, err)
	}
	runs, err = NewScheduler(db, Credentials{}).Tick(ctx, retryAt)
	if err != nil || len(runs) != 1 {
		t.Fatalf("retry due runs=%+v err=%v", runs, err)
	}
}

func TestConcurrentSchedulerTicksCreateOneIntent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	configuredAt := time.Date(2026, 10, 15, 13, 0, 0, 0, time.UTC)
	enableSchedule(t, db, HackerNews, 1, configuredAt)
	scheduler := NewScheduler(db, Credentials{})
	dueAt := configuredAt.Add(time.Hour)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := scheduler.Tick(ctx, dueAt); err != nil {
				t.Errorf("tick: %v", err)
			}
		}()
	}
	group.Wait()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM ingestion_runs WHERE source='HACKER_NEWS' AND trigger='SCHEDULED'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("scheduled intents=%d err=%v", count, err)
	}
}

func setNextRetry(t *testing.T, db *sql.DB, source RemoteSource, retryAt time.Time) {
	t.Helper()
	if _, err := db.Exec(`UPDATE source_configs SET next_retry_at=? WHERE source=?`, retryAt.UTC().Format(time.RFC3339Nano), source); err != nil {
		t.Fatal(err)
	}
}

func finishQueuedRun(t *testing.T, db *sql.DB, id string, now time.Time) {
	t.Helper()
	err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		if _, startErr := StartRun(context.Background(), tx, id, now); startErr != nil {
			return startErr
		}
		_, finishErr := FinishRun(context.Background(), tx, id, FinishRunInput{Status: StatusSucceededEmpty}, now)
		return finishErr
	})
	if err != nil {
		t.Fatal(err)
	}
}
