package brief

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"tradar/internal/analysis"
	"tradar/internal/catalog"
	"tradar/internal/fault"
	"tradar/internal/research"
	"tradar/internal/trends"
)

var coverageSources = []string{"GITHUB", "HACKER_NEWS", "PRODUCT_HUNT"}

// AcceptRun validates the requested business date and persists all input needed
// by the generator. Callers own the surrounding transaction.
func AcceptRun(ctx context.Context, tx *sql.Tx, input CreateInput, now time.Time) (Run, error) {
	location, err := time.LoadLocation(input.TimeZone)
	if err != nil || strings.TrimSpace(input.TimeZone) == "" {
		return Run{}, fault.Field("timeZone", "需要有效的 IANA 时区")
	}
	if _, err = time.Parse("2006-01-02", input.Date); err != nil || input.Date != now.In(location).Format("2006-01-02") {
		return Run{}, fault.Field("date", "只能生成该时区的今日日报")
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM brief_runs WHERE date=? AND time_zone=? AND status IN ('PENDING','RUNNING')`, input.Date, input.TimeZone).Scan(&active); err != nil {
		return Run{}, err
	}
	if active != 0 {
		return Run{}, fault.New("BRIEF_ALREADY_RUNNING", "该日期和时区已有日报生成任务")
	}

	cutoff := now.UTC()
	frozen, err := freezeInput(ctx, tx, input, cutoff, location)
	if err != nil {
		return Run{}, err
	}
	body, err := json.Marshal(frozen)
	if err != nil {
		return Run{}, err
	}
	run := Run{
		ID: uuid.NewString(), Status: "PENDING", Revision: 1,
		Date: input.Date, TimeZone: input.TimeZone, CutoffAt: catalog.Timestamp(cutoff),
		CreatedAt: catalog.Timestamp(now),
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO brief_runs(id,status,revision,date,time_zone,cutoff_at,brief_id,error_code,input_json,created_at,finished_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		run.ID, run.Status, run.Revision, run.Date, run.TimeZone, run.CutoffAt, nil, nil, string(body), run.CreatedAt, nil)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return Run{}, fault.New("BRIEF_ALREADY_RUNNING", "该日期和时区已有日报生成任务")
		}
		return Run{}, err
	}
	return run, nil
}

func freezeInput(ctx context.Context, tx *sql.Tx, input CreateInput, cutoff time.Time, location *time.Location) (FrozenInput, error) {
	settings, err := GetSettings(ctx, tx)
	if err != nil {
		return FrozenInput{}, err
	}
	preferences, err := research.GetPreferences(ctx, tx)
	if err != nil {
		return FrozenInput{}, err
	}
	previousID, previousCutoff, err := latestPreviousBrief(ctx, tx, input.TimeZone, cutoff)
	if err != nil {
		return FrozenInput{}, err
	}
	discoveryStart := cutoff.Add(-24 * time.Hour)
	if previousCutoff != nil {
		discoveryStart = *previousCutoff
	}
	coverage, err := freezeCoverage(ctx, tx, cutoff)
	if err != nil {
		return FrozenInput{}, err
	}
	projects, err := allProjects(ctx, tx)
	if err != nil {
		return FrozenInput{}, err
	}

	candidates := make([]CandidateSnapshot, 0, len(projects))
	for _, project := range projects {
		entry, entryErr := research.GetEntry(ctx, tx, project.ID)
		if entryErr != nil && !isNotFound(entryErr) {
			return FrozenInput{}, entryErr
		}
		var researchSnapshot *ResearchSnapshot
		if entryErr == nil {
			researchSnapshot = &ResearchSnapshot{
				Revision: entry.Revision, IsStarred: entry.IsStarred, Stage: entry.Stage,
				Decision: entry.Decision, NextAction: entry.NextAction,
			}
			if entry.Stage == "ARCHIVED" || entry.Decision == "PASS" {
				continue
			}
		}
		if !matchesFocus(project, preferences.FocusTopics) && (researchSnapshot == nil || !researchSnapshot.IsStarred) {
			continue
		}
		trend, trendErr := trends.GetProjectTrend(ctx, tx, project.ID, cutoff, trends.Window24Hours)
		if trendErr != nil {
			return FrozenInput{}, trendErr
		}
		evidence, evidenceErr := freezeEvidence(ctx, tx, project.ID, cutoff)
		if evidenceErr != nil {
			return FrozenInput{}, evidenceErr
		}
		analysisSnapshot, analysisErr := freezeAnalysis(ctx, tx, project.ID, cutoff)
		if analysisErr != nil {
			return FrozenInput{}, analysisErr
		}
		lastItem, seen, itemErr := latestRecentItem(ctx, tx, project.ID, input.TimeZone, input.Date, cutoff, location)
		if itemErr != nil {
			return FrozenInput{}, itemErr
		}
		candidates = append(candidates, CandidateSnapshot{
			Project: project, Trend: trend, Evidence: evidence, Analysis: analysisSnapshot,
			Research: researchSnapshot, SeenRecently: seen, LastItem: lastItem,
		})
	}
	return FrozenInput{
		Date: input.Date, TimeZone: input.TimeZone, CutoffAt: catalog.Timestamp(cutoff),
		DiscoveryStartAt: catalog.Timestamp(discoveryStart), MaxItems: settings.MaxItems,
		HasData: len(projects) != 0, PreviousBriefID: previousID,
		Candidates: candidates, Coverage: coverage,
	}, nil
}

func allProjects(ctx context.Context, q catalog.Querier) ([]catalog.Project, error) {
	items := make([]catalog.Project, 0)
	for page := 1; ; page++ {
		batch, total, err := catalog.ListProjects(ctx, q, catalog.ProjectFilter{Page: page, PageSize: 100})
		if err != nil {
			return nil, err
		}
		items = append(items, batch...)
		if len(items) >= total {
			return items, nil
		}
	}
}

func freezeEvidence(ctx context.Context, q catalog.Querier, projectID string, cutoff time.Time) ([]catalog.Evidence, error) {
	items := make([]catalog.Evidence, 0)
	for page := 1; ; page++ {
		// 只冻结截止时刻的当前版本；已被修订替代的旧版不再作为事实来源（#46）。
		batch, total, err := catalog.ListEvidence(ctx, q, projectID, catalog.EvidenceFilter{AsOf: &cutoff, CurrentOnly: true, Page: page, PageSize: 100})
		if err != nil {
			return nil, err
		}
		for _, item := range batch {
			if item.Kind == "METRIC_OBSERVATION" {
				continue
			}
			if item.SourceRecordID != nil {
				var currentOwner string
				err = q.QueryRowContext(ctx, `SELECT project_id FROM source_records WHERE id=?`, *item.SourceRecordID).Scan(&currentOwner)
				if errors.Is(err, sql.ErrNoRows) || (err == nil && currentOwner != projectID) {
					continue
				}
				if err != nil {
					return nil, err
				}
			}
			items = append(items, item)
		}
		if page*100 >= total {
			return items, nil
		}
	}
}

func freezeAnalysis(ctx context.Context, q catalog.Querier, projectID string, cutoff time.Time) (*AnalysisSnapshot, error) {
	for _, level := range []string{"DEEP", "SCREEN"} {
		view, err := analysis.GetLatestVersion(ctx, q, projectID, level, cutoff)
		if isNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		return &AnalysisSnapshot{ID: view.ID, Level: view.Level, Origin: view.Origin, Content: cloneContent(view.Content)}, nil
	}
	return nil, nil
}

func freezeCoverage(ctx context.Context, q catalog.Querier, cutoff time.Time) ([]Coverage, error) {
	result := make([]Coverage, 0, len(coverageSources))
	for _, source := range coverageSources {
		var latest sql.NullString
		if err := q.QueryRowContext(ctx, `SELECT max(fetched_at) FROM source_records WHERE source=? AND fetched_at<=?`, source, catalog.Timestamp(cutoff)).Scan(&latest); err != nil {
			return nil, err
		}
		item := Coverage{Source: source, State: "NO_DATA"}
		if latest.Valid {
			value := latest.String
			item.LatestCollectedAt = &value
			parsed, err := time.Parse(time.RFC3339Nano, value)
			if err != nil {
				return nil, err
			}
			item.State = "AVAILABLE"
			if parsed.Before(cutoff.Add(-24 * time.Hour)) {
				item.State = "STALE"
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func latestPreviousBrief(ctx context.Context, q catalog.Querier, timeZone string, cutoff time.Time) (*string, *time.Time, error) {
	var id, cutoffText string
	err := q.QueryRowContext(ctx, `SELECT id,cutoff_at FROM daily_briefs WHERE time_zone=? AND cutoff_at<? ORDER BY cutoff_at DESC,version DESC,id ASC LIMIT 1`, timeZone, catalog.Timestamp(cutoff)).Scan(&id, &cutoffText)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	parsed, err := time.Parse(time.RFC3339Nano, cutoffText)
	if err != nil {
		return nil, nil, err
	}
	return &id, &parsed, nil
}

func latestRecentItem(ctx context.Context, q catalog.Querier, projectID, timeZone, date string, cutoff time.Time, location *time.Location) (*BriefItem, bool, error) {
	localDate, err := time.ParseInLocation("2006-01-02", date, location)
	if err != nil {
		return nil, false, err
	}
	start := localDate.AddDate(0, 0, -6).Format("2006-01-02")
	var body string
	err = q.QueryRowContext(ctx, `SELECT bi.item_json FROM brief_items bi JOIN daily_briefs b ON b.id=bi.brief_id WHERE bi.project_id=? AND b.time_zone=? AND b.date>=? AND b.date<=? AND b.cutoff_at<? ORDER BY b.cutoff_at DESC,b.version DESC,b.id ASC LIMIT 1`, projectID, timeZone, start, date, catalog.Timestamp(cutoff)).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var item BriefItem
	if err = json.Unmarshal([]byte(body), &item); err != nil {
		return nil, false, err
	}
	normalizeBriefItem(&item)
	return &item, true, nil
}

func matchesFocus(project catalog.Project, topics []string) bool {
	if len(topics) == 0 {
		return true
	}
	haystacks := []string{normalizeTopic(project.Category), normalizeTopic(project.Name), normalizeTopic(project.Summary)}
	categoryAliases := map[string][]string{
		"AI_AGENT": {"aiagent", "智能体"},
		"DEVTOOLS": {"devtools", "developertools", "开发者工具", "开发工具"},
		"SAAS":     {"saas", "软件即服务"},
	}
	for _, alias := range categoryAliases[project.Category] {
		haystacks = append(haystacks, normalizeTopic(alias))
	}
	for _, topic := range topics {
		needle := normalizeTopic(topic)
		if needle == "" {
			continue
		}
		for _, haystack := range haystacks {
			if haystack != "" && strings.Contains(haystack, needle) {
				return true
			}
		}
	}
	return false
}

func normalizeTopic(value string) string {
	value = strings.ToLower(value)
	return strings.NewReplacer(" ", "", "_", "", "-", "", "/", "").Replace(value)
}

func isNotFound(err error) bool {
	var app *fault.Error
	return errors.As(err, &app) && app.Code == "NOT_FOUND"
}
