package ingestion

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tradar/internal/storage"
)

func TestGitHubBudgetIsClaimedBeforeRequestAndSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "radar.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	run := createQueuedRun(t, db, GitHub, now)
	policy := BudgetPolicy{HourlyLimit: 60, SearchMinuteLimit: 10}
	for index := 0; index < 60; index++ {
		state, claimErr := ClaimRequestBudget(ctx, db, run.ID, GitHub, RequestGeneral, policy, now)
		if claimErr != nil || state.RemainingRequests != 59-index {
			t.Fatalf("claim %d state=%+v err=%v", index+1, state, claimErr)
		}
	}
	_, err = ClaimRequestBudget(ctx, db, run.ID, GitHub, RequestGeneral, policy, now)
	var limited *RateLimitError
	if !errors.As(err, &limited) || !limited.RetryAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("61st request err=%v retry=%v", err, limited)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = ClaimRequestBudget(ctx, db, run.ID, GitHub, RequestGeneral, policy, now.Add(30*time.Minute))
	if !errors.As(err, &limited) {
		t.Fatalf("restart reset budget: %v", err)
	}
	state, err := ClaimRequestBudget(ctx, db, run.ID, GitHub, RequestGeneral, policy, now.Add(time.Hour+time.Nanosecond))
	if err != nil || state.RemainingRequests != 59 {
		t.Fatalf("rolling window did not recover: %+v err=%v", state, err)
	}
}

func TestGitHubSearchBudgetUsesIndependentRollingMinute(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	run := createQueuedRun(t, db, GitHub, now)
	policy := BudgetPolicy{HourlyLimit: 60, SearchMinuteLimit: 10}
	for index := 0; index < 10; index++ {
		if _, err := ClaimRequestBudget(ctx, db, run.ID, GitHub, RequestSearch, policy, now); err != nil {
			t.Fatalf("search claim %d: %v", index+1, err)
		}
	}
	_, err := ClaimRequestBudget(ctx, db, run.ID, GitHub, RequestSearch, policy, now)
	var limited *RateLimitError
	if !errors.As(err, &limited) || !limited.RetryAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("11th search err=%v limited=%+v", err, limited)
	}
	var claims int
	if err = db.QueryRow(`SELECT count(*) FROM request_budget_claims WHERE run_id=?`, run.ID).Scan(&claims); err != nil || claims != 10 {
		t.Fatalf("rejected request was claimed: count=%d err=%v", claims, err)
	}
}

func TestConcurrentBudgetClaimsCannotExceedLimit(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 9, 30, 0, 0, time.UTC)
	run := createQueuedRun(t, db, GitHub, now)
	policy := BudgetPolicy{HourlyLimit: 60, SearchMinuteLimit: 10}
	var accepted atomic.Int32
	var group sync.WaitGroup
	for range 80 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := ClaimRequestBudget(ctx, db, run.ID, GitHub, RequestGeneral, policy, now)
			if err == nil {
				accepted.Add(1)
				return
			}
			var limited *RateLimitError
			if !errors.As(err, &limited) {
				t.Errorf("claim: %v", err)
			}
		}()
	}
	group.Wait()
	if accepted.Load() != 60 {
		t.Fatalf("accepted claims=%d want=60", accepted.Load())
	}
	var stored int
	if err := db.QueryRow(`SELECT count(*) FROM request_budget_claims WHERE run_id=?`, run.ID).Scan(&stored); err != nil || stored != 60 {
		t.Fatalf("stored claims=%d err=%v", stored, err)
	}
}

func TestProviderLimitCanTightenLocalBudget(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	run := createQueuedRun(t, db, GitHub, now)
	if err := RecordProviderBudget(ctx, db, GitHub, "core", 0, now.Add(20*time.Minute), now); err != nil {
		t.Fatal(err)
	}
	_, err := ClaimRequestBudget(ctx, db, run.ID, GitHub, RequestGeneral, BudgetPolicy{HourlyLimit: 60, SearchMinuteLimit: 10}, now.Add(time.Minute))
	var limited *RateLimitError
	if !errors.As(err, &limited) || !limited.RetryAt.Equal(now.Add(20*time.Minute)) {
		t.Fatalf("provider limit ignored: err=%v limited=%+v", err, limited)
	}
	view, err := GetSourceView(ctx, db, GitHub, Credentials{})
	if err != nil || view.Budget.RemainingRequests == nil || *view.Budget.RemainingRequests != 0 || view.Budget.ResetAt == nil {
		t.Fatalf("source budget view: %+v err=%v", view.Budget, err)
	}
}

func TestProviderLimitCannotRaiseStricterLocalBudget(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	run := createQueuedRun(t, db, GitHub, now)
	policy := BudgetPolicy{HourlyLimit: 60, SearchMinuteLimit: 10}
	for index := 0; index < 50; index++ {
		if _, err := ClaimRequestBudget(ctx, db, run.ID, GitHub, RequestGeneral, policy, now); err != nil {
			t.Fatalf("local claim %d: %v", index+1, err)
		}
	}
	if err := RecordProviderBudget(ctx, db, GitHub, "core", 50, now.Add(2*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	state, err := ClaimRequestBudget(ctx, db, run.ID, GitHub, RequestGeneral, policy, now.Add(time.Minute))
	if err != nil || state.RemainingRequests != 9 || !state.ResetAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("looser provider raised local budget: state=%+v err=%v", state, err)
	}
}

func TestAnonymousGitHubPolicyNeverExceedsSixty(t *testing.T) {
	config := SourceConfig{RequestBudgetPerHour: 5000}
	if policy := githubBudgetPolicy(config, false); policy.HourlyLimit != 60 {
		t.Fatalf("anonymous hourly limit=%d", policy.HourlyLimit)
	}
	if policy := githubBudgetPolicy(config, true); policy.HourlyLimit != 5000 {
		t.Fatalf("token hourly limit=%d", policy.HourlyLimit)
	}
}

func enableSchedule(t *testing.T, db *sql.DB, source RemoteSource, revision int, now time.Time) {
	t.Helper()
	enabled := true
	err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		_, updateErr := UpdateSourceConfig(context.Background(), tx, source, revision, SourceConfigPatch{IsScheduleEnabled: &enabled}, false, now)
		return updateErr
	})
	if err != nil {
		t.Fatal(err)
	}
}
