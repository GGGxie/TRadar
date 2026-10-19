package brief

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"tradar/internal/storage"
)

const scheduleScope = "briefs:schedule"

type Scheduler struct{ db *sql.DB }

func NewScheduler(db *sql.DB) *Scheduler { return &Scheduler{db: db} }

// Tick creates at most one persisted scheduled intent for a local date. Wall
// clock comparison naturally catches up after a missing DST time; the stored
// date key suppresses the second occurrence during a repeated hour.
func (scheduler *Scheduler) Tick(ctx context.Context, now time.Time) ([]Run, error) {
	settings, err := GetSettings(ctx, scheduler.db)
	if err != nil || !settings.IsEnabled {
		return []Run{}, err
	}
	location, err := time.LoadLocation(settings.TimeZone)
	if err != nil {
		return nil, err
	}
	local := now.In(location)
	if local.Format("15:04") < settings.LocalTime {
		return []Run{}, nil
	}
	date := local.Format("2006-01-02")
	key := settings.TimeZone + ":" + date
	var run Run
	created := false
	err = storage.WithTx(ctx, scheduler.db, func(tx *sql.Tx) error {
		claim, err := tx.ExecContext(ctx, `INSERT INTO idempotency(scope,key,request_hash,status,body) VALUES(?,?,?,0,'null') ON CONFLICT(scope,key) DO NOTHING`, scheduleScope, key, SelectionVersion)
		if err != nil {
			return err
		}
		count, err := claim.RowsAffected()
		if err != nil || count == 0 {
			return err
		}
		run, err = AcceptRun(ctx, tx, CreateInput{Date: date, TimeZone: settings.TimeZone}, now)
		if err != nil {
			return err
		}
		body, err := json.Marshal(run)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE idempotency SET status=202,body=? WHERE scope=? AND key=?`, body, scheduleScope, key); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !created {
		return []Run{}, nil
	}
	return []Run{run}, nil
}

func RecoverInterrupted(ctx context.Context, db *sql.DB, now time.Time) (int64, error) {
	var changed int64
	err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE brief_runs SET status='INTERRUPTED',revision=revision+1,error_code='PROCESS_INTERRUPTED',finished_at=? WHERE status IN ('PENDING','RUNNING')`, now.UTC().Format("2006-01-02T15:04:05.000000000Z"))
		if err != nil {
			return err
		}
		changed, err = result.RowsAffected()
		return err
	})
	return changed, err
}
