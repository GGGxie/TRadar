package research

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"tradar/internal/catalog"
	"tradar/internal/fault"
)

const experimentColumns = `id,project_id,title,hypothesis,method,status,time_budget_minutes,actual_minutes,result,outcome,evidence_ids_json,artifact_urls_json,next_action,revision,created_at,updated_at`

func CreateExperiment(ctx context.Context, q catalog.Querier, projectID string, patch ExperimentPatch, now time.Time) (Experiment, error) {
	if _, err := catalog.GetProject(ctx, q, projectID); err != nil {
		return Experiment{}, err
	}
	experiment := Experiment{
		ID: uuid.NewString(), ProjectID: projectID, Status: "PLANNED",
		TimeBudgetMinutes: 180, EvidenceIDs: []string{}, ArtifactURLs: []string{},
		Revision: 1, CreatedAt: catalog.Timestamp(now), UpdatedAt: catalog.Timestamp(now),
	}
	applyExperimentPatch(&experiment, patch)
	if err := validateExperiment(ctx, q, &experiment); err != nil {
		return Experiment{}, err
	}
	evidence, _ := json.Marshal(experiment.EvidenceIDs)
	artifacts, _ := json.Marshal(experiment.ArtifactURLs)
	_, err := q.ExecContext(ctx, `INSERT INTO experiments (`+experimentColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		experiment.ID, experiment.ProjectID, experiment.Title, experiment.Hypothesis, experiment.Method,
		experiment.Status, experiment.TimeBudgetMinutes, experiment.ActualMinutes, experiment.Result,
		experiment.Outcome, string(evidence), string(artifacts), experiment.NextAction, experiment.Revision,
		experiment.CreatedAt, experiment.UpdatedAt)
	if err != nil {
		return Experiment{}, err
	}
	return experiment, nil
}

func GetExperiment(ctx context.Context, q catalog.Querier, id string) (Experiment, error) {
	experiment, err := scanExperiment(q.QueryRowContext(ctx, `SELECT `+experimentColumns+` FROM experiments WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Experiment{}, fault.Missing("实践记录")
	}
	return experiment, err
}

func ListExperiments(ctx context.Context, q catalog.Querier, projectID string, filter ExperimentFilter) ([]Experiment, int, error) {
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return nil, 0, fault.Field("page", "分页参数无效")
	}
	if filter.Status != "" && !validExperimentStatus(filter.Status) {
		return nil, 0, fault.Field("status", "未知实践状态")
	}
	if _, err := catalog.GetProject(ctx, q, projectID); err != nil {
		return nil, 0, err
	}
	where := ` WHERE project_id=?`
	args := []any{projectID}
	if filter.Status != "" {
		where += ` AND status=?`
		args = append(args, filter.Status)
	}
	var total int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM experiments`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.QueryContext(ctx, `SELECT `+experimentColumns+` FROM experiments`+where+` ORDER BY updated_at DESC,id ASC LIMIT ? OFFSET ?`, append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Experiment, 0)
	for rows.Next() {
		item, scanErr := scanExperiment(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func UpdateExperiment(ctx context.Context, q catalog.Querier, id string, expected int, patch ExperimentPatch, now time.Time) (Experiment, error) {
	if patch.empty() {
		return Experiment{}, fault.Field("body", "至少提供一个修改字段")
	}
	experiment, err := GetExperiment(ctx, q, id)
	if err != nil {
		return Experiment{}, err
	}
	if experiment.Revision != expected {
		return Experiment{}, fault.New("VERSION_CONFLICT", "实践记录已被修改，请加载最新版本并手工合并")
	}
	applyExperimentPatch(&experiment, patch)
	if err = validateExperiment(ctx, q, &experiment); err != nil {
		return Experiment{}, err
	}
	experiment.Revision++
	experiment.UpdatedAt = catalog.Timestamp(now)
	evidence, _ := json.Marshal(experiment.EvidenceIDs)
	artifacts, _ := json.Marshal(experiment.ArtifactURLs)
	result, err := q.ExecContext(ctx, `UPDATE experiments SET title=?,hypothesis=?,method=?,status=?,time_budget_minutes=?,actual_minutes=?,result=?,outcome=?,evidence_ids_json=?,artifact_urls_json=?,next_action=?,revision=?,updated_at=? WHERE id=? AND revision=?`,
		experiment.Title, experiment.Hypothesis, experiment.Method, experiment.Status,
		experiment.TimeBudgetMinutes, experiment.ActualMinutes, experiment.Result, experiment.Outcome,
		string(evidence), string(artifacts), experiment.NextAction, experiment.Revision, experiment.UpdatedAt,
		id, expected)
	if err != nil {
		return Experiment{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Experiment{}, err
	}
	if count == 0 {
		return Experiment{}, fault.New("VERSION_CONFLICT", "实践记录已被修改，请加载最新版本并手工合并")
	}
	return experiment, nil
}

func applyExperimentPatch(experiment *Experiment, patch ExperimentPatch) {
	if patch.Title != nil {
		experiment.Title = *patch.Title
	}
	if patch.Hypothesis != nil {
		experiment.Hypothesis = *patch.Hypothesis
	}
	if patch.Method != nil {
		experiment.Method = *patch.Method
	}
	if patch.Status != nil {
		experiment.Status = *patch.Status
	}
	if patch.TimeBudgetMinutes != nil {
		experiment.TimeBudgetMinutes = *patch.TimeBudgetMinutes
	}
	if patch.ActualMinutesSet {
		experiment.ActualMinutes = patch.ActualMinutes
	}
	if patch.Result != nil {
		experiment.Result = *patch.Result
	}
	if patch.OutcomeSet {
		experiment.Outcome = patch.Outcome
	}
	if patch.EvidenceIDs != nil {
		experiment.EvidenceIDs = append([]string{}, (*patch.EvidenceIDs)...)
	}
	if patch.ArtifactURLs != nil {
		experiment.ArtifactURLs = append([]string{}, (*patch.ArtifactURLs)...)
	}
	if patch.NextAction != nil {
		experiment.NextAction = *patch.NextAction
	}
}

func validateExperiment(ctx context.Context, q catalog.Querier, experiment *Experiment) error {
	experiment.Title = strings.TrimSpace(experiment.Title)
	if n := utf8.RuneCountInString(experiment.Title); n < 1 || n > 200 {
		return fault.Field("title", "实践标题需要 1–200 字")
	}
	for field, value := range map[string]string{
		"hypothesis": experiment.Hypothesis,
		"method":     experiment.Method,
		"result":     experiment.Result,
	} {
		if utf8.RuneCountInString(value) > 10000 {
			return fault.Field(field, "内容不能超过 10000 字")
		}
	}
	if utf8.RuneCountInString(experiment.NextAction) > 2000 {
		return fault.Field("nextAction", "下一步不能超过 2000 字")
	}
	if !validExperimentStatus(experiment.Status) {
		return fault.Field("status", "未知实践状态")
	}
	if experiment.TimeBudgetMinutes < 1 || experiment.TimeBudgetMinutes > 10080 {
		return fault.Field("timeBudgetMinutes", "时间预算需要 1–10080 分钟")
	}
	if experiment.ActualMinutes != nil && (*experiment.ActualMinutes < 0 || *experiment.ActualMinutes > 1000000) {
		return fault.Field("actualMinutes", "实际用时需要 0–1000000 分钟或 null")
	}
	if experiment.Outcome != nil && !validExperimentOutcome(*experiment.Outcome) {
		return fault.Field("outcome", "未知实践结论")
	}
	if experiment.Status == "COMPLETED" && (strings.TrimSpace(experiment.Result) == "" || experiment.Outcome == nil) {
		return fault.Field("status", "完成实践需要填写结果和结论")
	}
	if len(experiment.EvidenceIDs) > 50 {
		return fault.Field("evidenceIds", "实践最多引用 50 条证据")
	}
	seenEvidence := make(map[string]struct{}, len(experiment.EvidenceIDs))
	for _, id := range experiment.EvidenceIDs {
		if id == "" {
			return fault.Field("evidenceIds", "证据 ID 不能为空")
		}
		if _, duplicate := seenEvidence[id]; duplicate {
			return fault.Field("evidenceIds", "证据 ID 不能重复")
		}
		seenEvidence[id] = struct{}{}
		var owner string
		err := q.QueryRowContext(ctx, `SELECT project_id FROM evidence WHERE id=?`, id).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && owner != experiment.ProjectID) {
			return fault.Field("evidenceIds", "证据不存在或不属于此项目")
		}
		if err != nil {
			return err
		}
	}
	if len(experiment.ArtifactURLs) > 20 {
		return fault.Field("artifactUrls", "实践产物链接最多 20 个")
	}
	normalizedURLs := make([]string, 0, len(experiment.ArtifactURLs))
	seenURLs := make(map[string]struct{}, len(experiment.ArtifactURLs))
	for _, raw := range experiment.ArtifactURLs {
		canonical, err := catalog.NormalizeURL(raw)
		if err != nil {
			return fault.Field("artifactUrls", "产物链接需要是不含凭据的完整 HTTP(S) URL")
		}
		if _, duplicate := seenURLs[canonical]; duplicate {
			return fault.Field("artifactUrls", "产物链接不能重复")
		}
		seenURLs[canonical] = struct{}{}
		normalizedURLs = append(normalizedURLs, canonical)
	}
	experiment.ArtifactURLs = normalizedURLs
	return nil
}

func validExperimentStatus(value string) bool {
	return value == "PLANNED" || value == "RUNNING" || value == "COMPLETED" || value == "CANCELLED"
}

func validExperimentOutcome(value string) bool {
	return value == "SUPPORTED" || value == "REFUTED" || value == "INCONCLUSIVE"
}

func (patch ExperimentPatch) empty() bool {
	return patch.Title == nil && patch.Hypothesis == nil && patch.Method == nil && patch.Status == nil && patch.TimeBudgetMinutes == nil && !patch.ActualMinutesSet && patch.Result == nil && !patch.OutcomeSet && patch.EvidenceIDs == nil && patch.ArtifactURLs == nil && patch.NextAction == nil
}

func scanExperiment(row interface{ Scan(...any) error }) (Experiment, error) {
	var experiment Experiment
	var actual sql.NullInt64
	var outcome sql.NullString
	var evidenceJSON, artifactsJSON string
	if err := row.Scan(&experiment.ID, &experiment.ProjectID, &experiment.Title, &experiment.Hypothesis,
		&experiment.Method, &experiment.Status, &experiment.TimeBudgetMinutes, &actual,
		&experiment.Result, &outcome, &evidenceJSON, &artifactsJSON, &experiment.NextAction,
		&experiment.Revision, &experiment.CreatedAt, &experiment.UpdatedAt); err != nil {
		return Experiment{}, err
	}
	if actual.Valid {
		value := int(actual.Int64)
		experiment.ActualMinutes = &value
	}
	if outcome.Valid {
		experiment.Outcome = &outcome.String
	}
	if err := json.Unmarshal([]byte(evidenceJSON), &experiment.EvidenceIDs); err != nil {
		return Experiment{}, err
	}
	if err := json.Unmarshal([]byte(artifactsJSON), &experiment.ArtifactURLs); err != nil {
		return Experiment{}, err
	}
	if experiment.EvidenceIDs == nil {
		experiment.EvidenceIDs = []string{}
	}
	if experiment.ArtifactURLs == nil {
		experiment.ArtifactURLs = []string{}
	}
	return experiment, nil
}
