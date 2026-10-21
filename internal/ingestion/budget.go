package ingestion

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"tradar/internal/catalog"
	"tradar/internal/fault"
	"tradar/internal/storage"
)

type RequestClass string

const (
	RequestGeneral RequestClass = "GENERAL"
	RequestSearch  RequestClass = "SEARCH"
)

type BudgetPolicy struct {
	HourlyLimit       int
	SearchMinuteLimit int
}

type BudgetState struct {
	RemainingRequests int
	ResetAt           time.Time
}

type RateLimitError struct {
	RetryAt time.Time
}

func (e *RateLimitError) Error() string { return "来源请求预算暂不可用" }

// CheckRequestBudgetAvailability verifies that a request could claim budget
// without consuming it. ClaimRequestBudget remains the authoritative atomic
// check immediately before each outbound request.
func CheckRequestBudgetAvailability(ctx context.Context, q catalog.Querier, source RemoteSource, class RequestClass, policy BudgetPolicy, now time.Time) (BudgetState, error) {
	if !IsRemoteSource(source) {
		return BudgetState{}, fault.Field("source", "未知来源")
	}
	if class != RequestGeneral && class != RequestSearch {
		return BudgetState{}, fault.Field("requestClass", "未知请求类型")
	}
	if policy.HourlyLimit < 1 || policy.HourlyLimit > 5000 || policy.SearchMinuteLimit < 1 || policy.SearchMinuteLimit > 1000 {
		return BudgetState{}, fault.Field("budget", "请求预算策略无效")
	}
	now = now.UTC()
	hourCount, hourReset, err := budgetWindow(ctx, q, source, "", now.Add(-time.Hour))
	if err != nil {
		return BudgetState{}, err
	}
	retryAt := time.Time{}
	if hourCount >= policy.HourlyLimit {
		retryAt = laterTime(retryAt, hourReset)
	}
	if class == RequestSearch {
		searchCount, searchReset, countErr := budgetWindow(ctx, q, source, RequestSearch, now.Add(-time.Minute))
		if countErr != nil {
			return BudgetState{}, countErr
		}
		if searchCount >= policy.SearchMinuteLimit {
			retryAt = laterTime(retryAt, searchReset)
		}
	}
	stored, err := loadStoredBudgetState(ctx, q, source)
	if err != nil {
		return BudgetState{}, err
	}
	providerActive := stored.providerRemaining != nil && stored.providerReset.After(now)
	if providerActive && *stored.providerRemaining <= 0 {
		retryAt = laterTime(retryAt, stored.providerReset)
	}
	if !retryAt.IsZero() {
		return BudgetState{}, &RateLimitError{RetryAt: retryAt}
	}
	localReset := now.Add(time.Hour)
	if hourCount > 0 {
		localReset = hourReset
	}
	local := BudgetState{RemainingRequests: policy.HourlyLimit - hourCount, ResetAt: localReset}
	provider := BudgetState{}
	if providerActive {
		provider = BudgetState{RemainingRequests: *stored.providerRemaining, ResetAt: stored.providerReset}
	}
	return effectiveBudgetState(local, provider, providerActive), nil
}

func ClaimRequestBudget(ctx context.Context, db *sql.DB, runID string, source RemoteSource, class RequestClass, policy BudgetPolicy, now time.Time) (BudgetState, error) {
	if !IsRemoteSource(source) {
		return BudgetState{}, fault.Field("source", "未知来源")
	}
	if class != RequestGeneral && class != RequestSearch {
		return BudgetState{}, fault.Field("requestClass", "未知请求类型")
	}
	if policy.HourlyLimit < 1 || policy.HourlyLimit > 5000 || policy.SearchMinuteLimit < 1 || policy.SearchMinuteLimit > 1000 {
		return BudgetState{}, fault.Field("budget", "请求预算策略无效")
	}
	now = now.UTC()
	var state BudgetState
	err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		run, err := GetRun(ctx, tx, runID)
		if err != nil {
			return err
		}
		if run.Source != source {
			return fault.Field("source", "预算来源与任务不匹配")
		}
		hourStart := now.Add(-time.Hour)
		if _, err = tx.ExecContext(ctx, `DELETE FROM request_budget_claims WHERE claimed_at<=?`, catalog.Timestamp(hourStart)); err != nil {
			return err
		}
		hourCount, hourReset, err := budgetWindow(ctx, tx, source, "", hourStart)
		if err != nil {
			return err
		}
		retryAt := time.Time{}
		if hourCount >= policy.HourlyLimit {
			retryAt = laterTime(retryAt, hourReset)
		}
		if class == RequestSearch {
			minuteStart := now.Add(-time.Minute)
			searchCount, searchReset, countErr := budgetWindow(ctx, tx, source, RequestSearch, minuteStart)
			if countErr != nil {
				return countErr
			}
			if searchCount >= policy.SearchMinuteLimit {
				retryAt = laterTime(retryAt, searchReset)
			}
		}

		stored, err := loadStoredBudgetState(ctx, tx, source)
		if err != nil {
			return err
		}
		providerActive := stored.providerRemaining != nil && stored.providerReset.After(now)
		if providerActive && *stored.providerRemaining <= 0 {
			retryAt = laterTime(retryAt, stored.providerReset)
		}
		if !retryAt.IsZero() {
			return &RateLimitError{RetryAt: retryAt}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO request_budget_claims(id,source,run_id,request_class,claimed_at) VALUES(?,?,?,?,?)`, uuid.NewString(), source, runID, class, catalog.Timestamp(now))
		if err != nil {
			return err
		}
		localRemaining := policy.HourlyLimit - hourCount - 1
		resetAt := now.Add(time.Hour)
		if hourCount > 0 {
			resetAt = hourReset
		}
		local := BudgetState{RemainingRequests: localRemaining, ResetAt: resetAt}
		provider := BudgetState{}
		if providerActive {
			(*stored.providerRemaining)--
			provider = BudgetState{RemainingRequests: *stored.providerRemaining, ResetAt: stored.providerReset}
		}
		state = effectiveBudgetState(local, provider, providerActive)
		_, err = tx.ExecContext(ctx, `INSERT INTO source_budget_states(
			source,local_remaining_requests,local_reset_at,provider_remaining_requests,provider_reset_at,provider_observed_at,updated_at
		) VALUES(?,?,?,?,?,?,?) ON CONFLICT(source) DO UPDATE SET
			local_remaining_requests=excluded.local_remaining_requests,local_reset_at=excluded.local_reset_at,
			provider_remaining_requests=excluded.provider_remaining_requests,provider_reset_at=excluded.provider_reset_at,
			provider_observed_at=excluded.provider_observed_at,updated_at=excluded.updated_at`,
			source, local.RemainingRequests, catalog.Timestamp(local.ResetAt), nullableInt(stored.providerRemaining), nullableTime(stored.providerReset), nullableTime(stored.providerObservedAt), catalog.Timestamp(now))
		return err
	})
	return state, err
}

func RecordProviderBudget(ctx context.Context, db *sql.DB, source RemoteSource, remaining int, resetAt, observedAt time.Time) error {
	maxRemaining := 5000
	if source == ProductHunt {
		maxRemaining = 1_000_000
	}
	if !IsRemoteSource(source) || remaining < 0 || remaining > maxRemaining || !resetAt.After(observedAt) {
		return fault.Field("budget", "来源配额响应无效")
	}
	return storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO source_budget_states(
			source,local_remaining_requests,local_reset_at,provider_remaining_requests,provider_reset_at,provider_observed_at,updated_at
		) VALUES(?,?,?,?,?,?,?) ON CONFLICT(source) DO UPDATE SET
			provider_remaining_requests=excluded.provider_remaining_requests,provider_reset_at=excluded.provider_reset_at,
			provider_observed_at=excluded.provider_observed_at,updated_at=excluded.updated_at`,
			source, 0, catalog.Timestamp(observedAt), remaining, catalog.Timestamp(resetAt), catalog.Timestamp(observedAt), catalog.Timestamp(observedAt))
		return err
	})
}

func budgetWindow(ctx context.Context, q catalog.Querier, source RemoteSource, class RequestClass, start time.Time) (int, time.Time, error) {
	where := `source=? AND claimed_at>?`
	args := []any{source, catalog.Timestamp(start)}
	if class != "" {
		where += ` AND request_class=?`
		args = append(args, class)
	}
	var count int
	var earliest sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT count(*),min(claimed_at) FROM request_budget_claims WHERE `+where, args...).Scan(&count, &earliest); err != nil {
		return 0, time.Time{}, err
	}
	if !earliest.Valid {
		return count, time.Time{}, nil
	}
	value, err := time.Parse(time.RFC3339Nano, earliest.String)
	if err != nil {
		return 0, time.Time{}, err
	}
	duration := time.Hour
	if class == RequestSearch {
		duration = time.Minute
	}
	return count, value.Add(duration), nil
}

type storedBudgetState struct {
	localRemaining     int
	localReset         time.Time
	providerRemaining  *int
	providerReset      time.Time
	providerObservedAt time.Time
}

func loadStoredBudgetState(ctx context.Context, q catalog.Querier, source RemoteSource) (storedBudgetState, error) {
	var state storedBudgetState
	var localReset string
	var providerRemaining sql.NullInt64
	var providerReset, providerObserved sql.NullString
	err := q.QueryRowContext(ctx, `SELECT local_remaining_requests,local_reset_at,provider_remaining_requests,provider_reset_at,provider_observed_at FROM source_budget_states WHERE source=?`, source).
		Scan(&state.localRemaining, &localReset, &providerRemaining, &providerReset, &providerObserved)
	if errors.Is(err, sql.ErrNoRows) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	state.localReset, err = time.Parse(time.RFC3339Nano, localReset)
	if err != nil {
		return state, err
	}
	if providerRemaining.Valid {
		value := int(providerRemaining.Int64)
		state.providerRemaining = &value
		state.providerReset, err = time.Parse(time.RFC3339Nano, providerReset.String)
		if err != nil {
			return state, err
		}
		state.providerObservedAt, err = time.Parse(time.RFC3339Nano, providerObserved.String)
		if err != nil {
			return state, err
		}
	}
	return state, nil
}

func readEffectiveBudgetState(ctx context.Context, q catalog.Querier, source RemoteSource, now time.Time) (BudgetState, bool, error) {
	stored, err := loadStoredBudgetState(ctx, q, source)
	if err != nil {
		return BudgetState{}, false, err
	}
	localActive := stored.localReset.After(now)
	providerActive := stored.providerRemaining != nil && stored.providerReset.After(now)
	if !localActive && !providerActive {
		return BudgetState{}, false, nil
	}
	local := BudgetState{RemainingRequests: stored.localRemaining, ResetAt: stored.localReset}
	provider := BudgetState{}
	if providerActive {
		provider = BudgetState{RemainingRequests: *stored.providerRemaining, ResetAt: stored.providerReset}
	}
	if !localActive {
		return provider, true, nil
	}
	return effectiveBudgetState(local, provider, providerActive), true, nil
}

func effectiveBudgetState(local, provider BudgetState, providerActive bool) BudgetState {
	if !providerActive || local.RemainingRequests < provider.RemainingRequests {
		return local
	}
	if provider.RemainingRequests < local.RemainingRequests {
		return provider
	}
	return BudgetState{RemainingRequests: local.RemainingRequests, ResetAt: laterTime(local.ResetAt, provider.ResetAt)}
}

func laterTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return catalog.Timestamp(value)
}
