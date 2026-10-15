package ingestion

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"tradar/internal/storage"
)

type ExecutionResult struct {
	RequestsMade int
	IsTruncated  bool
	NextRetryAt  *time.Time
	Errors       []RunError
}

type Executor interface {
	Execute(context.Context, IngestionRun, *Repository) (ExecutionResult, error)
}

type ExecutorFunc func(context.Context, IngestionRun, *Repository) (ExecutionResult, error)

func (f ExecutorFunc) Execute(ctx context.Context, run IngestionRun, repository *Repository) (ExecutionResult, error) {
	return f(ctx, run, repository)
}

type Runner struct {
	db  *sql.DB
	now func() time.Time
}

func NewRunner(db *sql.DB, now func() time.Time) *Runner {
	if now == nil {
		now = time.Now
	}
	return &Runner{db: db, now: now}
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
	result, executionErr := executor.Execute(ctx, run, repository)
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
	finishErr := storage.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		_, err := FinishRun(ctx, tx, id, FinishRunInput{
			Status: status, RequestsMade: result.RequestsMade, IsTruncated: result.IsTruncated,
			NextRetryAt: result.NextRetryAt, Errors: result.Errors,
		}, r.now())
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
