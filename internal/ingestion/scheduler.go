package ingestion

import (
	"context"
	"database/sql"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/storage"
)

type Scheduler struct {
	db          *sql.DB
	credentials Credentials
}

func NewScheduler(db *sql.DB, credentials Credentials) *Scheduler {
	return &Scheduler{db: db, credentials: credentials}
}

func (s *Scheduler) Tick(ctx context.Context, now time.Time) ([]IngestionRun, error) {
	now = now.UTC()
	rows, err := s.db.QueryContext(ctx, `SELECT source FROM source_configs WHERE next_scheduled_at IS NOT NULL AND next_scheduled_at<=? ORDER BY source`, catalog.Timestamp(now))
	if err != nil {
		return nil, err
	}
	due := make([]RemoteSource, 0, 3)
	for rows.Next() {
		var source RemoteSource
		if err = rows.Scan(&source); err != nil {
			rows.Close()
			return nil, err
		}
		due = append(due, source)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	created := make([]IngestionRun, 0, len(due))
	for _, source := range due {
		var run IngestionRun
		madeRun := false
		err = storage.WithTx(ctx, s.db, func(tx *sql.Tx) error {
			config, configErr := GetSourceConfig(ctx, tx, source)
			if configErr != nil {
				return configErr
			}
			if config.NextScheduledAt == nil {
				return nil
			}
			dueAt, parseErr := time.Parse(time.RFC3339Nano, *config.NextScheduledAt)
			if parseErr != nil {
				return parseErr
			}
			if dueAt.After(now) {
				return nil
			}
			nextAt := now.Add(time.Duration(config.IntervalMinutes) * time.Minute)
			retryBlocked := false
			if config.NextRetryAt != nil {
				retryAt, retryErr := time.Parse(time.RFC3339Nano, *config.NextRetryAt)
				if retryErr != nil {
					return retryErr
				}
				if retryAt.After(nextAt) {
					nextAt = retryAt
				}
				retryBlocked = retryAt.After(now)
			}
			updated, updateErr := tx.ExecContext(ctx, `UPDATE source_configs SET next_scheduled_at=? WHERE source=? AND next_scheduled_at=?`, catalog.Timestamp(nextAt), source, *config.NextScheduledAt)
			if updateErr != nil {
				return updateErr
			}
			changed, updateErr := updated.RowsAffected()
			if updateErr != nil {
				return updateErr
			}
			if changed != 1 {
				return nil
			}
			if retryBlocked || !config.IsEnabled || !config.IsScheduleEnabled || source == ProductHunt && !s.credentials.ProductHunt {
				return nil
			}
			var active int
			if queryErr := tx.QueryRowContext(ctx, `SELECT count(*) FROM ingestion_runs WHERE source=? AND status IN ('QUEUED','RUNNING','RETRY_WAIT')`, source).Scan(&active); queryErr != nil {
				return queryErr
			}
			if active > 0 {
				return nil
			}
			hasCredentials := source == GitHub && s.credentials.GitHub || source == ProductHunt && s.credentials.ProductHunt
			run, configErr = CreateRun(ctx, tx, CreateRunInput{Source: source, Trigger: TriggerScheduled, HasCredentials: hasCredentials}, now)
			madeRun = configErr == nil
			return configErr
		})
		if err != nil {
			return nil, err
		}
		if madeRun {
			created = append(created, run)
		}
	}
	return created, nil
}
