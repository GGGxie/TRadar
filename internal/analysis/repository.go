package analysis

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"tradar/internal/catalog"
	"tradar/internal/fault"
	"tradar/internal/trends"
)

const versionColumns = `id,project_id,level,version,origin,project_revision,based_on_analysis_id,created_at,cutoff_at,rules_version,model_json,content_json,evidence_ids_json,evidence_snapshots_json,trend_snapshot_json`
const runColumns = `id,project_id,level,mode,status,revision,project_revision,cutoff_at,analysis_id,error_code,usage_json,created_at,finished_at`

func CreateManual(ctx context.Context, tx *sql.Tx, projectID string, input CreateInput, now time.Time) (Version, error) {
	return createVersion(ctx, tx, projectID, input, "MANUAL", nil, now)
}

func CreateRulesRun(ctx context.Context, tx *sql.Tx, projectID string, input CreateRunInput, now time.Time) (Run, error) {
	if input.Mode == "MODEL" {
		return Run{}, fault.New("MODEL_DISABLED", "模型分析尚未启用")
	}
	if input.Mode != "RULES" {
		return Run{}, fault.Field("mode", "仅支持 RULES 或 MODEL")
	}
	if !validLevel(input.Level) {
		return Run{}, fault.Field("level", "仅支持 SCREEN 或 DEEP")
	}
	if len(input.EvidenceIDs) > 50 {
		return Run{}, fault.Field("evidenceIds", "一次最多选择 50 条证据")
	}
	if duplicate(input.EvidenceIDs) {
		return Run{}, fault.Field("evidenceIds", "证据 ID 不能重复")
	}
	project, err := validateProjectRevision(ctx, tx, projectID, input.ProjectRevision)
	if err != nil {
		return Run{}, err
	}
	cutoff := now.UTC()
	evidence, err := loadEvidence(ctx, tx, projectID, input.EvidenceIDs, cutoff)
	if err != nil {
		return Run{}, err
	}
	run := Run{ID: uuid.NewString(), ProjectID: projectID, Level: input.Level, Mode: input.Mode, Status: "RUNNING", Revision: 1, ProjectRevision: input.ProjectRevision, CutoffAt: catalog.Timestamp(cutoff), CreatedAt: catalog.Timestamp(now)}
	inputJSON, _ := json.Marshal(input)
	if _, err = tx.ExecContext(ctx, `INSERT INTO analysis_runs(id,project_id,level,mode,status,revision,project_revision,cutoff_at,input_json,analysis_id,error_code,usage_json,created_at,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, run.ID, run.ProjectID, run.Level, run.Mode, run.Status, run.Revision, run.ProjectRevision, run.CutoffAt, string(inputJSON), nil, nil, nil, run.CreatedAt, nil); err != nil {
		return Run{}, err
	}
	content := rulesContent(project, input.Level, evidence)
	rulesVersion := RulesVersion
	version, err := createVersion(ctx, tx, projectID, CreateInput{Level: input.Level, ProjectRevision: input.ProjectRevision, Content: content}, "RULES", &rulesVersion, now)
	if err != nil {
		return Run{}, err
	}
	finished := catalog.Timestamp(now)
	run.Status, run.AnalysisID, run.FinishedAt, run.Revision = "SUCCEEDED", &version.ID, &finished, 2
	if _, err = tx.ExecContext(ctx, `UPDATE analysis_runs SET status=?,revision=?,analysis_id=?,finished_at=? WHERE id=?`, run.Status, run.Revision, run.AnalysisID, run.FinishedAt, run.ID); err != nil {
		return Run{}, err
	}
	return run, nil
}

func createVersion(ctx context.Context, tx *sql.Tx, projectID string, input CreateInput, origin string, rulesVersion *string, now time.Time) (Version, error) {
	project, err := validateProjectRevision(ctx, tx, projectID, input.ProjectRevision)
	if err != nil {
		return Version{}, err
	}
	content, evidenceIDs, err := validateContent(input.Level, input.Content)
	if err != nil {
		return Version{}, err
	}
	if origin == "MANUAL" && !hasAuthoredContent(content) {
		return Version{}, fault.Field("content", "人工版本至少需要写下一条声明、待验证问题或下一步检查")
	}
	if input.BasedOnAnalysisID != nil {
		base, readErr := getStoredVersion(ctx, tx, *input.BasedOnAnalysisID)
		if readErr != nil || base.ProjectID != projectID || base.Level != input.Level {
			return Version{}, fault.Field("basedOnAnalysisId", "基础版本不存在或项目、层级不匹配")
		}
	}
	cutoff := now.UTC()
	snapshots, err := loadEvidence(ctx, tx, projectID, evidenceIDs, cutoff)
	if err != nil {
		return Version{}, err
	}
	trend, err := trends.GetProjectTrend(ctx, tx, projectID, cutoff, trends.Window24Hours)
	if err != nil {
		return Version{}, err
	}
	var number int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(version),0)+1 FROM analyses WHERE project_id=? AND level=?`, projectID, input.Level).Scan(&number); err != nil {
		return Version{}, err
	}
	version := Version{ID: uuid.NewString(), ProjectID: projectID, Level: input.Level, Version: number, Origin: origin, ProjectRevision: project.Revision, BasedOnAnalysisID: input.BasedOnAnalysisID, CreatedAt: catalog.Timestamp(now), CutoffAt: catalog.Timestamp(cutoff), RulesVersion: rulesVersion, EvidenceIDs: evidenceIDs, EvidenceSnapshots: snapshots, TrendSnapshot: &trend, Content: content}
	contentJSON, _ := json.Marshal(content)
	evidenceJSON, _ := json.Marshal(evidenceIDs)
	snapshotsJSON, _ := json.Marshal(snapshots)
	trendJSON, _ := json.Marshal(trend)
	if _, err = tx.ExecContext(ctx, `INSERT INTO analyses (`+versionColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, version.ID, version.ProjectID, version.Level, version.Version, version.Origin, version.ProjectRevision, version.BasedOnAnalysisID, version.CreatedAt, version.CutoffAt, version.RulesVersion, nil, string(contentJSON), string(evidenceJSON), string(snapshotsJSON), string(trendJSON)); err != nil {
		return Version{}, err
	}
	for _, section := range content.Sections {
		for claimIndex, claim := range section.Claims {
			for _, evidenceID := range claim.EvidenceIDs {
				if _, err = tx.ExecContext(ctx, `INSERT INTO analysis_references(analysis_id,section_key,claim_index,evidence_id) VALUES(?,?,?,?)`, version.ID, section.Key, claimIndex, evidenceID); err != nil {
					return Version{}, err
				}
			}
		}
	}
	return version, nil
}

func GetVersion(ctx context.Context, q catalog.Querier, id string) (View, error) {
	version, err := getStoredVersion(ctx, q, id)
	if errors.Is(err, sql.ErrNoRows) {
		return View{}, fault.Missing("分析版本")
	}
	if err != nil {
		return View{}, err
	}
	changed, err := contextChanged(ctx, q, version)
	return View{Version: version, ContextChanged: changed}, err
}

func ListVersions(ctx context.Context, q catalog.Querier, projectID string, filter VersionFilter) ([]View, int, error) {
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return nil, 0, fault.Field("page", "分页参数无效")
	}
	if filter.Level != "" && !validLevel(filter.Level) {
		return nil, 0, fault.Field("level", "仅支持 SCREEN 或 DEEP")
	}
	if _, err := catalog.GetProject(ctx, q, projectID); err != nil {
		return nil, 0, err
	}
	where, args := ` WHERE project_id=?`, []any{projectID}
	if filter.Level != "" {
		where += ` AND level=?`
		args = append(args, filter.Level)
	}
	if filter.CutoffAt != nil {
		where += ` AND cutoff_at<=?`
		args = append(args, *filter.CutoffAt)
	}
	var total int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM analyses`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.QueryContext(ctx, `SELECT `+versionColumns+` FROM analyses`+where+` ORDER BY created_at DESC,id ASC LIMIT ? OFFSET ?`, append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)...)
	if err != nil {
		return nil, 0, err
	}
	versions := make([]Version, 0)
	for rows.Next() {
		version, scanErr := scanVersion(rows)
		if scanErr != nil {
			rows.Close()
			return nil, 0, scanErr
		}
		versions = append(versions, version)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	if err = rows.Close(); err != nil {
		return nil, 0, err
	}
	items := make([]View, 0, len(versions))
	for _, version := range versions {
		changed, changeErr := contextChanged(ctx, q, version)
		if changeErr != nil {
			return nil, 0, changeErr
		}
		items = append(items, View{Version: version, ContextChanged: changed})
	}
	return items, total, nil
}

func GetLatestVersion(ctx context.Context, q catalog.Querier, projectID, level string, cutoffAt time.Time) (View, error) {
	if !validLevel(level) {
		return View{}, fault.Field("level", "仅支持 SCREEN 或 DEEP")
	}
	var id string
	err := q.QueryRowContext(ctx, `SELECT id FROM analyses WHERE project_id=? AND level=? AND cutoff_at<=? ORDER BY version DESC LIMIT 1`, projectID, level, catalog.Timestamp(cutoffAt)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return View{}, fault.Missing("分析版本")
	}
	if err != nil {
		return View{}, err
	}
	return GetVersion(ctx, q, id)
}

func GetRun(ctx context.Context, q catalog.Querier, id string) (Run, error) {
	run, err := scanRun(q.QueryRowContext(ctx, `SELECT `+runColumns+` FROM analysis_runs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, fault.Missing("分析任务")
	}
	return run, err
}

func getStoredVersion(ctx context.Context, q catalog.Querier, id string) (Version, error) {
	return scanVersion(q.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM analyses WHERE id=?`, id))
}

func scanVersion(row interface{ Scan(...any) error }) (Version, error) {
	var item Version
	var base, rules, modelJSON, trendJSON sql.NullString
	var contentJSON, evidenceJSON, snapshotsJSON string
	if err := row.Scan(&item.ID, &item.ProjectID, &item.Level, &item.Version, &item.Origin, &item.ProjectRevision, &base, &item.CreatedAt, &item.CutoffAt, &rules, &modelJSON, &contentJSON, &evidenceJSON, &snapshotsJSON, &trendJSON); err != nil {
		return Version{}, err
	}
	item.BasedOnAnalysisID = nullString(base)
	item.RulesVersion = nullString(rules)
	if modelJSON.Valid {
		item.Model = &ModelInfo{}
		if err := json.Unmarshal([]byte(modelJSON.String), item.Model); err != nil {
			return Version{}, err
		}
	}
	if err := json.Unmarshal([]byte(contentJSON), &item.Content); err != nil {
		return Version{}, err
	}
	if err := json.Unmarshal([]byte(evidenceJSON), &item.EvidenceIDs); err != nil {
		return Version{}, err
	}
	if err := json.Unmarshal([]byte(snapshotsJSON), &item.EvidenceSnapshots); err != nil {
		return Version{}, err
	}
	if trendJSON.Valid {
		item.TrendSnapshot = &trends.TrendResult{}
		if err := json.Unmarshal([]byte(trendJSON.String), item.TrendSnapshot); err != nil {
			return Version{}, err
		}
	}
	return item, nil
}

func scanRun(row interface{ Scan(...any) error }) (Run, error) {
	var run Run
	var analysisID, errorCode, usageJSON, finished sql.NullString
	if err := row.Scan(&run.ID, &run.ProjectID, &run.Level, &run.Mode, &run.Status, &run.Revision, &run.ProjectRevision, &run.CutoffAt, &analysisID, &errorCode, &usageJSON, &run.CreatedAt, &finished); err != nil {
		return Run{}, err
	}
	run.AnalysisID, run.ErrorCode, run.FinishedAt = nullString(analysisID), nullString(errorCode), nullString(finished)
	if usageJSON.Valid {
		if err := json.Unmarshal([]byte(usageJSON.String), &run.Usage); err != nil {
			return Run{}, err
		}
	}
	return run, nil
}

func validateProjectRevision(ctx context.Context, q catalog.Querier, projectID string, expected int) (catalog.Project, error) {
	project, err := catalog.GetProject(ctx, q, projectID)
	if err != nil {
		return catalog.Project{}, err
	}
	if expected < 1 || project.Revision != expected {
		return catalog.Project{}, fault.New("PROJECT_CONTEXT_CHANGED", "项目上下文已变化，请重新加载")
	}
	return project, nil
}

// UnknownPlaceholder 是人工表单为未填写声明预置的文字（#56），不算用户写下的内容。
const UnknownPlaceholder = "尚未形成判断。"

// hasAuthoredContent 判断人工版本是否至少包含一处用户写下的内容；
// 不完整资料允许保存（SPEC-project-analysis 停止条件只作指引），完全空白的不允许。
func hasAuthoredContent(content Content) bool {
	if len(content.OpenQuestions) > 0 || len(content.NextChecks) > 0 {
		return true
	}
	for _, section := range content.Sections {
		for _, claim := range section.Claims {
			if claim.Kind != "UNKNOWN" || claim.Text != UnknownPlaceholder {
				return true
			}
		}
	}
	return false
}

func validateContent(level string, input Content) (Content, []string, error) {
	expected := sectionKeys(level)
	if expected == nil || len(input.Sections) != len(expected) {
		return Content{}, nil, fault.Field("sections", "分析层级与章节不匹配")
	}
	content := Content{Sections: make([]Section, len(input.Sections)), OpenQuestions: []string{}, NextChecks: []string{}}
	evidenceIDs, seenEvidence := []string{}, map[string]bool{}
	for index, section := range input.Sections {
		if section.Key != expected[index] || len(section.Claims) > 12 {
			return Content{}, nil, fault.Field("sections", "章节顺序、名称或声明数量无效")
		}
		content.Sections[index] = Section{Key: section.Key, Claims: make([]Claim, len(section.Claims))}
		for claimIndex, claim := range section.Claims {
			claim.Text = strings.TrimSpace(claim.Text)
			if !validClaimKind(claim.Kind) || utf8.RuneCountInString(claim.Text) < 1 || utf8.RuneCountInString(claim.Text) > 2000 {
				return Content{}, nil, fault.Field("claims", "声明类型或正文无效")
			}
			if claim.Kind == "FACT" && len(claim.EvidenceIDs) == 0 {
				return Content{}, nil, fault.Field("evidenceIds", "事实声明至少需要一条证据")
			}
			if duplicate(claim.EvidenceIDs) {
				return Content{}, nil, fault.Field("evidenceIds", "同一声明不能重复引用证据")
			}
			claim.EvidenceIDs = append([]string{}, claim.EvidenceIDs...)
			content.Sections[index].Claims[claimIndex] = claim
			for _, id := range claim.EvidenceIDs {
				if id == "" {
					return Content{}, nil, fault.Field("evidenceIds", "证据 ID 不能为空")
				}
				if !seenEvidence[id] {
					seenEvidence[id] = true
					evidenceIDs = append(evidenceIDs, id)
				}
			}
		}
	}
	var err error
	if content.OpenQuestions, err = validateTexts(input.OpenQuestions, "openQuestions"); err != nil {
		return Content{}, nil, err
	}
	if content.NextChecks, err = validateTexts(input.NextChecks, "nextChecks"); err != nil {
		return Content{}, nil, err
	}
	if len(evidenceIDs) > 50 {
		return Content{}, nil, fault.Field("evidenceIds", "分析最多引用 50 条证据")
	}
	return content, evidenceIDs, nil
}

func loadEvidence(ctx context.Context, q catalog.Querier, projectID string, ids []string, cutoff time.Time) ([]catalog.Evidence, error) {
	items := make([]catalog.Evidence, 0, len(ids))
	for _, id := range ids {
		item, err := catalog.GetEvidence(ctx, q, id)
		if err != nil || item.ProjectID != projectID {
			return nil, fault.Field("evidenceIds", "证据不存在或不属于此项目")
		}
		collected, err := time.Parse(time.RFC3339Nano, item.CollectedAt)
		if err != nil || collected.After(cutoff) {
			return nil, fault.Field("evidenceIds", "证据晚于分析截止时间")
		}
		if item.SourceRecordID != nil {
			source, sourceErr := catalog.GetSource(ctx, q, *item.SourceRecordID)
			if sourceErr != nil || source.ProjectID != projectID {
				return nil, fault.Field("evidenceIds", "证据来源已不属于此项目")
			}
		}
		items = append(items, item)
	}
	return items, nil
}

func contextChanged(ctx context.Context, q catalog.Querier, version Version) (bool, error) {
	project, err := catalog.GetProject(ctx, q, version.ProjectID)
	if err != nil {
		return false, err
	}
	if project.Revision != version.ProjectRevision {
		return true, nil
	}
	for _, evidence := range version.EvidenceSnapshots {
		if evidence.SourceRecordID == nil {
			continue
		}
		source, err := catalog.GetSource(ctx, q, *evidence.SourceRecordID)
		if err != nil {
			return false, err
		}
		if source.ProjectID != version.ProjectID {
			return true, nil
		}
	}
	return false, nil
}

func rulesContent(project catalog.Project, level string, evidence []catalog.Evidence) Content {
	keys := sectionKeys(level)
	content := Content{Sections: make([]Section, len(keys)), OpenQuestions: []string{"哪些关键主张仍缺少一手证据？"}, NextChecks: []string{"核对最影响判断的一条未知项。"}}
	for index, key := range keys {
		content.Sections[index] = Section{Key: key, Claims: []Claim{{Kind: "UNKNOWN", Text: "规则模板尚无足够证据回答此项。", EvidenceIDs: []string{}}}}
	}
	if project.Summary != "" {
		content.Sections[0].Claims = []Claim{{Kind: "HYPOTHESIS", Text: truncate("项目目录中的自述："+project.Summary, 2000), EvidenceIDs: []string{}}}
	}
	// rules-v2：来源描述说明「是什么/产品」，来源指标说明「关注/增长」，其余证据位置同 v1。
	// 只整理用户选中的证据；不根据星数推断需求、付费或留存。
	describe, attention, other := 0, 2, 2
	if level == "DEEP" {
		describe, attention, other = 1, 3, 4
	}
	grouped := map[int][]catalog.Evidence{}
	order := []int{}
	hasMetric := false
	for _, item := range evidence {
		target := other
		switch {
		case item.Kind == "METRIC_OBSERVATION":
			target, hasMetric = attention, true
		case item.SourceRecordID != nil && (item.Kind == "OFFICIAL_DESCRIPTION" || item.Kind == "PUBLIC_EXCERPT"):
			target = describe
		}
		if _, seen := grouped[target]; !seen {
			order = append(order, target)
		}
		grouped[target] = append(grouped[target], item)
	}
	for _, target := range order {
		items := grouped[target]
		// 同一节内指标观测排在前面，便于先看到关注度事实。
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].Kind == "METRIC_OBSERVATION" && items[j].Kind != "METRIC_OBSERVATION"
		})
		content.Sections[target].Claims = evidenceClaims(items)
	}
	if hasMetric {
		content.OpenQuestions = append(content.OpenQuestions, "关注度来自累计指标，是否有真实使用或付费仍需一手证据？")
	}
	return content
}

func evidenceClaims(evidence []catalog.Evidence) []Claim {
	claims := make([]Claim, 0, min(len(evidence), 12))
	for index, item := range evidence {
		if index == 11 && len(evidence) > 12 {
			ids := make([]string, 0, len(evidence)-11)
			for _, rest := range evidence[11:] {
				ids = append(ids, rest.ID)
			}
			claims = append(claims, Claim{Kind: "FACT", Text: fmt.Sprintf("另有 %d 条选定证据已冻结，需逐条核对其主张。", len(ids)), EvidenceIDs: ids})
			break
		}
		label := "证据"
		switch {
		case item.Kind == "FOUNDER_CLAIM":
			label = "创始人自述证据（未独立核实）"
		case item.Kind == "THIRD_PARTY_ESTIMATE":
			label = "第三方估计证据（未独立核实）"
		case item.Kind == "METRIC_OBSERVATION":
			label = "来源指标"
		case item.SourceRecordID != nil:
			label = "来源记录"
		}
		claims = append(claims, Claim{Kind: "FACT", Text: truncate(fmt.Sprintf("%s《%s》记录：%s", label, item.Title, item.Excerpt), 2000), EvidenceIDs: []string{item.ID}})
	}
	return claims
}

func sectionKeys(level string) []string {
	if level == "SCREEN" {
		return []string{"WHAT", "PROBLEM", "ATTENTION", "FIT"}
	}
	if level == "DEEP" {
		return []string{"NEED", "PRODUCT", "TECH", "GROWTH", "BUSINESS", "COMPETITION"}
	}
	return nil
}

func validLevel(value string) bool { return value == "SCREEN" || value == "DEEP" }
func validClaimKind(value string) bool {
	return value == "FACT" || value == "HYPOTHESIS" || value == "JUDGMENT" || value == "UNKNOWN"
}
func validateTexts(values []string, field string) ([]string, error) {
	if len(values) > 20 {
		return nil, fault.Field(field, "最多 20 条")
	}
	result := make([]string, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if n := utf8.RuneCountInString(value); n < 1 || n > 1000 {
			return nil, fault.Field(field, "每条需要 1–1000 字")
		}
		result[index] = value
	}
	return result, nil
}
func duplicate(values []string) bool {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if seen[value] {
			return true
		}
		seen[value] = true
	}
	return false
}
func truncate(value string, limit int) string {
	characters := []rune(value)
	if len(characters) <= limit {
		return value
	}
	return string(characters[:limit-1]) + "…"
}
func nullString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
