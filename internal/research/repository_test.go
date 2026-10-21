package research

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/fault"
	"tradar/internal/storage"
)

func TestCreateEntryDefaultsAndSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "radar.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC)
	project := createProject(t, db, "restart", now)
	entry := createEntry(t, db, project.ID, EntryPatch{}, now)
	if entry.ProjectID != project.ID || entry.Revision != 1 || entry.IsStarred || entry.Stage != "SCREENING" || entry.Decision != "UNDECIDED" || entry.NextActionAt != nil || len(entry.Tags) != 0 {
		t.Fatalf("defaults=%+v", entry)
	}
	db.Close()
	db, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stored, err := GetEntry(context.Background(), db, project.ID)
	if err != nil || stored.ID != entry.ID {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	err = storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		_, createErr := CreateEntry(context.Background(), tx, project.ID, EntryPatch{}, now)
		return createErr
	})
	if faultCode(err) != "RESEARCH_ALREADY_EXISTS" {
		t.Fatalf("duplicate error=%v", err)
	}
}

func TestUpdateEntryNormalizesTagsAndRejectsStaleRevision(t *testing.T) {
	db := researchDB(t)
	now := time.Date(2026, 10, 16, 2, 0, 0, 0, time.UTC)
	project := createProject(t, db, "update", now)
	entry := createEntry(t, db, project.ID, EntryPatch{}, now)
	starred, stage, decision := true, "DEEP_RESEARCH", "VALIDATE"
	notes, rationale, next := "我的私密研究笔记", "有证据，仍需验证", "访谈三个用户"
	tags := []string{" AI Agent ", "Cafe\u0301", "Café", "OPS", "ops"}
	when := "2026-10-20T09:30:00+08:00"
	patch := EntryPatch{IsStarred: &starred, Tags: &tags, Stage: &stage, Decision: &decision, Notes: &notes, Rationale: &rationale, NextAction: &next, NextActionAt: &when, NextActionAtSet: true}
	updated := updateEntry(t, db, project.ID, entry.Revision, patch, now.Add(time.Minute))
	if updated.Revision != 2 || len(updated.Tags) != 3 || updated.Tags[0] != "AI Agent" || updated.Tags[1] != "Café" || updated.Tags[2] != "OPS" || updated.NextActionAt == nil || *updated.NextActionAt != "2026-10-20T01:30:00.000000000Z" {
		t.Fatalf("updated=%+v", updated)
	}
	overwrite := "不应覆盖服务器内容"
	err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		_, updateErr := UpdateEntry(context.Background(), tx, project.ID, entry.Revision, EntryPatch{Notes: &overwrite}, now.Add(2*time.Minute))
		return updateErr
	})
	if faultCode(err) != "VERSION_CONFLICT" {
		t.Fatalf("stale error=%v", err)
	}
	stored, err := GetEntry(context.Background(), db, project.ID)
	if err != nil || stored.Notes != notes {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func TestListEntriesFiltersSearchesAndProjectChangesDoNotRewriteNotes(t *testing.T) {
	db := researchDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 3, 0, 0, 0, time.UTC)
	alpha := createProject(t, db, "Alpha Tool", now)
	beta := createProject(t, db, "Beta Tool", now.Add(time.Second))
	starred := true
	tags := []string{"Agent", "商业"}
	notes := "需要核对留存"
	decision := "OBSERVE"
	createEntry(t, db, alpha.ID, EntryPatch{IsStarred: &starred, Tags: &tags, Notes: &notes, Decision: &decision}, now)
	createEntry(t, db, beta.ID, EntryPatch{}, now.Add(time.Minute))
	for name, filter := range map[string]EntryFilter{
		"starred":       {IsStarred: &starred, Page: 1, PageSize: 20},
		"tag case":      {Tag: "agent", Page: 1, PageSize: 20},
		"project name":  {Query: "Alpha", Page: 1, PageSize: 20},
		"personal body": {Query: "留存", Page: 1, PageSize: 20},
	} {
		t.Run(name, func(t *testing.T) {
			items, total, err := ListEntries(ctx, db, filter)
			if err != nil || total != 1 || len(items) != 1 || items[0].ProjectID != alpha.ID {
				t.Fatalf("items=%+v total=%d err=%v", items, total, err)
			}
		})
	}
	newSummary := "采集或模型变化不能改个人笔记"
	if _, err := catalog.UpdateProject(ctx, db, alpha.ID, alpha.Revision, catalog.ProjectPatch{Summary: &newSummary}, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	stored, err := GetEntry(ctx, db, alpha.ID)
	if err != nil || stored.Notes != notes || stored.Decision != decision || stored.Revision != 1 {
		t.Fatalf("personal content changed: %+v err=%v", stored, err)
	}
}

func TestPreferencesHaveDefaultsAndUseRevisionConflicts(t *testing.T) {
	db := researchDB(t)
	ctx := context.Background()
	preferences, err := GetPreferences(ctx, db)
	if err != nil || preferences.Revision != 1 || len(preferences.FocusTopics) != 3 || preferences.DailySaveTarget != 3 || preferences.WeeklyDeepTarget != 2 || preferences.MonthlyPracticeTarget != 1 {
		t.Fatalf("defaults=%+v err=%v", preferences, err)
	}
	topics := []string{" AI Agent ", "AI agent", "独立开发"}
	daily, weekly := 0, 4
	var updated Preferences
	if err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var updateErr error
		updated, updateErr = UpdatePreferences(ctx, tx, preferences.Revision, PreferencesPatch{FocusTopics: &topics, DailySaveTarget: &daily, WeeklyDeepTarget: &weekly})
		return updateErr
	}); err != nil {
		t.Fatal(err)
	}
	if updated.Revision != 2 || len(updated.FocusTopics) != 2 || updated.FocusTopics[0] != "AI Agent" || updated.DailySaveTarget != 0 || updated.WeeklyDeepTarget != 4 {
		t.Fatalf("updated=%+v", updated)
	}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, updateErr := UpdatePreferences(ctx, tx, preferences.Revision, PreferencesPatch{DailySaveTarget: &weekly})
		return updateErr
	})
	if faultCode(err) != "VERSION_CONFLICT" {
		t.Fatalf("stale error=%v", err)
	}
	tooHigh := 101
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, updateErr := UpdatePreferences(ctx, tx, updated.Revision, PreferencesPatch{MonthlyPracticeTarget: &tooHigh})
		return updateErr
	})
	if faultCode(err) != "VALIDATION_ERROR" {
		t.Fatalf("invalid error=%v", err)
	}
}

func researchDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func createProject(t *testing.T, db *sql.DB, name string, now time.Time) catalog.Project {
	t.Helper()
	var project catalog.Project
	err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		project, _, err = catalog.CreateProject(context.Background(), tx, catalog.CreateProjectInput{Name: name, CanonicalURL: "https://example.com/" + name, Category: "OTHER"}, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return project
}

func createEntry(t *testing.T, db *sql.DB, projectID string, patch EntryPatch, now time.Time) Entry {
	t.Helper()
	var entry Entry
	if err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		entry, err = CreateEntry(context.Background(), tx, projectID, patch, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return entry
}

func updateEntry(t *testing.T, db *sql.DB, projectID string, revision int, patch EntryPatch, now time.Time) Entry {
	t.Helper()
	var entry Entry
	if err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		entry, err = UpdateEntry(context.Background(), tx, projectID, revision, patch, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return entry
}

func faultCode(err error) string {
	var problem *fault.Error
	if errors.As(err, &problem) {
		return problem.Code
	}
	return ""
}
