package ingestion

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"tradar/internal/catalog"
	"tradar/internal/fault"
	"tradar/internal/storage"
)

type Repository struct {
	db  *sql.DB
	now func() time.Time
}

type Batch struct {
	Key        string
	Checkpoint json.RawMessage
	Stats      BatchStats
}

type BatchStats struct {
	FetchedItems, AcceptedItems, SkippedItems, FailedItems, ObservationsWritten int
}

type Checkpoint struct {
	RunID     string          `json:"runId"`
	BatchKey  string          `json:"batchKey"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt string          `json:"updatedAt"`
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db, now: time.Now}
}

func (r *Repository) CommitBatch(ctx context.Context, runID string, batch Batch, write func(*sql.Tx) error) (bool, error) {
	batch.Key = strings.TrimSpace(batch.Key)
	if utf8.RuneCountInString(batch.Key) < 1 || utf8.RuneCountInString(batch.Key) > 200 {
		return false, fault.Field("batchKey", "批次键需要 1–200 字")
	}
	if len(batch.Checkpoint) < 2 || len(batch.Checkpoint) > 64<<10 || !json.Valid(batch.Checkpoint) || batch.Checkpoint[0] != '{' {
		return false, fault.Field("checkpoint", "checkpoint 需要是不超过 64 KiB 的 JSON 对象")
	}
	if batch.Stats.FetchedItems < 0 || batch.Stats.AcceptedItems < 0 || batch.Stats.SkippedItems < 0 || batch.Stats.FailedItems < 0 || batch.Stats.ObservationsWritten < 0 {
		return false, fault.Field("stats", "批次计数不能为负数")
	}
	committed := false
	err := storage.WithTx(ctx, r.db, func(tx *sql.Tx) error {
		run, err := GetRun(ctx, tx, runID)
		if err != nil {
			return err
		}
		if run.Status != StatusRunning {
			return fault.New("CONFLICT", "只有运行中的任务可以提交批次")
		}
		var checkpoint string
		var stored BatchStats
		err = tx.QueryRowContext(ctx, `SELECT checkpoint_json,fetched_items,accepted_items,skipped_items,failed_items,observations_written FROM ingestion_batches WHERE run_id=? AND batch_key=?`, runID, batch.Key).
			Scan(&checkpoint, &stored.FetchedItems, &stored.AcceptedItems, &stored.SkippedItems, &stored.FailedItems, &stored.ObservationsWritten)
		if err == nil {
			if !bytes.Equal([]byte(checkpoint), batch.Checkpoint) || stored != batch.Stats {
				return fault.New("IDEMPOTENCY_MISMATCH", "同一任务批次的内容发生变化")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if write != nil {
			if err = write(tx); err != nil {
				return err
			}
		}
		committedAt := catalog.Timestamp(r.now())
		_, err = tx.ExecContext(ctx, `INSERT INTO ingestion_batches(run_id,batch_key,checkpoint_json,fetched_items,accepted_items,skipped_items,failed_items,observations_written,committed_at) VALUES(?,?,?,?,?,?,?,?,?)`,
			runID, batch.Key, string(batch.Checkpoint), batch.Stats.FetchedItems, batch.Stats.AcceptedItems,
			batch.Stats.SkippedItems, batch.Stats.FailedItems, batch.Stats.ObservationsWritten, committedAt)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO ingestion_checkpoints(run_id,batch_key,checkpoint_json,updated_at) VALUES(?,?,?,?) ON CONFLICT(run_id) DO UPDATE SET batch_key=excluded.batch_key,checkpoint_json=excluded.checkpoint_json,updated_at=excluded.updated_at`,
			runID, batch.Key, string(batch.Checkpoint), committedAt)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE ingestion_runs SET fetched_items=fetched_items+?,accepted_items=accepted_items+?,skipped_items=skipped_items+?,failed_items=failed_items+?,observations_written=observations_written+? WHERE id=? AND status='RUNNING'`,
			batch.Stats.FetchedItems, batch.Stats.AcceptedItems, batch.Stats.SkippedItems, batch.Stats.FailedItems, batch.Stats.ObservationsWritten, runID)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return fault.New("CONFLICT", "任务状态在提交批次时发生变化")
		}
		committed = true
		return nil
	})
	return committed, err
}

func GetCheckpoint(ctx context.Context, q catalog.Querier, runID string) (Checkpoint, error) {
	var item Checkpoint
	var raw string
	err := q.QueryRowContext(ctx, `SELECT run_id,batch_key,checkpoint_json,updated_at FROM ingestion_checkpoints WHERE run_id=?`, runID).Scan(&item.RunID, &item.BatchKey, &raw, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Checkpoint{}, fault.Missing("采集检查点")
	}
	if err != nil {
		return Checkpoint{}, err
	}
	item.Value = json.RawMessage(raw)
	return item, nil
}
