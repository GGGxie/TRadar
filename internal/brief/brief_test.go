package brief

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/fault"
	"tradar/internal/research"
	"tradar/internal/storage"
)

func TestEmptyBriefVersionsAndFailedRunsRemainQueryable(t *testing.T) {
	db := briefDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC) // 09:00 Asia/Shanghai
	first := acceptRun(t, db, CreateInput{Date: "2026-10-16", TimeZone: "Asia/Shanghai"}, now)
	if first.Status != "PENDING" || first.CutoffAt != catalog.Timestamp(now) {
		t.Fatalf("accepted=%+v", first)
	}
	if _, err := ProcessRun(ctx, db, first.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	completed, err := GetRun(ctx, db, first.ID)
	if err != nil || completed.Status != "SUCCEEDED" || completed.BriefID == nil {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	brief, err := GetBrief(ctx, db, *completed.BriefID)
	if err != nil || brief.Version != 1 || brief.EmptyReason == nil || *brief.EmptyReason != "NO_DATA" || len(brief.Items) != 0 || brief.PreviousBriefID != nil {
		t.Fatalf("brief=%+v err=%v", brief, err)
	}
	second := acceptRun(t, db, CreateInput{Date: "2026-10-16", TimeZone: "Asia/Shanghai"}, now.Add(time.Hour))
	if _, err = ProcessRun(ctx, db, second.ID, now.Add(time.Hour+time.Second)); err != nil {
		t.Fatal(err)
	}
	secondRun, _ := GetRun(ctx, db, second.ID)
	secondBrief, err := GetBrief(ctx, db, *secondRun.BriefID)
	if err != nil || secondBrief.Version != 2 || secondBrief.PreviousBriefID == nil || *secondBrief.PreviousBriefID != brief.ID {
		t.Fatalf("second=%+v err=%v", secondBrief, err)
	}
	items, total, err := ListBriefs(ctx, db, BriefFilter{Date: "2026-10-16", TimeZone: "Asia/Shanghai", Page: 1, PageSize: 20})
	if err != nil || total != 2 || len(items) != 2 || items[0].Version != 2 || items[1].Version != 1 {
		t.Fatalf("items=%+v total=%d err=%v", items, total, err)
	}
	active := acceptRun(t, db, CreateInput{Date: "2026-10-16", TimeZone: "Asia/Shanghai"}, now.Add(2*time.Hour))
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, acceptErr := AcceptRun(ctx, tx, CreateInput{Date: "2026-10-16", TimeZone: "Asia/Shanghai"}, now.Add(2*time.Hour))
		return acceptErr
	})
	if briefFaultCode(err) != "BRIEF_ALREADY_RUNNING" {
		t.Fatalf("active conflict=%v", err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE brief_runs SET input_json='{}' WHERE id=?`, active.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = ProcessRun(ctx, db, active.ID, now.Add(2*time.Hour+time.Second)); err == nil {
		t.Fatal("expected corrupt frozen input to fail")
	}
	failed, err := GetRun(ctx, db, active.ID)
	if err != nil || failed.Status != "FAILED" || failed.ErrorCode == nil || failed.BriefID != nil {
		t.Fatalf("failed=%+v err=%v", failed, err)
	}
}

func TestAcceptedBriefInputStaysFrozenAcrossLaterEvidenceAndResearchChanges(t *testing.T) {
	db := briefDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 2, 0, 0, 0, time.UTC)
	project := createBriefProject(t, db, "Frozen Agent", "AI_AGENT", now.Add(-time.Hour))
	founder := createBriefEvidence(t, db, project.ID, "收入说明", "创始人称已有付费用户", "FOUNDER_CLAIM", now.Add(-30*time.Minute))
	decision, next := "OBSERVE", "核对付费留存"
	entry := createBriefResearch(t, db, project.ID, research.EntryPatch{Decision: &decision, NextAction: &next}, now.Add(-20*time.Minute))
	upsertBriefSource(t, db, project.ID, "GITHUB", "fresh-source", now.Add(-time.Hour))
	upsertBriefSource(t, db, project.ID, "HACKER_NEWS", "stale-source", now.Add(-25*time.Hour))
	run := acceptRun(t, db, CreateInput{Date: "2026-10-16", TimeZone: "Asia/Shanghai"}, now)
	newDecision := "USE"
	updatedNote := "不能进入已冻结日报"
	if err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, updateErr := research.UpdateEntry(ctx, tx, project.ID, entry.Revision, research.EntryPatch{Decision: &newDecision, Notes: &updatedNote}, now.Add(time.Minute))
		return updateErr
	}); err != nil {
		t.Fatal(err)
	}
	late := createBriefEvidence(t, db, project.ID, "后到证据", "cutoff 后才采集", "OFFICIAL_PRICING", now.Add(time.Minute))
	if _, err := ProcessRun(ctx, db, run.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	storedRun, _ := GetRun(ctx, db, run.ID)
	first, err := GetBrief(ctx, db, *storedRun.BriefID)
	if err != nil || len(first.Items) != 1 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	item := first.Items[0]
	if item.ResearchRevision == nil || *item.ResearchRevision != 1 || item.PersonalDecision == nil || *item.PersonalDecision != "OBSERVE" || len(item.EvidenceIDs) != 1 || item.EvidenceIDs[0] != founder.ID || item.FactSummary != "创始人自述：创始人称已有付费用户" {
		t.Fatalf("frozen item=%+v", item)
	}
	if item.NextAction == nil || *item.NextAction != next || containsText(item.FactSummary, updatedNote) || containsID(item.EvidenceIDs, late.ID) {
		t.Fatalf("private or late data leaked: %+v", item)
	}
	if len(first.Coverage) != 3 || first.Coverage[0].Source != "GITHUB" || first.Coverage[0].State != "AVAILABLE" ||
		first.Coverage[1].Source != "HACKER_NEWS" || first.Coverage[1].State != "STALE" ||
		first.Coverage[2].Source != "PRODUCT_HUNT" || first.Coverage[2].State != "NO_DATA" {
		t.Fatalf("coverage=%+v", first.Coverage)
	}
	secondRun := acceptRun(t, db, CreateInput{Date: "2026-10-16", TimeZone: "Asia/Shanghai"}, now.Add(time.Hour))
	if _, err = ProcessRun(ctx, db, secondRun.ID, now.Add(time.Hour+time.Second)); err != nil {
		t.Fatal(err)
	}
	storedSecond, _ := GetRun(ctx, db, secondRun.ID)
	second, err := GetBrief(ctx, db, *storedSecond.BriefID)
	if err != nil || len(second.Items) != 1 {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if !containsReason(second.Items[0].Reasons, "NEW_EVIDENCE") || !containsReason(second.Items[0].Reasons, "RESEARCH_CHANGE") {
		t.Fatalf("reasons=%v", second.Items[0].Reasons)
	}
	again, err := GetBrief(ctx, db, first.ID)
	if err != nil || again.Items[0].PersonalDecision == nil || *again.Items[0].PersonalDecision != "OBSERVE" || len(again.Items[0].EvidenceIDs) != 1 {
		t.Fatalf("old brief changed: %+v err=%v", again, err)
	}
}

func TestBriefFreezesOnlyCurrentEvidenceVersions(t *testing.T) {
	db := briefDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 0, 30, 0, 0, time.UTC)
	project := createBriefProject(t, db, "versioned", "AI_AGENT", now.Add(-time.Hour))
	original := createBriefEvidence(t, db, project.ID, "旧版", "旧版本正文", "PUBLIC_EXCERPT", now.Add(-50*time.Minute))
	var revised catalog.Evidence
	if err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		revised, err = catalog.CreateEvidence(ctx, tx, project.ID, catalog.CreateEvidenceInput{
			URL: "https://example.com/revised", Title: "新版", Excerpt: "修订后的正文", Kind: "PUBLIC_EXCERPT", SupersedesID: &original.ID,
		}, now.Add(-40*time.Minute))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	run := acceptRun(t, db, CreateInput{Date: "2026-10-16", TimeZone: "Asia/Shanghai"}, now)
	processed, err := ProcessRun(ctx, db, run.ID, now.Add(time.Second))
	if err != nil || processed.BriefID == nil {
		t.Fatalf("process=%+v err=%v", processed, err)
	}
	brief, err := GetBrief(ctx, db, *processed.BriefID)
	if err != nil || len(brief.Items) != 1 {
		t.Fatalf("brief=%+v err=%v", brief, err)
	}
	item := brief.Items[0]
	if len(item.EvidenceIDs) != 1 || item.EvidenceIDs[0] != revised.ID || item.FactSummary != "修订后的正文" {
		t.Fatalf("superseded evidence frozen: ids=%v fact=%q", item.EvidenceIDs, item.FactSummary)
	}
}

func TestAcceptRunRejectsNonTodayAndInvalidTimeZone(t *testing.T) {
	db := briefDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 2, 0, 0, 0, time.UTC)
	for _, input := range []CreateInput{
		{Date: "2026-10-15", TimeZone: "Asia/Shanghai"},
		{Date: "2026-10-16", TimeZone: "Not/A_Zone"},
	} {
		err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
			_, acceptErr := AcceptRun(ctx, tx, input, now)
			return acceptErr
		})
		if briefFaultCode(err) != "VALIDATION_ERROR" {
			t.Fatalf("input=%+v err=%v", input, err)
		}
	}
}

func TestSourceReassociationDoesNotRewriteAcceptedOrCreateNewEvidenceChange(t *testing.T) {
	db := briefDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC)
	left := createBriefProject(t, db, "Left Agent", "AI_AGENT", now.Add(-2*time.Hour))
	right := createBriefProject(t, db, "Right Project", "OTHER", now.Add(-2*time.Hour))
	var source catalog.SourceRecord
	var evidence catalog.Evidence
	if err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		source, _, err = catalog.UpsertSource(ctx, tx, catalog.UpsertSourceInput{
			ProjectID: left.ID, Source: "GITHUB", ExternalID: "reassociated", RecordType: "PROJECT",
			URL: "https://example.com/source/reassociated", Title: "Reassociated", FetchedAt: now.Add(-time.Hour),
		})
		if err != nil {
			return err
		}
		evidence, err = catalog.CreateManualEvidence(ctx, tx, left.ID, catalog.CreateEvidenceInput{
			URL: "https://example.com/reassociated/fact", Title: "Original fact", Excerpt: "belongs to the original association",
			Kind: "PUBLIC_EXCERPT", SourceRecordID: &source.ID,
		}, now.Add(-30*time.Minute))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	accepted := acceptRun(t, db, CreateInput{Date: "2026-10-16", TimeZone: "Asia/Shanghai"}, now)
	if err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, err := catalog.ReassociateSource(ctx, tx, source.ID, source.Revision, right.ID, "人工纠正归属", now.Add(time.Minute))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessRun(ctx, db, accepted.ID, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	firstRun, _ := GetRun(ctx, db, accepted.ID)
	first, err := GetBrief(ctx, db, *firstRun.BriefID)
	if err != nil || len(first.Items) != 1 || !containsID(first.Items[0].EvidenceIDs, evidence.ID) {
		t.Fatalf("accepted snapshot changed after reassociation: brief=%+v err=%v", first, err)
	}
	secondRun := acceptRun(t, db, CreateInput{Date: "2026-10-16", TimeZone: "Asia/Shanghai"}, now.Add(time.Hour))
	if _, err = ProcessRun(ctx, db, secondRun.ID, now.Add(time.Hour+time.Second)); err != nil {
		t.Fatal(err)
	}
	storedSecond, _ := GetRun(ctx, db, secondRun.ID)
	second, err := GetBrief(ctx, db, *storedSecond.BriefID)
	if err != nil || len(second.Items) != 0 || second.EmptyReason == nil || *second.EmptyReason != "NO_NEW_CHANGES" {
		t.Fatalf("removed association was presented as new evidence: brief=%+v err=%v", second, err)
	}
}

func TestBriefSettingsAreVersionedAndValidated(t *testing.T) {
	db := briefDB(t)
	ctx := context.Background()
	settings, err := GetSettings(ctx, db)
	if err != nil || settings.Revision != 1 || settings.TimeZone != "Asia/Shanghai" || settings.LocalTime != "08:30" || settings.MaxItems != 5 || !settings.IsEnabled {
		t.Fatalf("defaults=%+v err=%v", settings, err)
	}
	badZone := "Not/A_Zone"
	if _, err = UpdateSettings(ctx, db, 1, SettingsPatch{TimeZone: &badZone}); briefFaultCode(err) != "VALIDATION_ERROR" {
		t.Fatalf("invalid timezone=%v", err)
	}
	maxItems := 8
	updated, err := UpdateSettings(ctx, db, 1, SettingsPatch{MaxItems: &maxItems})
	if err != nil || updated.Revision != 2 || updated.MaxItems != 8 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	if _, err = UpdateSettings(ctx, db, 1, SettingsPatch{MaxItems: &maxItems}); briefFaultCode(err) != "VERSION_CONFLICT" {
		t.Fatalf("stale update=%v", err)
	}
}

func briefDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func acceptRun(t *testing.T, db *sql.DB, input CreateInput, now time.Time) Run {
	t.Helper()
	var run Run
	if err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		run, err = AcceptRun(context.Background(), tx, input, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return run
}

func createBriefProject(t *testing.T, db *sql.DB, name, category string, now time.Time) catalog.Project {
	t.Helper()
	var project catalog.Project
	if err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		project, _, err = catalog.CreateProject(context.Background(), tx, catalog.CreateProjectInput{Name: name, CanonicalURL: "https://example.com/" + name, Category: category}, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return project
}

func createBriefEvidence(t *testing.T, db *sql.DB, projectID, title, excerpt, kind string, now time.Time) catalog.Evidence {
	t.Helper()
	var item catalog.Evidence
	if err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		item, err = catalog.CreateManualEvidence(context.Background(), tx, projectID, catalog.CreateEvidenceInput{URL: "https://example.com/evidence/" + title, Title: title, Excerpt: excerpt, Kind: kind}, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return item
}

func createBriefResearch(t *testing.T, db *sql.DB, projectID string, patch research.EntryPatch, now time.Time) research.Entry {
	t.Helper()
	var entry research.Entry
	if err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		entry, err = research.CreateEntry(context.Background(), tx, projectID, patch, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return entry
}

func upsertBriefSource(t *testing.T, db *sql.DB, projectID, source, externalID string, fetchedAt time.Time) {
	t.Helper()
	if err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		_, _, err := catalog.UpsertSource(context.Background(), tx, catalog.UpsertSourceInput{ProjectID: projectID, Source: source, ExternalID: externalID, RecordType: "PROJECT", URL: "https://example.com/source/" + externalID, Title: externalID, FetchedAt: fetchedAt})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func briefFaultCode(err error) string {
	var app *fault.Error
	if errors.As(err, &app) {
		return app.Code
	}
	return ""
}

func containsReason(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsID(values []string, target string) bool { return containsReason(values, target) }
func containsText(value, target string) bool {
	return len(target) > 0 && strings.Contains(value, target)
}

// #59：因 maxItems 未收录的数量冻结在版本里，读取与列表都能看到。
func TestBriefRecordsCandidatesOmittedByMaxItems(t *testing.T) {
	db := briefDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 2, 0, 0, 0, time.UTC)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		project := createBriefProject(t, db, name, "AI_AGENT", now.Add(-time.Hour))
		upsertBriefSource(t, db, project.ID, "GITHUB", "src-"+name, now.Add(-30*time.Minute))
	}
	limit := 2
	if _, err := UpdateSettings(ctx, db, 1, SettingsPatch{MaxItems: &limit}); err != nil {
		t.Fatal(err)
	}
	run := acceptRun(t, db, CreateInput{Date: "2026-10-16", TimeZone: "Asia/Shanghai"}, now)
	if _, err := ProcessRun(ctx, db, run.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	stored, _ := GetRun(ctx, db, run.ID)
	brief, err := GetBrief(ctx, db, *stored.BriefID)
	if err != nil || len(brief.Items) != 2 || brief.OmittedItemCount != 1 {
		t.Fatalf("brief items=%d omitted=%d err=%v", len(brief.Items), brief.OmittedItemCount, err)
	}
	listed, _, err := ListBriefs(ctx, db, BriefFilter{Page: 1, PageSize: 20})
	if err != nil || len(listed) != 1 || listed[0].OmittedItemCount != 1 {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	if markdown := RenderMarkdown(brief); !strings.Contains(markdown, "另有 1 个符合条件的项目因上限 2 未收录") {
		t.Fatalf("markdown missing omitted note:\n%s", markdown)
	}
}
