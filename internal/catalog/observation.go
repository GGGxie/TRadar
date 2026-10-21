package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"tradar/internal/fault"
)

const maxSafeInteger int64 = 9007199254740991

type ProviderPeriod struct {
	WeekUnix int64  `json:"weekUnix"`
	DayIndex int    `json:"dayIndex"`
	TimeZone string `json:"timeZone"`
}

type Observation struct {
	ID             string          `json:"id"`
	SourceRecordID string          `json:"sourceRecordId"`
	RunID          string          `json:"runId"`
	Metric         string          `json:"metric"`
	Basis          string          `json:"basis"`
	PeriodKey      string          `json:"periodKey"`
	Value          int64           `json:"value"`
	ObservedAt     string          `json:"observedAt"`
	FetchedAt      string          `json:"fetchedAt"`
	ProviderPeriod *ProviderPeriod `json:"providerPeriod"`
}

type ObservationInput struct {
	Metric         string
	Basis          string
	PeriodKey      string
	Value          int64
	ObservedAt     time.Time
	FetchedAt      time.Time
	ProviderPeriod *ProviderPeriod
}

type ObservationFilter struct {
	SourceRecordID string
	Metric         string
	Basis          string
	From           time.Time
	To             time.Time
	Page           int
	PageSize       int
}

const observationColumns = "id,source_record_id,run_id,metric,basis,period_key,value,observed_at,fetched_at,provider_period"

func AppendObservations(ctx context.Context, q *sql.Tx, runID, sourceRecordID string, inputs []ObservationInput) ([]Observation, error) {
	runID = strings.TrimSpace(runID)
	if n := utf8.RuneCountInString(runID); n < 1 || n > 200 {
		return nil, fault.Field("runId", "采集 run ID 需要 1–200 字")
	}
	if len(inputs) > 100 {
		return nil, fault.Field("observations", "单次最多写入 100 条观测")
	}
	if _, err := GetSource(ctx, q, sourceRecordID); err != nil {
		return nil, err
	}
	items := make([]Observation, 0, len(inputs))
	for _, input := range inputs {
		item, providerJSON, err := prepareObservation(runID, sourceRecordID, input)
		if err != nil {
			return nil, err
		}
		result, err := q.ExecContext(ctx, `INSERT INTO observations (`+observationColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(run_id,source_record_id,metric,basis,period_key) DO NOTHING`,
			item.ID, item.SourceRecordID, item.RunID, item.Metric, item.Basis,
			item.PeriodKey, item.Value, item.ObservedAt, item.FetchedAt, providerJSON)
		if err != nil {
			return nil, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if count == 0 {
			existing, err := scanObservation(q.QueryRowContext(ctx, `SELECT `+observationColumns+` FROM observations WHERE run_id=? AND source_record_id=? AND metric=? AND basis=? AND period_key=?`,
				runID, sourceRecordID, item.Metric, item.Basis, item.PeriodKey))
			if err != nil {
				return nil, err
			}
			if !sameObservation(existing, item) {
				return nil, fault.New("IDEMPOTENCY_MISMATCH", "同一 run 的观测内容发生变化")
			}
			item = existing
		}
		items = append(items, item)
	}
	return items, nil
}

func ListObservations(ctx context.Context, q Querier, projectID string, filter ObservationFilter) ([]Observation, int, error) {
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return nil, 0, fault.Field("page", "分页参数无效")
	}
	if filter.From.IsZero() || filter.To.IsZero() || !filter.To.After(filter.From) || filter.To.Sub(filter.From) > 90*24*time.Hour {
		return nil, 0, fault.Field("from", "观测窗口需要大于 0 且不超过 90 天")
	}
	if filter.Metric != "" && !isMetric(filter.Metric) {
		return nil, 0, fault.Field("metric", "未知指标")
	}
	if filter.Basis != "" && !isBasis(filter.Basis) {
		return nil, 0, fault.Field("basis", "未知观测口径")
	}
	if _, err := GetProject(ctx, q, projectID); err != nil {
		return nil, 0, err
	}
	where := ` WHERE sr.project_id=? AND o.observed_at>=? AND o.observed_at<=?`
	args := []any{projectID, Timestamp(filter.From), Timestamp(filter.To)}
	if filter.SourceRecordID != "" {
		where += ` AND o.source_record_id=?`
		args = append(args, filter.SourceRecordID)
	}
	if filter.Metric != "" {
		where += ` AND o.metric=?`
		args = append(args, filter.Metric)
	}
	if filter.Basis != "" {
		where += ` AND o.basis=?`
		args = append(args, filter.Basis)
	}
	cte := `WITH ranked AS (
		SELECT o.*, ROW_NUMBER() OVER (
			PARTITION BY o.source_record_id,o.metric,o.basis,
			CASE WHEN o.basis='SOURCE_DAILY' THEN o.period_key ELSE o.id END
			ORDER BY o.observed_at DESC,o.fetched_at DESC,o.id DESC
		) AS bucket_rank
		FROM observations o JOIN source_records sr ON sr.id=o.source_record_id` + where + `
	)`
	var total int
	if err := q.QueryRowContext(ctx, cte+` SELECT count(*) FROM ranked WHERE bucket_rank=1`, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	pageArgs := append(append([]any(nil), args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := q.QueryContext(ctx, cte+` SELECT `+observationColumns+` FROM ranked WHERE bucket_rank=1 ORDER BY observed_at ASC,id ASC LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Observation, 0)
	for rows.Next() {
		item, err := scanObservation(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func prepareObservation(runID, sourceRecordID string, input ObservationInput) (Observation, any, error) {
	if !isMetric(input.Metric) {
		return Observation{}, nil, fault.Field("metric", "未知指标")
	}
	if !isBasis(input.Basis) {
		return Observation{}, nil, fault.Field("basis", "未知观测口径")
	}
	if input.Value < 0 || input.Value > maxSafeInteger {
		return Observation{}, nil, fault.Field("value", "观测值需要是非负安全整数")
	}
	if input.ObservedAt.IsZero() || input.FetchedAt.IsZero() {
		return Observation{}, nil, fault.Field("observedAt", "观测及采集时间不能为空")
	}
	var providerJSON any
	if input.Basis == "CUMULATIVE" {
		if input.PeriodKey != "" || input.ProviderPeriod != nil {
			return Observation{}, nil, fault.Field("periodKey", "累计观测不能包含来源日桶")
		}
	} else {
		if input.PeriodKey == "" || utf8.RuneCountInString(input.PeriodKey) > 200 || input.ProviderPeriod == nil {
			return Observation{}, nil, fault.Field("periodKey", "日桶观测需要 periodKey 和 providerPeriod")
		}
		if input.ProviderPeriod.WeekUnix < 0 || input.ProviderPeriod.DayIndex < 0 || input.ProviderPeriod.DayIndex > 6 || input.ProviderPeriod.TimeZone != "UNSPECIFIED" {
			return Observation{}, nil, fault.Field("providerPeriod", "来源日桶字段无效")
		}
		encoded, err := json.Marshal(input.ProviderPeriod)
		if err != nil {
			return Observation{}, nil, err
		}
		providerJSON = string(encoded)
	}
	return Observation{
		ID: uuid.NewString(), SourceRecordID: sourceRecordID, RunID: runID,
		Metric: input.Metric, Basis: input.Basis, PeriodKey: input.PeriodKey,
		Value: input.Value, ObservedAt: Timestamp(input.ObservedAt), FetchedAt: Timestamp(input.FetchedAt),
		ProviderPeriod: input.ProviderPeriod,
	}, providerJSON, nil
}

func scanObservation(row scanner) (Observation, error) {
	var item Observation
	var providerJSON sql.NullString
	err := row.Scan(&item.ID, &item.SourceRecordID, &item.RunID, &item.Metric,
		&item.Basis, &item.PeriodKey, &item.Value, &item.ObservedAt, &item.FetchedAt, &providerJSON)
	if err != nil {
		return Observation{}, err
	}
	if providerJSON.Valid {
		item.ProviderPeriod = &ProviderPeriod{}
		if err = json.Unmarshal([]byte(providerJSON.String), item.ProviderPeriod); err != nil {
			return Observation{}, fmt.Errorf("stored provider period: %w", err)
		}
	}
	return item, nil
}

func sameObservation(left, right Observation) bool {
	if left.SourceRecordID != right.SourceRecordID || left.RunID != right.RunID || left.Metric != right.Metric || left.Basis != right.Basis || left.PeriodKey != right.PeriodKey || left.Value != right.Value || left.ObservedAt != right.ObservedAt || left.FetchedAt != right.FetchedAt {
		return false
	}
	if left.ProviderPeriod == nil || right.ProviderPeriod == nil {
		return left.ProviderPeriod == nil && right.ProviderPeriod == nil
	}
	return *left.ProviderPeriod == *right.ProviderPeriod
}

func isMetric(value string) bool {
	return value == "STARS" || value == "FORKS" || value == "POINTS" || value == "COMMENTS" || value == "VOTES"
}

func isBasis(value string) bool {
	return value == "CUMULATIVE" || value == "SOURCE_DAILY"
}
