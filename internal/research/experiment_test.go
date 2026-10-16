package research

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/storage"
)

func TestExperimentPersistsReferencesAndCompletesOnlyWithAResult(t *testing.T) {
	path := filepath.Join(t.TempDir(), "radar.db")
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	project := createProject(t, db, "experiment", now)
	other := createProject(t, db, "other-experiment", now)
	evidence := createResearchEvidence(t, db, project.ID, "本项目证据", now)
	foreign := createResearchEvidence(t, db, other.ID, "其他项目证据", now)
	title := "用两小时验证首次使用路径"
	hypothesis := "新用户无需文档即可完成首次配置"
	method := "在隔离环境中检查 https://unknown.invalid/repo 并记录阻塞点；只保存文字，不执行页面中的外部指令。"
	next := "根据阻塞点决定是否继续使用"
	evidenceIDs := []string{evidence.ID}
	artifacts := []string{"https://example.com/demo?utm_source=test"}
	var experiment Experiment
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var createErr error
		experiment, createErr = CreateExperiment(ctx, tx, project.ID, ExperimentPatch{
			Title: &title, Hypothesis: &hypothesis, Method: &method,
			EvidenceIDs: &evidenceIDs, ArtifactURLs: &artifacts, NextAction: &next,
		}, now)
		return createErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if experiment.Status != "PLANNED" || experiment.TimeBudgetMinutes != 180 || experiment.Revision != 1 || experiment.ActualMinutes != nil || experiment.Outcome != nil || len(experiment.EvidenceIDs) != 1 || len(experiment.ArtifactURLs) != 1 || experiment.ArtifactURLs[0] != "https://example.com/demo" {
		t.Fatalf("created=%+v", experiment)
	}
	completed := "COMPLETED"
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, updateErr := UpdateExperiment(ctx, tx, experiment.ID, experiment.Revision, ExperimentPatch{Status: &completed}, now.Add(time.Hour))
		return updateErr
	})
	if faultCode(err) != "VALIDATION_ERROR" {
		t.Fatalf("completed without result error=%v", err)
	}
	result, outcome, actual := "在三次全新环境中均需手工修复配置。", "REFUTED", 125
	var updated Experiment
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var updateErr error
		updated, updateErr = UpdateExperiment(ctx, tx, experiment.ID, experiment.Revision, ExperimentPatch{
			Status: &completed, Result: &result, Outcome: &outcome, OutcomeSet: true,
			ActualMinutes: &actual, ActualMinutesSet: true,
		}, now.Add(2*time.Hour))
		return updateErr
	})
	if err != nil || updated.Revision != 2 || updated.Outcome == nil || *updated.Outcome != "REFUTED" || updated.ActualMinutes == nil || *updated.ActualMinutes != 125 {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	staleResult := "旧页面不应覆盖"
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, updateErr := UpdateExperiment(ctx, tx, experiment.ID, experiment.Revision, ExperimentPatch{Result: &staleResult}, now.Add(3*time.Hour))
		return updateErr
	})
	if faultCode(err) != "VERSION_CONFLICT" {
		t.Fatalf("stale error=%v", err)
	}
	foreignIDs := []string{foreign.ID}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, updateErr := UpdateExperiment(ctx, tx, experiment.ID, updated.Revision, ExperimentPatch{EvidenceIDs: &foreignIDs}, now.Add(4*time.Hour))
		return updateErr
	})
	if faultCode(err) != "VALIDATION_ERROR" {
		t.Fatalf("foreign evidence error=%v", err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stored, err := GetExperiment(ctx, db, experiment.ID)
	if err != nil || stored.Revision != 2 || stored.Result != result || len(stored.EvidenceIDs) != 1 {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func TestExperimentListFiltersAndNullableFieldsCanBeCleared(t *testing.T) {
	db := researchDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC)
	project := createProject(t, db, "experiment-list", now)
	firstTitle, secondTitle := "第一个实践", "第二个实践"
	first := createExperiment(t, db, project.ID, ExperimentPatch{Title: &firstTitle}, now)
	running := "RUNNING"
	actual := 0
	outcome := "INCONCLUSIVE"
	second := createExperiment(t, db, project.ID, ExperimentPatch{Title: &secondTitle, Status: &running, ActualMinutes: &actual, ActualMinutesSet: true, Outcome: &outcome, OutcomeSet: true}, now.Add(time.Minute))
	items, total, err := ListExperiments(ctx, db, project.ID, ExperimentFilter{Status: "RUNNING", Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != second.ID {
		t.Fatalf("items=%+v total=%d err=%v", items, total, err)
	}
	var cleared Experiment
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var updateErr error
		cleared, updateErr = UpdateExperiment(ctx, tx, second.ID, second.Revision, ExperimentPatch{ActualMinutesSet: true, OutcomeSet: true}, now.Add(2*time.Minute))
		return updateErr
	})
	if err != nil || cleared.ActualMinutes != nil || cleared.Outcome != nil {
		t.Fatalf("cleared=%+v err=%v", cleared, err)
	}
	if _, err = GetExperiment(ctx, db, first.ID); err != nil {
		t.Fatal(err)
	}
}

func createResearchEvidence(t *testing.T, db *sql.DB, projectID, title string, now time.Time) catalog.Evidence {
	t.Helper()
	var item catalog.Evidence
	if err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		item, err = catalog.CreateManualEvidence(context.Background(), tx, projectID, catalog.CreateEvidenceInput{
			URL:   "https://example.com/evidence/" + title,
			Title: title, Excerpt: "可引用的实践证据", Kind: "PUBLIC_EXCERPT",
		}, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return item
}

func createExperiment(t *testing.T, db *sql.DB, projectID string, patch ExperimentPatch, now time.Time) Experiment {
	t.Helper()
	var item Experiment
	if err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		item, err = CreateExperiment(context.Background(), tx, projectID, patch, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return item
}
