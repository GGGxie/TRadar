package brief

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"
)

type RuntimeConfig struct {
	Now              func() time.Time
	ScheduleInterval time.Duration
}

type Runtime struct {
	db        *sql.DB
	now       func() time.Time
	interval  time.Duration
	scheduler *Scheduler
	queue     chan Run

	mu      sync.Mutex
	ctx     context.Context
	started bool
	pending map[string]struct{}
	wait    sync.WaitGroup
}

func NewRuntime(db *sql.DB, config RuntimeConfig) *Runtime {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.ScheduleInterval <= 0 {
		config.ScheduleInterval = 30 * time.Second
	}
	return &Runtime{
		db: db, now: config.Now, interval: config.ScheduleInterval,
		scheduler: NewScheduler(db), queue: make(chan Run, 16), pending: make(map[string]struct{}),
	}
}

func (runtime *Runtime) Start(parent context.Context) {
	runtime.mu.Lock()
	if runtime.started {
		runtime.mu.Unlock()
		return
	}
	runtime.started, runtime.ctx = true, parent
	runtime.mu.Unlock()
	runtime.wait.Add(2)
	go runtime.worker(parent)
	go runtime.scheduleLoop(parent)
}

func (runtime *Runtime) Enqueue(run Run) error {
	if run.ID == "" || run.Status != "PENDING" {
		return errors.New("invalid queued brief run")
	}
	runtime.mu.Lock()
	if !runtime.started || runtime.ctx == nil {
		runtime.mu.Unlock()
		return errors.New("brief runtime is not started")
	}
	if _, exists := runtime.pending[run.ID]; exists {
		runtime.mu.Unlock()
		return nil
	}
	runtime.pending[run.ID] = struct{}{}
	ctx := runtime.ctx
	runtime.mu.Unlock()
	select {
	case runtime.queue <- run:
		return nil
	case <-ctx.Done():
		runtime.mu.Lock()
		delete(runtime.pending, run.ID)
		runtime.mu.Unlock()
		return ctx.Err()
	}
}

func (runtime *Runtime) Wait() { runtime.wait.Wait() }

func (runtime *Runtime) worker(ctx context.Context) {
	defer runtime.wait.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case run := <-runtime.queue:
			_, err := ProcessRun(ctx, runtime.db, run.ID, runtime.now())
			runtime.mu.Lock()
			delete(runtime.pending, run.ID)
			runtime.mu.Unlock()
			if err != nil && ctx.Err() == nil {
				slog.Error("brief run failed", "runId", run.ID, "error", err)
			}
		}
	}
}

func (runtime *Runtime) scheduleLoop(ctx context.Context) {
	defer runtime.wait.Done()
	runtime.scheduleDue(ctx)
	ticker := time.NewTicker(runtime.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runtime.scheduleDue(ctx)
		}
	}
}

func (runtime *Runtime) scheduleDue(ctx context.Context) {
	runs, err := runtime.scheduler.Tick(ctx, runtime.now())
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("brief scheduler failed", "error", err)
		}
		return
	}
	for _, run := range runs {
		if err = runtime.Enqueue(run); err != nil && ctx.Err() == nil {
			slog.Error("brief schedule enqueue failed", "runId", run.ID, "error", err)
		}
	}
}
