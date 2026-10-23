package research

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"tradar/internal/storage"
)

func TestRhythmCountsOnlyTimestampedResearchFacts(t *testing.T) {
	db := researchDB(t)
	ctx := context.Background()
	shanghai, _ := time.LoadLocation("Asia/Shanghai")
	// 2026-10-23 是周五；上海时间 10:00。
	now := time.Date(2026, 10, 23, 2, 0, 0, 0, time.UTC)
	starred := true
	a := createProject(t, db, "starred-today", now)
	createEntry(t, db, a.ID, EntryPatch{IsStarred: &starred}, now.Add(-time.Hour))
	b := createProject(t, db, "starred-yesterday", now)
	createEntry(t, db, b.ID, EntryPatch{IsStarred: &starred}, now.Add(-26*time.Hour))
	c := createProject(t, db, "starred-later", now)
	entry := createEntry(t, db, c.ID, EntryPatch{}, now.Add(-30*time.Hour))
	// 旧记录今天才被收藏：按收藏时刻计入今天。
	if err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, err := UpdateEntry(ctx, tx, c.ID, entry.Revision, EntryPatch{IsStarred: &starred}, now.Add(-10*time.Minute))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// 修改阶段不算深研；只有 DEEP 分析版本才算。
	stage := "DEEP_RESEARCH"
	d := createProject(t, db, "stage-only", now)
	createEntry(t, db, d.ID, EntryPatch{Stage: &stage}, now)
	insertDeepAnalysis(t, db, a.ID, now.Add(-2*24*time.Hour)) // 周三：本周
	insertDeepAnalysis(t, db, a.ID, now.Add(-time.Hour))      // 同一项目只计一次
	insertDeepAnalysis(t, db, b.ID, now.Add(-6*24*time.Hour)) // 上周六：不计
	insertRulesDeepAnalysis(t, db, d.ID, now.Add(-time.Hour)) // 一键规则模板不算深研
	insertExperiment(t, db, b.ID, now.Add(-20*24*time.Hour))  // 10 月 3 日：本月
	insertExperiment(t, db, c.ID, now.Add(-25*24*time.Hour))  // 9 月 28 日：上月

	rhythm, err := GetRhythm(ctx, db, now, shanghai)
	if err != nil {
		t.Fatal(err)
	}
	if rhythm.DailySaved != 2 || rhythm.WeeklyDeep != 1 || rhythm.MonthlyPractice != 1 {
		t.Fatalf("rhythm=%+v", rhythm)
	}
	if rhythm.DayStart != "2026-10-22T16:00:00.000000000Z" || rhythm.WeekStart != "2026-10-18T16:00:00.000000000Z" || rhythm.MonthStart != "2026-09-30T16:00:00.000000000Z" {
		t.Fatalf("period starts=%+v", rhythm)
	}
	if rhythm.DailySaveTarget != 3 || rhythm.WeeklyDeepTarget != 2 || rhythm.MonthlyPracticeTarget != 1 {
		t.Fatalf("targets=%+v", rhythm)
	}
	// 取消收藏后不再计入今日收藏。
	current, _ := GetEntry(ctx, db, c.ID)
	unstar := false
	if err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, err := UpdateEntry(ctx, tx, c.ID, current.Revision, EntryPatch{IsStarred: &unstar}, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if rhythm, _ = GetRhythm(ctx, db, now, shanghai); rhythm.DailySaved != 1 {
		t.Fatalf("after unstar=%+v", rhythm)
	}
}

func insertDeepAnalysis(t *testing.T, db *sql.DB, projectID string, at time.Time) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO analyses(id,project_id,level,version,origin,project_revision,created_at,cutoff_at,content_json,evidence_ids_json,evidence_snapshots_json,trend_snapshot_json)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, projectID+at.String(), projectID, "DEEP", at.Unix()%100000+1, "MANUAL", 1,
		at.UTC().Format("2006-01-02T15:04:05.000000000Z"), at.UTC().Format("2006-01-02T15:04:05.000000000Z"), `{"sections":[],"openQuestions":[],"nextChecks":[]}`, `[]`, `[]`, `{}`)
	if err != nil {
		t.Fatal(err)
	}
}

func insertRulesDeepAnalysis(t *testing.T, db *sql.DB, projectID string, at time.Time) {
	t.Helper()
	stamp := at.UTC().Format("2006-01-02T15:04:05.000000000Z")
	_, err := db.Exec(`INSERT INTO analyses(id,project_id,level,version,origin,project_revision,created_at,cutoff_at,rules_version,content_json,evidence_ids_json,evidence_snapshots_json,trend_snapshot_json)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, "rules-"+projectID, projectID, "DEEP", 99999, "RULES", 1, stamp, stamp, "rules-v2", `{"sections":[],"openQuestions":[],"nextChecks":[]}`, `[]`, `[]`, `{}`)
	if err != nil {
		t.Fatal(err)
	}
}

func insertExperiment(t *testing.T, db *sql.DB, projectID string, at time.Time) {
	t.Helper()
	stamp := at.UTC().Format("2006-01-02T15:04:05.000000000Z")
	_, err := db.Exec(`INSERT INTO experiments(id,project_id,title,hypothesis,method,status,time_budget_minutes,result,evidence_ids_json,artifact_urls_json,next_action,revision,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, projectID+stamp, projectID, "试用", "", "", "PLANNED", 180, "", `[]`, `[]`, "", 1, stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
}
