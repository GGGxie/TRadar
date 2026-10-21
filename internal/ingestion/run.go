package ingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"tradar/internal/catalog"
	"tradar/internal/fault"
	"tradar/internal/storage"
)

type RunStatus string

const (
	StatusQueued         RunStatus = "QUEUED"
	StatusRunning        RunStatus = "RUNNING"
	StatusRetryWait      RunStatus = "RETRY_WAIT"
	StatusSucceeded      RunStatus = "SUCCEEDED"
	StatusSucceededEmpty RunStatus = "SUCCEEDED_EMPTY"
	StatusPartial        RunStatus = "PARTIAL"
	StatusFailed         RunStatus = "FAILED"
	StatusInterrupted    RunStatus = "INTERRUPTED"
)

const (
	TriggerManual    = "MANUAL"
	TriggerScheduled = "SCHEDULED"
)

type RunError struct {
	Code    string  `json:"code"`
	Message string  `json:"message"`
	ItemID  *string `json:"itemId"`
}

type IngestionRun struct {
	ID                  string       `json:"id"`
	Source              RemoteSource `json:"source"`
	Trigger             string       `json:"trigger"`
	Status              RunStatus    `json:"status"`
	ConfigRevision      int          `json:"configRevision"`
	CreatedAt           string       `json:"createdAt"`
	StartedAt           *string      `json:"startedAt"`
	FinishedAt          *string      `json:"finishedAt"`
	WindowStart         *string      `json:"windowStart"`
	WindowEnd           string       `json:"windowEnd"`
	FetchedItems        int          `json:"fetchedItems"`
	AcceptedItems       int          `json:"acceptedItems"`
	SkippedItems        int          `json:"skippedItems"`
	FailedItems         int          `json:"failedItems"`
	ObservationsWritten int          `json:"observationsWritten"`
	RequestsMade        int          `json:"requestsMade"`
	IsTruncated         bool         `json:"isTruncated"`
	NextRetryAt         *string      `json:"nextRetryAt"`
	Errors              []RunError   `json:"errors"`
	Config              SourceConfig `json:"-"`
}

type CreateRunInput struct {
	Source         RemoteSource `json:"source"`
	Trigger        string       `json:"-"`
	HasCredentials bool         `json:"-"`
}

type RunFilter struct {
	Source         RemoteSource
	Status         RunStatus
	Page, PageSize int
}

type FinishRunInput struct {
	Status       RunStatus
	RequestsMade int
	IsTruncated  bool
	NextRetryAt  *time.Time
	Errors       []RunError
}

const runColumns = `id,source,trigger,status,config_revision,config_json,created_at,started_at,finished_at,window_start,window_end,fetched_items,accepted_items,skipped_items,failed_items,observations_written,requests_made,is_truncated,next_retry_at,errors_json`

func CreateRun(ctx context.Context, tx *sql.Tx, input CreateRunInput, now time.Time) (IngestionRun, error) {
	if input.Trigger == "" {
		input.Trigger = TriggerManual
	}
	if input.Trigger != TriggerManual && input.Trigger != TriggerScheduled {
		return IngestionRun{}, fault.Field("trigger", "未知任务触发方式")
	}
	config, err := GetSourceConfig(ctx, tx, input.Source)
	if err != nil {
		return IngestionRun{}, err
	}
	if !config.IsEnabled {
		return IngestionRun{}, fault.New("SOURCE_DISABLED", "来源已禁用")
	}
	if input.Source == ProductHunt && !input.HasCredentials {
		return IngestionRun{}, fault.New("SOURCE_CREDENTIALS_REQUIRED", "Product Hunt 来源需要配置凭据")
	}
	var activeID string
	err = tx.QueryRowContext(ctx, `SELECT id FROM ingestion_runs WHERE source=? AND status IN ('QUEUED','RUNNING','RETRY_WAIT') LIMIT 1`, input.Source).Scan(&activeID)
	if err == nil {
		return IngestionRun{}, &fault.Error{Code: "SOURCE_BUSY", Message: fmt.Sprintf("此来源已有未结束任务 %s", activeID), Fields: map[string]string{"runId": activeID}}
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return IngestionRun{}, err
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return IngestionRun{}, err
	}
	end := now.UTC()
	var start *string
	if input.Source == GitHub || input.Source == ProductHunt {
		value := catalog.Timestamp(end.AddDate(0, 0, -config.DiscoveryDays))
		start = &value
	}
	run := IngestionRun{
		ID: uuid.NewString(), Source: input.Source, Trigger: input.Trigger, Status: StatusQueued,
		ConfigRevision: config.Revision, Config: config, CreatedAt: catalog.Timestamp(now),
		WindowStart: start, WindowEnd: catalog.Timestamp(end), Errors: []RunError{},
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ingestion_runs (`+runColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		run.ID, run.Source, run.Trigger, run.Status, run.ConfigRevision, string(configJSON), run.CreatedAt,
		nil, nil, run.WindowStart, run.WindowEnd, 0, 0, 0, 0, 0, 0, 0, nil, `[]`)
	if err != nil {
		if strings.Contains(err.Error(), "ingestion_runs.source") {
			return IngestionRun{}, fault.New("SOURCE_BUSY", "此来源已有未结束任务")
		}
		return IngestionRun{}, err
	}
	return run, nil
}

func GetRun(ctx context.Context, q catalog.Querier, id string) (IngestionRun, error) {
	run, err := scanRun(q.QueryRowContext(ctx, `SELECT `+runColumns+` FROM ingestion_runs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return IngestionRun{}, fault.Missing("采集任务")
	}
	return run, err
}

func ListRuns(ctx context.Context, q catalog.Querier, filter RunFilter) ([]IngestionRun, int, error) {
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return nil, 0, fault.Field("page", "分页参数无效")
	}
	if filter.Source != "" && !IsRemoteSource(filter.Source) {
		return nil, 0, fault.Field("source", "未知来源")
	}
	if filter.Status != "" && !isRunStatus(filter.Status) {
		return nil, 0, fault.Field("status", "未知任务状态")
	}
	where := ` WHERE 1=1`
	args := []any{}
	if filter.Source != "" {
		where += ` AND source=?`
		args = append(args, filter.Source)
	}
	if filter.Status != "" {
		where += ` AND status=?`
		args = append(args, filter.Status)
	}
	var total int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM ingestion_runs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	pageArgs := append(append([]any(nil), args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := q.QueryContext(ctx, `SELECT `+runColumns+` FROM ingestion_runs`+where+` ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]IngestionRun, 0)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, run)
	}
	return items, total, rows.Err()
}

func StartRun(ctx context.Context, tx *sql.Tx, id string, now time.Time) (IngestionRun, error) {
	run, err := GetRun(ctx, tx, id)
	if err != nil {
		return IngestionRun{}, err
	}
	if run.Status != StatusQueued && run.Status != StatusRetryWait {
		return IngestionRun{}, fault.New("CONFLICT", "采集任务不处于可启动状态")
	}
	startedAt := catalog.Timestamp(now)
	result, err := tx.ExecContext(ctx, `UPDATE ingestion_runs SET status='RUNNING',started_at=coalesce(started_at,?),next_retry_at=NULL WHERE id=? AND status=?`, startedAt, id, run.Status)
	if err != nil {
		return IngestionRun{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return IngestionRun{}, err
	}
	if changed != 1 {
		return IngestionRun{}, fault.New("CONFLICT", "采集任务状态已改变")
	}
	return GetRun(ctx, tx, id)
}

func FinishRun(ctx context.Context, tx *sql.Tx, id string, input FinishRunInput, now time.Time) (IngestionRun, error) {
	if !isTerminal(input.Status) {
		return IngestionRun{}, fault.Field("status", "完成任务需要终态")
	}
	if input.RequestsMade < 0 {
		return IngestionRun{}, fault.Field("requestsMade", "请求数不能为负数")
	}
	if len(input.Errors) > 50 {
		input.Errors = input.Errors[:50]
	}
	for _, item := range input.Errors {
		if utf8.RuneCountInString(item.Code) < 1 || utf8.RuneCountInString(item.Code) > 100 || utf8.RuneCountInString(item.Message) < 1 || utf8.RuneCountInString(item.Message) > 1000 {
			return IngestionRun{}, fault.Field("errors", "任务错误格式无效")
		}
	}
	errorsJSON, err := json.Marshal(input.Errors)
	if err != nil {
		return IngestionRun{}, err
	}
	var retryAt *string
	if input.NextRetryAt != nil {
		value := catalog.Timestamp(*input.NextRetryAt)
		retryAt = &value
	}
	result, err := tx.ExecContext(ctx, `UPDATE ingestion_runs SET status=?,finished_at=?,requests_made=requests_made+?,is_truncated=?,next_retry_at=?,errors_json=? WHERE id=? AND status='RUNNING'`,
		input.Status, catalog.Timestamp(now), input.RequestsMade, input.IsTruncated, retryAt, string(errorsJSON), id)
	if err != nil {
		return IngestionRun{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return IngestionRun{}, err
	}
	if changed != 1 {
		return IngestionRun{}, fault.New("CONFLICT", "采集任务不处于运行状态")
	}
	return GetRun(ctx, tx, id)
}

func RecoverInterrupted(ctx context.Context, db *sql.DB, now time.Time) (int64, error) {
	var changed int64
	err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE ingestion_runs SET status='INTERRUPTED',finished_at=? WHERE status IN ('QUEUED','RUNNING','RETRY_WAIT')`, catalog.Timestamp(now))
		if err != nil {
			return err
		}
		changed, err = result.RowsAffected()
		return err
	})
	return changed, err
}

func scanRun(row interface{ Scan(...any) error }) (IngestionRun, error) {
	var run IngestionRun
	var configJSON, errorsJSON string
	var started, finished, windowStart, retry sql.NullString
	var truncated int
	err := row.Scan(&run.ID, &run.Source, &run.Trigger, &run.Status, &run.ConfigRevision, &configJSON,
		&run.CreatedAt, &started, &finished, &windowStart, &run.WindowEnd,
		&run.FetchedItems, &run.AcceptedItems, &run.SkippedItems, &run.FailedItems,
		&run.ObservationsWritten, &run.RequestsMade, &truncated, &retry, &errorsJSON)
	if err != nil {
		return IngestionRun{}, err
	}
	if err = json.Unmarshal([]byte(configJSON), &run.Config); err != nil {
		return IngestionRun{}, fmt.Errorf("stored ingestion config: %w", err)
	}
	run.Config.Revision = run.ConfigRevision
	if err = json.Unmarshal([]byte(errorsJSON), &run.Errors); err != nil {
		return IngestionRun{}, fmt.Errorf("stored ingestion errors: %w", err)
	}
	if run.Errors == nil {
		run.Errors = []RunError{}
	}
	run.StartedAt = nullStringPointer(started)
	run.FinishedAt = nullStringPointer(finished)
	run.WindowStart = nullStringPointer(windowStart)
	run.NextRetryAt = nullStringPointer(retry)
	run.IsTruncated = truncated == 1
	return run, nil
}

func isRunStatus(value RunStatus) bool {
	switch value {
	case StatusQueued, StatusRunning, StatusRetryWait, StatusSucceeded, StatusSucceededEmpty, StatusPartial, StatusFailed, StatusInterrupted:
		return true
	default:
		return false
	}
}

func isTerminal(value RunStatus) bool {
	return value == StatusSucceeded || value == StatusSucceededEmpty || value == StatusPartial || value == StatusFailed || value == StatusInterrupted
}
