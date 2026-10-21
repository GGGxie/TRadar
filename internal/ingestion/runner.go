package ingestion

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/storage"
)

type ExecutionResult struct {
	RequestsMade int
	IsTruncated  bool
	NextRetryAt  *time.Time
	Errors       []RunError
	Warnings     []RunError
}

type Executor interface {
	Execute(context.Context, IngestionRun, *Repository) (ExecutionResult, error)
}

type ExecutorFunc func(context.Context, IngestionRun, *Repository) (ExecutionResult, error)

func (f ExecutorFunc) Execute(ctx context.Context, run IngestionRun, repository *Repository) (ExecutionResult, error) {
	return f(ctx, run, repository)
}

type Runner struct {
	db          *sql.DB
	now         func() time.Time
	taskTimeout time.Duration
}

func NewRunner(db *sql.DB, now func() time.Time) *Runner {
	if now == nil {
		now = time.Now
	}
	return &Runner{db: db, now: now, taskTimeout: 5 * time.Minute}
}

func (r *Runner) Run(ctx context.Context, id string, executor Executor) error {
	var run IngestionRun
	err := storage.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		var startErr error
		run, startErr = StartRun(ctx, tx, id, r.now())
		return startErr
	})
	if err != nil {
		return err
	}
	repository := NewRepository(r.db)
	repository.now = r.now
	executionContext, cancel := context.WithTimeout(ctx, r.taskTimeout)
	result, executionErr := executor.Execute(executionContext, run, repository)
	executionContextErr := executionContext.Err()
	cancel()
	if executionErr == nil && executionContextErr != nil {
		executionErr = executionContextErr
	}
	latest, readErr := GetRun(ctx, r.db, id)
	if readErr != nil {
		return errors.Join(executionErr, readErr)
	}
	status := StatusSucceeded
	if executionErr != nil || len(result.Errors) > 0 {
		status = StatusFailed
		if latest.AcceptedItems > 0 || latest.ObservationsWritten > 0 {
			status = StatusPartial
		}
		if len(result.Errors) == 0 {
			result.Errors = []RunError{{Code: "COLLECT_FAILED", Message: "来源采集失败"}}
		}
	} else if latest.AcceptedItems == 0 && latest.ObservationsWritten == 0 {
		status = StatusSucceededEmpty
	}
	storedErrors := append(append([]RunError(nil), result.Errors...), result.Warnings...)
	if len(storedErrors) > 50 {
		storedErrors = storedErrors[:50]
	}
	finishErr := storage.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		_, err := FinishRun(ctx, tx, id, FinishRunInput{
			Status: status, RequestsMade: result.RequestsMade, IsTruncated: result.IsTruncated,
			NextRetryAt: result.NextRetryAt, Errors: storedErrors,
		}, r.now())
		if err != nil {
			return err
		}
		var nextRetry any
		if result.NextRetryAt != nil {
			nextRetry = catalog.Timestamp(*result.NextRetryAt)
		}
		_, err = tx.ExecContext(ctx, `UPDATE source_configs SET next_retry_at=? WHERE source=?`, nextRetry, run.Source)
		return err
	})
	return errors.Join(executionErr, finishErr)
}

func (r *Runner) RunMany(ctx context.Context, jobs map[string]Executor) map[string]error {
	results := make(map[string]error, len(jobs))
	var mutex sync.Mutex
	var group sync.WaitGroup
	for id, executor := range jobs {
		group.Add(1)
		go func() {
			defer group.Done()
			err := r.Run(ctx, id, executor)
			mutex.Lock()
			results[id] = err
			mutex.Unlock()
		}()
	}
	group.Wait()
	return results
}
