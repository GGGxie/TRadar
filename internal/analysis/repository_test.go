package analysis

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
	"tradar/internal/storage"
)

func TestManualAnalysisFreezesEvidenceAndVersions(t *testing.T) {
	db := analysisDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 7, 0, 0, 0, time.UTC)
	project := createAnalysisProject(t, db, "one", now)
	evidence := createAnalysisEvidence(t, db, project.ID, "FOUNDER_CLAIM", false, nil, now)
	input := screenInput(project.Revision, Claim{Kind: "FACT", Text: "创始人自述月收入为 1 万美元，尚未独立核实。", EvidenceIDs: []string{evidence.ID}})
	first := createManualVersion(t, db, project.ID, input, now)
	if first.Version != 1 || first.Origin != "MANUAL" || len(first.EvidenceSnapshots) != 1 || first.EvidenceSnapshots[0].Kind != "FOUNDER_CLAIM" || first.EvidenceSnapshots[0].IsPublic {
		t.Fatalf("first=%+v", first)
	}
	input.BasedOnAnalysisID = &first.ID
	input.Sections[0].Claims[0].Text = "修订后的事实表述仍保留自述标签。"
	second := createManualVersion(t, db, project.ID, input, now.Add(time.Minute))
	if second.Version != 2 || second.BasedOnAnalysisID == nil || *second.BasedOnAnalysisID != first.ID {
		t.Fatalf("second=%+v", second)
	}
	stored, err := GetVersion(ctx, db, first.ID)
	if err != nil || stored.Sections[0].Claims[0].Text == second.Sections[0].Claims[0].Text {
		t.Fatalf("old version changed: %+v err=%v", stored, err)
	}
	name := "changed"
	if _, err = catalog.UpdateProject(ctx, db, project.ID, project.Revision, catalog.ProjectPatch{Name: &name}, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	stored, err = GetVersion(ctx, db, first.ID)
	if err != nil || !stored.ContextChanged {
		t.Fatalf("context change=%+v err=%v", stored, err)
	}
	if err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, createErr := CreateManual(ctx, tx, project.ID, screenInput(project.Revision, Claim{Kind: "UNKNOWN", Text: "待核实", EvidenceIDs: []string{}}), now.Add(3*time.Minute))
		return createErr
	}); faultCode(err) != "PROJECT_CONTEXT_CHANGED" {
		t.Fatalf("stale revision error=%v", err)
	}
}

func TestFactReferencesAreValidatedWithoutPartialVersion(t *testing.T) {
	db := analysisDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	project := createAnalysisProject(t, db, "one", now)
	other := createAnalysisProject(t, db, "two", now)
	foreign := createAnalysisEvidence(t, db, other.ID, "PUBLIC_EXCERPT", true, nil, now)
	for name, claim := range map[string]Claim{
		"missing": {Kind: "FACT", Text: "没有引用", EvidenceIDs: []string{}},
		"unknown": {Kind: "FACT", Text: "不存在", EvidenceIDs: []string{"missing"}},
		"foreign": {Kind: "FACT", Text: "跨项目", EvidenceIDs: []string{foreign.ID}},
	} {
		t.Run(name, func(t *testing.T) {
			err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
				_, createErr := CreateManual(ctx, tx, project.ID, screenInput(project.Revision, claim), now)
				return createErr
			})
			if faultCode(err) != "VALIDATION_ERROR" {
				t.Fatalf("error=%v", err)
			}
		})
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM analyses`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial versions=%d err=%v", count, err)
	}
}

func TestRulesRunKeepsPrivateEvidenceLocalAndModelDisabled(t *testing.T) {
	db := analysisDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	project := createAnalysisProject(t, db, "rules", now)
	evidence := createAnalysisEvidence(t, db, project.ID, "FOUNDER_CLAIM", false, nil, now)
	var run Run
	if err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var createErr error
		run, createErr = CreateRulesRun(ctx, tx, project.ID, CreateRunInput{Level: "SCREEN", Mode: "RULES", ProjectRevision: project.Revision, EvidenceIDs: []string{evidence.ID}}, now)
		return createErr
	}); err != nil {
		t.Fatal(err)
	}
	if run.Status != "SUCCEEDED" || run.AnalysisID == nil {
		t.Fatalf("run=%+v", run)
	}
	view, err := GetVersion(ctx, db, *run.AnalysisID)
	if err != nil || view.Origin != "RULES" || view.RulesVersion == nil || *view.RulesVersion != RulesVersion || len(view.Sections) != 4 || len(view.EvidenceSnapshots) != 1 || view.EvidenceSnapshots[0].IsPublic {
		t.Fatalf("rules view=%+v err=%v", view, err)
	}
	if !strings.Contains(view.Sections[2].Claims[0].Text, "未独立核实") {
		t.Fatalf("founder label lost: %+v", view.Sections[2])
	}
	for _, section := range view.Sections {
		for _, claim := range section.Claims {
			if claim.EvidenceIDs == nil {
				t.Fatalf("evidenceIds must encode as an array: %+v", claim)
			}
		}
	}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, createErr := CreateRulesRun(ctx, tx, project.ID, CreateRunInput{Level: "SCREEN", Mode: "MODEL", ProjectRevision: project.Revision, EvidenceIDs: []string{evidence.ID}}, now)
		return createErr
	})
	if faultCode(err) != "MODEL_DISABLED" {
		t.Fatalf("model error=%v", err)
	}
}

func TestRulesRunPlacesSourceDerivedEvidenceAsCitedFacts(t *testing.T) {
	db := analysisDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 23, 3, 0, 0, 0, time.UTC)
	project := createAnalysisProject(t, db, "sourced", now)
	founder := createAnalysisEvidence(t, db, project.ID, "FOUNDER_CLAIM", false, nil, now)
	var derived catalog.DerivedEvidence
	if err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		target := "https://github.com/acme/sourced"
		source, _, err := catalog.UpsertSource(ctx, tx, catalog.UpsertSourceInput{
			ProjectID: project.ID, Source: "GITHUB", ExternalID: "77", RecordType: "PROJECT",
			URL: "https://api.github.com/repositories/77", TargetURL: &target, Title: "acme/sourced", Excerpt: "Agent toolkit", FetchedAt: now,
		})
		if err != nil {
			return err
		}
		if _, err = catalog.AppendObservations(ctx, tx, "run-1", source.ID, []catalog.ObservationInput{{Metric: "STARS", Basis: "CUMULATIVE", Value: 1200, ObservedAt: now, FetchedAt: now}}); err != nil {
			return err
		}
		derived, err = catalog.DeriveSourceEvidence(ctx, tx, project.ID, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	ids := []string{founder.ID}
	for _, item := range derived.Evidence {
		ids = append(ids, item.ID)
	}
	run := func(level string) View {
		t.Helper()
		var created Run
		if err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
			var err error
			created, err = CreateRulesRun(ctx, tx, project.ID, CreateRunInput{Level: level, Mode: "RULES", ProjectRevision: project.Revision, EvidenceIDs: ids}, now.Add(time.Minute))
			return err
		}); err != nil {
			t.Fatal(err)
		}
		view, err := GetVersion(ctx, db, *created.AnalysisID)
		if err != nil || *view.RulesVersion != "rules-v2" || len(view.EvidenceSnapshots) != 3 {
			t.Fatalf("%s view=%+v err=%v", level, view, err)
		}
		return view
	}
	screen := run("SCREEN")
	what := screen.Sections[0].Claims
	if len(what) != 1 || what[0].Kind != "FACT" || !strings.Contains(what[0].Text, "Agent toolkit") || len(what[0].EvidenceIDs) != 1 {
		t.Fatalf("WHAT should cite the frozen source description: %+v", what)
	}
	attention := screen.Sections[2].Claims
	if len(attention) != 2 || !strings.Contains(attention[0].Text, "Stars 1,200") || !strings.Contains(attention[1].Text, "未独立核实") {
		t.Fatalf("ATTENTION should list metric then other evidence: %+v", attention)
	}
	for _, index := range []int{1, 3} {
		if screen.Sections[index].Claims[0].Kind != "UNKNOWN" {
			t.Fatalf("section %s must stay unknown: %+v", screen.Sections[index].Key, screen.Sections[index])
		}
	}
	if !strings.Contains(strings.Join(screen.OpenQuestions, "\n"), "真实使用或付费") {
		t.Fatalf("open questions=%v", screen.OpenQuestions)
	}
	deep := run("DEEP")
	if !strings.Contains(deep.Sections[1].Claims[0].Text, "Agent toolkit") || !strings.Contains(deep.Sections[3].Claims[0].Text, "Stars 1,200") || !strings.Contains(deep.Sections[4].Claims[0].Text, "未独立核实") {
		t.Fatalf("DEEP placement PRODUCT/GROWTH/BUSINESS: %+v", deep.Sections)
	}
	if deep.Sections[0].Claims[0].Kind != "HYPOTHESIS" {
		t.Fatalf("NEED keeps the summary as hypothesis: %+v", deep.Sections[0])
	}
}

func TestSourceReassociationMarksHistoryAndRejectsNewReference(t *testing.T) {
	db := analysisDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	project := createAnalysisProject(t, db, "source-one", now)
	other := createAnalysisProject(t, db, "source-two", now)
	var source catalog.SourceRecord
	if err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var createErr error
		source, _, createErr = catalog.UpsertSource(ctx, tx, catalog.UpsertSourceInput{ProjectID: project.ID, Source: "MANUAL", ExternalID: "source-one", RecordType: "STORY", URL: "https://example.com/story", Title: "Story", FetchedAt: now})
		return createErr
	}); err != nil {
		t.Fatal(err)
	}
	evidence := createAnalysisEvidence(t, db, project.ID, "PUBLIC_EXCERPT", true, &source.ID, now)
	version := createManualVersion(t, db, project.ID, screenInput(project.Revision, Claim{Kind: "FACT", Text: "有来源", EvidenceIDs: []string{evidence.ID}}), now)
	if err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, changeErr := catalog.ReassociateSource(ctx, tx, source.ID, source.Revision, other.ID, "纠正归属", now.Add(time.Minute))
		return changeErr
	}); err != nil {
		t.Fatal(err)
	}
	view, err := GetVersion(ctx, db, version.ID)
	if err != nil || !view.ContextChanged || view.EvidenceSnapshots[0].SourceRecordID == nil {
		t.Fatalf("historical snapshot=%+v err=%v", view, err)
	}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, createErr := CreateManual(ctx, tx, project.ID, screenInput(project.Revision, Claim{Kind: "FACT", Text: "旧来源", EvidenceIDs: []string{evidence.ID}}), now.Add(2*time.Minute))
		return createErr
	})
	if faultCode(err) != "VALIDATION_ERROR" {
		t.Fatalf("reassociated reference error=%v", err)
	}
}

func analysisDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func createAnalysisProject(t *testing.T, db *sql.DB, suffix string, now time.Time) catalog.Project {
	t.Helper()
	var project catalog.Project
	if err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		project, _, err = catalog.CreateProject(context.Background(), tx, catalog.CreateProjectInput{Name: suffix, CanonicalURL: "https://example.com/" + suffix, Summary: "项目自述", Category: "OTHER"}, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return project
}

func createAnalysisEvidence(t *testing.T, db *sql.DB, projectID, kind string, public bool, sourceID *string, now time.Time) catalog.Evidence {
	t.Helper()
	var item catalog.Evidence
	if err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		item, err = catalog.CreateEvidence(context.Background(), tx, projectID, catalog.CreateEvidenceInput{URL: "https://example.com/evidence/" + projectID + kind, Title: "收入说明", Excerpt: "创始人称某月收入为 1 万美元。", Kind: kind, Publisher: stringPtr("Founder"), IsPublic: &public, SourceRecordID: sourceID}, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return item
}

func screenInput(revision int, first Claim) CreateInput {
	return CreateInput{Level: "SCREEN", ProjectRevision: revision, Content: Content{Sections: []Section{
		{Key: "WHAT", Claims: []Claim{first}},
		{Key: "PROBLEM", Claims: []Claim{{Kind: "UNKNOWN", Text: "待核实", EvidenceIDs: []string{}}}},
		{Key: "ATTENTION", Claims: []Claim{{Kind: "HYPOTHESIS", Text: "可能受到关注", EvidenceIDs: []string{}}}},
		{Key: "FIT", Claims: []Claim{{Kind: "JUDGMENT", Text: "需要进一步判断", EvidenceIDs: []string{}}}},
	}, OpenQuestions: []string{"谁会付费？"}, NextChecks: []string{"核对定价。"}}}
}

func createManualVersion(t *testing.T, db *sql.DB, projectID string, input CreateInput, now time.Time) Version {
	t.Helper()
	var version Version
	if err := storage.WithTx(context.Background(), db, func(tx *sql.Tx) error {
		var err error
		version, err = CreateManual(context.Background(), tx, projectID, input, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return version
}

func faultCode(err error) string {
	var problem *fault.Error
	if errors.As(err, &problem) {
		return problem.Code
	}
	return ""
}

func stringPtr(value string) *string { return &value }

// #56：没有任何用户写下内容的人工版本不能保存；只写一处内容的不完整版本仍可保存。
func TestManualAnalysisRequiresAuthoredContent(t *testing.T) {
	db := analysisDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	project := createAnalysisProject(t, db, "one", now)
	placeholder := func() []Section {
		sections := []Section{}
		for _, key := range []string{"NEED", "PRODUCT", "TECH", "GROWTH", "BUSINESS", "COMPETITION"} {
			sections = append(sections, Section{Key: key, Claims: []Claim{{Kind: "UNKNOWN", Text: UnknownPlaceholder, EvidenceIDs: []string{}}}})
		}
		return sections
	}
	empty := CreateInput{Level: "DEEP", ProjectRevision: project.Revision, Content: Content{Sections: placeholder(), OpenQuestions: []string{}, NextChecks: []string{}}}
	err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, createErr := CreateManual(ctx, tx, project.ID, empty, now)
		return createErr
	})
	var problem *fault.Error
	if !errors.As(err, &problem) || problem.Code != "VALIDATION_ERROR" || problem.Fields["content"] == "" {
		t.Fatalf("empty manual version error=%v", err)
	}
	if _, total, _ := ListVersions(ctx, db, project.ID, VersionFilter{Level: "DEEP", Page: 1, PageSize: 20}); total != 0 {
		t.Fatalf("empty manual version stored: total=%d", total)
	}

	oneClaim := empty
	oneClaim.Content.Sections = placeholder()
	oneClaim.Content.Sections[0].Claims[0] = Claim{Kind: "HYPOTHESIS", Text: "开发者希望先看到结论。", EvidenceIDs: []string{}}
	if version := createManualVersion(t, db, project.ID, oneClaim, now.Add(time.Minute)); version.Version != 1 {
		t.Fatalf("one claim version=%+v", version)
	}
	unknownWithNote := empty
	unknownWithNote.Content.Sections = placeholder()
	unknownWithNote.Content.Sections[4].Claims[0].Text = "还没找到定价信息。"
	createManualVersion(t, db, project.ID, unknownWithNote, now.Add(2*time.Minute))
	questionOnly := empty
	questionOnly.Content.Sections = placeholder()
	questionOnly.Content.OpenQuestions = []string{"真实用户是否愿意安装？"}
	createManualVersion(t, db, project.ID, questionOnly, now.Add(3*time.Minute))
}
