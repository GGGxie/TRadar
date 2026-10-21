package ingestion

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type RuntimeConfig struct {
	GitHubToken, ProductHuntToken                     string
	GitHubClient, HackerNewsClient, ProductHuntClient *http.Client
	Now                                               func() time.Time
	ScheduleInterval                                  time.Duration
}

type Runtime struct {
	db        *sql.DB
	now       func() time.Time
	interval  time.Duration
	runner    *Runner
	scheduler *Scheduler
	executors map[RemoteSource]Executor
	queue     chan IngestionRun

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
	credentials := Credentials{GitHub: config.GitHubToken != "", ProductHunt: config.ProductHuntToken != ""}
	return &Runtime{
		db: db, now: config.Now, interval: config.ScheduleInterval,
		runner: NewRunner(db, config.Now), scheduler: NewScheduler(db, credentials),
		executors: map[RemoteSource]Executor{
			GitHub:      NewGitHubExecutor(db, config.GitHubClient, config.GitHubToken, config.Now),
			HackerNews:  NewHackerNewsExecutor(db, config.HackerNewsClient, config.Now),
			ProductHunt: NewProductHuntExecutor(db, config.ProductHuntClient, config.ProductHuntToken, config.Now),
		},
		queue: make(chan IngestionRun, 16), pending: make(map[string]struct{}),
	}
}

func (r *Runtime) Start(parent context.Context) {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.ctx = parent
	r.mu.Unlock()
	for range 3 {
		r.wait.Add(1)
		go r.worker(parent)
	}
	r.wait.Add(1)
	go r.scheduleLoop(parent)
}

func (r *Runtime) Enqueue(run IngestionRun) error {
	if !IsRemoteSource(run.Source) || run.ID == "" || run.Status != StatusQueued {
		return errors.New("invalid queued ingestion run")
	}
	r.mu.Lock()
	if !r.started || r.ctx == nil {
		r.mu.Unlock()
		return errors.New("ingestion runtime is not started")
	}
	if _, exists := r.pending[run.ID]; exists {
		r.mu.Unlock()
		return nil
	}
	r.pending[run.ID] = struct{}{}
	ctx := r.ctx
	r.mu.Unlock()
	select {
	case r.queue <- run:
		return nil
	case <-ctx.Done():
		r.mu.Lock()
		delete(r.pending, run.ID)
		r.mu.Unlock()
		return ctx.Err()
	}
}

func (r *Runtime) Wait() {
	r.wait.Wait()
}

func (r *Runtime) worker(ctx context.Context) {
	defer r.wait.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case run := <-r.queue:
			executor := r.executors[run.Source]
			err := r.runner.Run(ctx, run.ID, executor)
			r.mu.Lock()
			delete(r.pending, run.ID)
			r.mu.Unlock()
			if err != nil {
				slog.Error("ingestion run failed", "runId", run.ID, "source", run.Source, "error", err)
			}
		}
	}
}

func (r *Runtime) scheduleLoop(ctx context.Context) {
	defer r.wait.Done()
	r.scheduleDue(ctx)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.scheduleDue(ctx)
		}
	}
}

func (r *Runtime) scheduleDue(ctx context.Context) {
	runs, err := r.scheduler.Tick(ctx, r.now())
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("ingestion scheduler failed", "error", err)
		}
		return
	}
	for _, run := range runs {
		if err = r.Enqueue(run); err != nil && ctx.Err() == nil {
			slog.Error("ingestion schedule enqueue failed", "runId", run.ID, "source", run.Source, "error", err)
		}
	}
}
