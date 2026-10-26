package brief

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"tradar/internal/catalog"
	"tradar/internal/fault"
	"tradar/internal/storage"
	"tradar/internal/trends"
)

const runColumns = `id,status,revision,date,time_zone,cutoff_at,brief_id,error_code,created_at,finished_at`
const briefColumns = `id,date,time_zone,version,cutoff_at,generated_at,previous_brief_id,selection_version,ranking_version,is_demo,coverage_json,empty_reason,omitted_item_count`

func ProcessRun(ctx context.Context, db *sql.DB, runID string, generatedAt time.Time) (Run, error) {
	var completed Run
	err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		run, inputJSON, err := getStoredRun(ctx, tx, runID)
		if err != nil {
			return err
		}
		if run.Status == "SUCCEEDED" {
			completed = run
			return nil
		}
		if run.Status != "PENDING" {
			return fault.New("BRIEF_RUN_FINISHED", "日报生成任务已结束")
		}
		var input FrozenInput
		if err = json.Unmarshal([]byte(inputJSON), &input); err != nil {
			return err
		}
		if err = validateFrozenInput(input, run); err != nil {
			return err
		}
		items, emptyReason, omitted := SelectItemsWithOmitted(input)
		coverageJSON, err := json.Marshal(input.Coverage)
		if err != nil {
			return err
		}
		var version int
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(version),0)+1 FROM daily_briefs WHERE date=? AND time_zone=?`, run.Date, run.TimeZone).Scan(&version); err != nil {
			return err
		}
		brief := DailyBrief{
			ID: uuid.NewString(), Date: run.Date, TimeZone: run.TimeZone, Version: version,
			CutoffAt: run.CutoffAt, GeneratedAt: catalog.Timestamp(generatedAt),
			PreviousBriefID: input.PreviousBriefID, SelectionVersion: SelectionVersion,
			RankingVersion: trends.AlgorithmVersion, IsDemo: false,
			Items: items, Coverage: append([]Coverage{}, input.Coverage...), EmptyReason: emptyReason,
			OmittedItemCount: omitted,
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO daily_briefs(`+briefColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			brief.ID, brief.Date, brief.TimeZone, brief.Version, brief.CutoffAt, brief.GeneratedAt,
			brief.PreviousBriefID, brief.SelectionVersion, brief.RankingVersion, brief.IsDemo,
			string(coverageJSON), brief.EmptyReason, brief.OmittedItemCount)
		if err != nil {
			return err
		}
		for ordinal, item := range brief.Items {
			body, marshalErr := json.Marshal(item)
			if marshalErr != nil {
				return marshalErr
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO brief_items(brief_id,project_id,ordinal,item_json) VALUES(?,?,?,?)`, brief.ID, item.ProjectID, ordinal, string(body)); err != nil {
				return err
			}
		}
		finished := catalog.Timestamp(generatedAt)
		result, err := tx.ExecContext(ctx, `UPDATE brief_runs SET status='SUCCEEDED',revision=revision+1,brief_id=?,error_code=NULL,finished_at=? WHERE id=? AND status='PENDING' AND revision=?`, brief.ID, finished, run.ID, run.Revision)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return fault.New("VERSION_CONFLICT", "日报生成任务状态已变化")
		}
		briefID := brief.ID
		completed = run
		completed.Status, completed.Revision, completed.BriefID = "SUCCEEDED", run.Revision+1, &briefID
		completed.ErrorCode, completed.FinishedAt = nil, &finished
		return nil
	})
	if err == nil {
		return completed, nil
	}
	if isNotFound(err) {
		return Run{}, err
	}
	failedAt := catalog.Timestamp(generatedAt)
	_, updateErr := db.ExecContext(ctx, `UPDATE brief_runs SET status='FAILED',revision=revision+1,error_code='GENERATION_FAILED',finished_at=? WHERE id=? AND status IN ('PENDING','RUNNING')`, failedAt, runID)
	if updateErr != nil {
		return Run{}, errors.Join(err, updateErr)
	}
	return Run{}, err
}

func validateFrozenInput(input FrozenInput, run Run) error {
	if input.Date != run.Date || input.TimeZone != run.TimeZone || input.CutoffAt != run.CutoffAt {
		return fmt.Errorf("frozen brief identity does not match run")
	}
	cutoff, err := time.Parse(time.RFC3339Nano, input.CutoffAt)
	if err != nil {
		return fmt.Errorf("invalid frozen cutoff: %w", err)
	}
	start, err := time.Parse(time.RFC3339Nano, input.DiscoveryStartAt)
	if err != nil || start.After(cutoff) {
		return fmt.Errorf("invalid frozen discovery window")
	}
	if input.MaxItems < 1 || input.MaxItems > 20 || input.Candidates == nil || input.Coverage == nil {
		return fmt.Errorf("invalid frozen collection settings")
	}
	if len(input.Coverage) != len(coverageSources) {
		return fmt.Errorf("invalid frozen coverage")
	}
	for index, coverage := range input.Coverage {
		if coverage.Source != coverageSources[index] || (coverage.State != "AVAILABLE" && coverage.State != "STALE" && coverage.State != "NO_DATA") {
			return fmt.Errorf("invalid frozen coverage entry")
		}
	}
	seenProjects := make(map[string]bool, len(input.Candidates))
	for _, candidate := range input.Candidates {
		if candidate.Project.ID == "" || candidate.Trend.ProjectID != candidate.Project.ID || seenProjects[candidate.Project.ID] {
			return fmt.Errorf("invalid frozen candidate")
		}
		seenProjects[candidate.Project.ID] = true
	}
	return nil
}

func GetRun(ctx context.Context, q catalog.Querier, id string) (Run, error) {
	run, _, err := getStoredRun(ctx, q, id)
	return run, err
}

func getStoredRun(ctx context.Context, q catalog.Querier, id string) (Run, string, error) {
	var run Run
	var briefID, errorCode, finished sql.NullString
	var inputJSON string
	err := q.QueryRowContext(ctx, `SELECT `+runColumns+`,input_json FROM brief_runs WHERE id=?`, id).Scan(
		&run.ID, &run.Status, &run.Revision, &run.Date, &run.TimeZone, &run.CutoffAt,
		&briefID, &errorCode, &run.CreatedAt, &finished, &inputJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, "", fault.Missing("日报生成任务")
	}
	if err != nil {
		return Run{}, "", err
	}
	run.BriefID, run.ErrorCode, run.FinishedAt = nullableString(briefID), nullableString(errorCode), nullableString(finished)
	return run, inputJSON, nil
}

func GetBrief(ctx context.Context, q catalog.Querier, id string) (DailyBrief, error) {
	brief, err := scanBrief(q.QueryRowContext(ctx, `SELECT `+briefColumns+` FROM daily_briefs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return DailyBrief{}, fault.Missing("日报")
	}
	if err != nil {
		return DailyBrief{}, err
	}
	items, err := loadBriefItems(ctx, q, brief.ID)
	if err != nil {
		return DailyBrief{}, err
	}
	brief.Items = items
	return brief, nil
}

func ListBriefs(ctx context.Context, q catalog.Querier, filter BriefFilter) ([]DailyBrief, int, error) {
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return nil, 0, fault.Field("page", "分页参数无效")
	}
	where, args := ` WHERE 1=1`, []any{}
	if filter.Date != "" {
		if parsed, err := time.Parse("2006-01-02", filter.Date); err != nil || parsed.Format("2006-01-02") != filter.Date {
			return nil, 0, fault.Field("date", "需要 YYYY-MM-DD 日期")
		}
		where += ` AND date=?`
		args = append(args, filter.Date)
	}
	if filter.TimeZone != "" {
		if _, err := time.LoadLocation(filter.TimeZone); err != nil {
			return nil, 0, fault.Field("timeZone", "需要有效的 IANA 时区")
		}
		where += ` AND time_zone=?`
		args = append(args, filter.TimeZone)
	}
	var total int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM daily_briefs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.QueryContext(ctx, `SELECT `+briefColumns+` FROM daily_briefs`+where+` ORDER BY date DESC,version DESC,id ASC LIMIT ? OFFSET ?`, append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)...)
	if err != nil {
		return nil, 0, err
	}
	items := make([]DailyBrief, 0)
	for rows.Next() {
		item, scanErr := scanBrief(rows)
		if scanErr != nil {
			rows.Close()
			return nil, 0, scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	if err = rows.Close(); err != nil {
		return nil, 0, err
	}
	for index := range items {
		items[index].Items, err = loadBriefItems(ctx, q, items[index].ID)
		if err != nil {
			return nil, 0, err
		}
	}
	return items, total, nil
}

func scanBrief(row interface{ Scan(...any) error }) (DailyBrief, error) {
	var item DailyBrief
	var previous, emptyReason sql.NullString
	var coverageJSON string
	if err := row.Scan(&item.ID, &item.Date, &item.TimeZone, &item.Version, &item.CutoffAt, &item.GeneratedAt,
		&previous, &item.SelectionVersion, &item.RankingVersion, &item.IsDemo, &coverageJSON, &emptyReason, &item.OmittedItemCount); err != nil {
		return DailyBrief{}, err
	}
	item.PreviousBriefID, item.EmptyReason = nullableString(previous), nullableString(emptyReason)
	if err := json.Unmarshal([]byte(coverageJSON), &item.Coverage); err != nil {
		return DailyBrief{}, err
	}
	if item.Coverage == nil {
		item.Coverage = []Coverage{}
	}
	item.Items = []BriefItem{}
	return item, nil
}

func loadBriefItems(ctx context.Context, q catalog.Querier, briefID string) ([]BriefItem, error) {
	rows, err := q.QueryContext(ctx, `SELECT item_json FROM brief_items WHERE brief_id=? ORDER BY ordinal ASC`, briefID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]BriefItem, 0)
	for rows.Next() {
		var body string
		if err = rows.Scan(&body); err != nil {
			return nil, err
		}
		var item BriefItem
		if err = json.Unmarshal([]byte(body), &item); err != nil {
			return nil, err
		}
		normalizeBriefItem(&item)
		items = append(items, item)
	}
	return items, rows.Err()
}

func normalizeBriefItem(item *BriefItem) {
	if item.Reasons == nil {
		item.Reasons = []string{}
	}
	if item.EvidenceIDs == nil {
		item.EvidenceIDs = []string{}
	}
	if item.EvidenceSnapshots == nil {
		item.EvidenceSnapshots = []catalog.Evidence{}
	}
}

func nullableString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
