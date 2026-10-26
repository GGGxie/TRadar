package brief

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSchedulerRunsAtFirstValidTimeAfterDSTGapAndOnlyOncePerLocalDate(t *testing.T) {
	db := briefDB(t)
	ctx := context.Background()
	zone, localTime := "America/New_York", "02:30"
	if _, err := UpdateSettings(ctx, db, 1, SettingsPatch{TimeZone: &zone, LocalTime: &localTime}); err != nil {
		t.Fatal(err)
	}
	createBriefProject(t, db, "dst", "AI_AGENT", time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
	scheduler := NewScheduler(db)
	before := time.Date(2026, 3, 8, 6, 59, 0, 0, time.UTC) // 01:59, then 03:00.
	if runs, err := scheduler.Tick(ctx, before); err != nil || len(runs) != 0 {
		t.Fatalf("before gap runs=%+v err=%v", runs, err)
	}
	after := time.Date(2026, 3, 8, 7, 1, 0, 0, time.UTC)
	runs, err := scheduler.Tick(ctx, after)
	if err != nil || len(runs) != 1 || runs[0].Date != "2026-03-08" || runs[0].TimeZone != zone {
		t.Fatalf("after gap runs=%+v err=%v", runs, err)
	}
	if _, err = ProcessRun(ctx, db, runs[0].ID, after.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	changedTime := "03:00"
	if _, err = UpdateSettings(ctx, db, 2, SettingsPatch{LocalTime: &changedTime}); err != nil {
		t.Fatal(err)
	}
	if again, err := scheduler.Tick(ctx, after.Add(time.Hour)); err != nil || len(again) != 0 {
		t.Fatalf("same date duplicated after setting change: runs=%+v err=%v", again, err)
	}
}

func TestSchedulerRepeatedHourAndConcurrentTicksCreateOneIntent(t *testing.T) {
	db := briefDB(t)
	ctx := context.Background()
	zone, localTime := "America/New_York", "01:15"
	if _, err := UpdateSettings(ctx, db, 1, SettingsPatch{TimeZone: &zone, LocalTime: &localTime}); err != nil {
		t.Fatal(err)
	}
	createBriefProject(t, db, "repeated-hour", "AI_AGENT", time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC))
	scheduler := NewScheduler(db)
	firstOccurrence := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := scheduler.Tick(ctx, firstOccurrence); err != nil {
				t.Errorf("tick: %v", err)
			}
		}()
	}
	group.Wait()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM brief_runs WHERE date='2026-11-01' AND time_zone=?`, zone).Scan(&count); err != nil || count != 1 {
		t.Fatalf("scheduled intents=%d err=%v", count, err)
	}
	var runID string
	if err := db.QueryRowContext(ctx, `SELECT id FROM brief_runs WHERE date='2026-11-01' AND time_zone=?`, zone).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessRun(ctx, db, runID, firstOccurrence.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	secondOccurrence := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)
	if runs, err := scheduler.Tick(ctx, secondOccurrence); err != nil || len(runs) != 0 {
		t.Fatalf("repeated hour duplicated: runs=%+v err=%v", runs, err)
	}
}

func TestSchedulerWaitsForCatalogDataBeforeClaimingTheDay(t *testing.T) {
	db := briefDB(t)
	ctx := context.Background()
	scheduler := NewScheduler(db)
	// 默认 08:30 Asia/Shanghai；空库启动不应生成「尚无数据」日报，也不占用当天名额。
	morning := time.Date(2026, 10, 23, 1, 0, 0, 0, time.UTC) // 09:00 上海
	if runs, err := scheduler.Tick(ctx, morning); err != nil || len(runs) != 0 {
		t.Fatalf("empty catalog runs=%+v err=%v", runs, err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM brief_runs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("brief runs=%d err=%v", count, err)
	}
	createBriefProject(t, db, "first", "AI_AGENT", morning.Add(time.Hour))
	runs, err := scheduler.Tick(ctx, morning.Add(2*time.Hour))
	if err != nil || len(runs) != 1 || runs[0].Date != "2026-10-23" {
		t.Fatalf("after first project runs=%+v err=%v", runs, err)
	}
}

func TestRecoverInterruptedBriefRuns(t *testing.T) {
	db := briefDB(t)
	now := time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC)
	run := acceptRun(t, db, CreateInput{Date: "2026-10-16", TimeZone: "Asia/Shanghai"}, now)
	changed, err := RecoverInterrupted(context.Background(), db, now.Add(time.Minute))
	if err != nil || changed != 1 {
		t.Fatalf("changed=%d err=%v", changed, err)
	}
	stored, err := GetRun(context.Background(), db, run.ID)
	if err != nil || stored.Status != "INTERRUPTED" || stored.FinishedAt == nil {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}
