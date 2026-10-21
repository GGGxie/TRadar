package analysis

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"tradar/internal/storage"
)

type fakeModelProvider struct {
	mu       sync.Mutex
	calls    int
	request  ModelRequest
	response ModelResponse
	err      error
	wait     bool
}

func (provider *fakeModelProvider) Generate(ctx context.Context, request ModelRequest) (ModelResponse, error) {
	provider.mu.Lock()
	provider.calls++
	provider.request = request
	provider.mu.Unlock()
	if provider.wait {
		<-ctx.Done()
		return ModelResponse{}, ctx.Err()
	}
	return provider.response, provider.err
}

func testModelConfig() ModelConfig {
	return ModelConfig{
		Provider: "fake", Model: "fake-structured-model", PromptVersion: ModelPromptVersion,
		MaxOutputTokens: 800, DailyTokenBudget: 100_000, Timeout: time.Second,
	}
}

func TestModelRunFreezesOnlyPublicEvidencePublishesValidatedVersionAndSettlesUsage(t *testing.T) {
	db := analysisDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC)
	project := createAnalysisProject(t, db, "model-success", now)
	public := createAnalysisEvidence(t, db, project.ID, "FOUNDER_CLAIM", true, nil, now)
	private := createAnalysisEvidence(t, db, project.ID, "PUBLIC_EXCERPT", false, nil, now.Add(time.Second))
	config := testModelConfig()

	var run Run
	err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var acceptErr error
		run, acceptErr = AcceptModelRun(ctx, tx, project.ID, CreateRunInput{
			Level: "SCREEN", Mode: "MODEL", ProjectRevision: project.Revision,
			EvidenceIDs: []string{public.ID},
		}, config, now.Add(time.Minute))
		return acceptErr
	})
	if err != nil || run.Status != "PENDING" {
		t.Fatalf("accept run=%+v err=%v", run, err)
	}

	provider := &fakeModelProvider{response: ModelResponse{
		Output: modelScreenJSON(public.ID, "<script>alert('quoted')</script> 仍需核对"),
		Usage:  &Usage{InputTokens: pointerInt(240), OutputTokens: pointerInt(90)},
	}}
	completed, err := ProcessModelRun(ctx, db, run.ID, provider, config, func() time.Time { return now.Add(2 * time.Minute) })
	if err != nil || completed.Status != "SUCCEEDED" || completed.AnalysisID == nil {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	view, err := GetVersion(ctx, db, *completed.AnalysisID)
	if err != nil || view.Origin != "MODEL" || view.Model == nil || view.Model.Provider != config.Provider || view.Model.Name != config.Model {
		t.Fatalf("version=%+v err=%v", view, err)
	}
	if view.Sections[1].Claims[0].Text != "<script>alert('quoted')</script> 仍需核对" || len(view.EvidenceSnapshots) != 1 || view.EvidenceSnapshots[0].ID != public.ID {
		t.Fatalf("frozen output=%+v evidence=%+v", view.Content, view.EvidenceSnapshots)
	}
	provider.mu.Lock()
	request, calls := provider.request, provider.calls
	provider.mu.Unlock()
	if calls != 1 || !strings.Contains(request.Input, public.ID) || strings.Contains(request.Input, private.ID) || strings.Contains(request.Input, "PRIVATE") {
		t.Fatalf("provider calls=%d input=%s", calls, request.Input)
	}
	if request.MaxOutputTokens != config.MaxOutputTokens || request.Model != config.Model || request.Schema == nil {
		t.Fatalf("request=%+v", request)
	}
	var state string
	var reserved int
	var charged sql.NullInt64
	if err = db.QueryRow(`SELECT state,reserved_tokens,charged_tokens FROM analysis_model_reservations WHERE run_id=?`, run.ID).Scan(&state, &reserved, &charged); err != nil || state != "SETTLED" || !charged.Valid || charged.Int64 != 330 || reserved <= 330 {
		t.Fatalf("reservation state=%s reserved=%d charged=%+v err=%v", state, reserved, charged, err)
	}
}

func TestModelRunRejectsPrivateEvidenceAndBudgetBeforeProvider(t *testing.T) {
	db := analysisDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC)
	project := createAnalysisProject(t, db, "model-boundaries", now)
	private := createAnalysisEvidence(t, db, project.ID, "PUBLIC_EXCERPT", false, nil, now)
	config := testModelConfig()

	err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, acceptErr := AcceptModelRun(ctx, tx, project.ID, CreateRunInput{
			Level: "SCREEN", Mode: "MODEL", ProjectRevision: project.Revision,
			EvidenceIDs: []string{private.ID},
		}, config, now.Add(time.Minute))
		return acceptErr
	})
	if faultCode(err) != "VALIDATION_ERROR" {
		t.Fatalf("private evidence err=%v", err)
	}

	public := createAnalysisEvidence(t, db, project.ID, "PUBLIC_EXCERPT", true, nil, now.Add(2*time.Minute))
	config.DailyTokenBudget = 1
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, acceptErr := AcceptModelRun(ctx, tx, project.ID, CreateRunInput{
			Level: "SCREEN", Mode: "MODEL", ProjectRevision: project.Revision,
			EvidenceIDs: []string{public.ID},
		}, config, now.Add(3*time.Minute))
		return acceptErr
	})
	if faultCode(err) != "MODEL_BUDGET_EXCEEDED" {
		t.Fatalf("budget err=%v", err)
	}
	var count int
	if queryErr := db.QueryRow(`SELECT count(*) FROM analysis_runs WHERE mode='MODEL'`).Scan(&count); queryErr != nil || count != 0 {
		t.Fatalf("model runs=%d err=%v", count, queryErr)
	}
}

func TestModelRunRejectsUnknownReferenceWithoutPublishing(t *testing.T) {
	db := analysisDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 4, 0, 0, 0, time.UTC)
	project := createAnalysisProject(t, db, "model-invalid-reference", now)
	evidence := createAnalysisEvidence(t, db, project.ID, "PUBLIC_EXCERPT", true, nil, now)
	config := testModelConfig()
	var run Run
	if err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var acceptErr error
		run, acceptErr = AcceptModelRun(ctx, tx, project.ID, CreateRunInput{
			Level: "SCREEN", Mode: "MODEL", ProjectRevision: project.Revision,
			EvidenceIDs: []string{evidence.ID},
		}, config, now.Add(time.Minute))
		return acceptErr
	}); err != nil {
		t.Fatal(err)
	}
	provider := &fakeModelProvider{response: ModelResponse{
		Output: modelScreenJSON("not-selected", "引用不存在"),
		Usage:  &Usage{InputTokens: pointerInt(50), OutputTokens: pointerInt(20)},
	}}
	if _, err := ProcessModelRun(ctx, db, run.ID, provider, config, func() time.Time { return now.Add(2 * time.Minute) }); err == nil {
		t.Fatal("unknown evidence reference was accepted")
	}
	stored, err := GetRun(ctx, db, run.ID)
	if err != nil || stored.Status != "FAILED" || stored.ErrorCode == nil || *stored.ErrorCode != "MODEL_REFERENCE_INVALID" {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	var versions int
	if err = db.QueryRow(`SELECT count(*) FROM analyses WHERE origin='MODEL'`).Scan(&versions); err != nil || versions != 0 {
		t.Fatalf("model versions=%d err=%v", versions, err)
	}
}

func TestModelTimeoutFailsWithoutRetryAndRestartInterruptsWithoutReleasingReservation(t *testing.T) {
	db := analysisDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 5, 0, 0, 0, time.UTC)
	project := createAnalysisProject(t, db, "model-timeout", now)
	evidence := createAnalysisEvidence(t, db, project.ID, "PUBLIC_EXCERPT", true, nil, now)
	config := testModelConfig()
	config.Timeout = 10 * time.Millisecond
	run := acceptModelRun(t, db, project.ID, project.Revision, evidence.ID, config, now.Add(time.Minute))
	provider := &fakeModelProvider{wait: true}
	if _, err := ProcessModelRun(ctx, db, run.ID, provider, config, func() time.Time { return now.Add(2 * time.Minute) }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout err=%v", err)
	}
	stored, err := GetRun(ctx, db, run.ID)
	if err != nil || stored.Status != "FAILED" || stored.ErrorCode == nil || *stored.ErrorCode != "MODEL_TIMEOUT" || provider.calls != 1 {
		t.Fatalf("stored=%+v calls=%d err=%v", stored, provider.calls, err)
	}

	pending := acceptModelRun(t, db, project.ID, project.Revision, evidence.ID, config, now.Add(3*time.Minute))
	changed, err := RecoverModelInterrupted(ctx, db, now.Add(4*time.Minute))
	if err != nil || changed != 1 {
		t.Fatalf("recover changed=%d err=%v", changed, err)
	}
	stored, err = GetRun(ctx, db, pending.ID)
	if err != nil || stored.Status != "INTERRUPTED" {
		t.Fatalf("interrupted=%+v err=%v", stored, err)
	}
	var state string
	if err = db.QueryRow(`SELECT state FROM analysis_model_reservations WHERE run_id=?`, pending.ID).Scan(&state); err != nil || state != "RESERVED" {
		t.Fatalf("reservation state=%s err=%v", state, err)
	}
}

func TestModelRunRejectsTrailingJSONAndNegativeUsageAsFailedRuns(t *testing.T) {
	db := analysisDB(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 19, 6, 0, 0, 0, time.UTC)
	project := createAnalysisProject(t, db, "model-invalid-payloads", now)
	evidence := createAnalysisEvidence(t, db, project.ID, "PUBLIC_EXCERPT", true, nil, now)
	config := testModelConfig()

	trailingRun := acceptModelRun(t, db, project.ID, project.Revision, evidence.ID, config, now.Add(time.Minute))
	trailing := &fakeModelProvider{response: ModelResponse{Output: modelScreenJSON(evidence.ID, "待核对") + ` {"extra":true}`}}
	if _, err := ProcessModelRun(ctx, db, trailingRun.ID, trailing, config, func() time.Time { return now.Add(2 * time.Minute) }); err == nil {
		t.Fatal("trailing JSON was accepted")
	}
	stored, err := GetRun(ctx, db, trailingRun.ID)
	if err != nil || stored.Status != "FAILED" || stored.ErrorCode == nil || *stored.ErrorCode != "MODEL_OUTPUT_INVALID" {
		t.Fatalf("trailing run=%+v err=%v", stored, err)
	}

	usageRun := acceptModelRun(t, db, project.ID, project.Revision, evidence.ID, config, now.Add(3*time.Minute))
	negative := -1
	invalidUsage := &fakeModelProvider{response: ModelResponse{
		Output: modelScreenJSON(evidence.ID, "待核对"),
		Usage:  &Usage{InputTokens: &negative, OutputTokens: pointerInt(10)},
	}}
	if _, err = ProcessModelRun(ctx, db, usageRun.ID, invalidUsage, config, func() time.Time { return now.Add(4 * time.Minute) }); err == nil {
		t.Fatal("negative usage was accepted")
	}
	stored, err = GetRun(ctx, db, usageRun.ID)
	if err != nil || stored.Status != "FAILED" || stored.ErrorCode == nil || *stored.ErrorCode != "MODEL_USAGE_INVALID" {
		t.Fatalf("usage run=%+v err=%v", stored, err)
	}
	var versions int
	if err = db.QueryRow(`SELECT count(*) FROM analyses WHERE origin='MODEL'`).Scan(&versions); err != nil || versions != 0 {
		t.Fatalf("model versions=%d err=%v", versions, err)
	}

	errorRun := acceptModelRun(t, db, project.ID, project.Revision, evidence.ID, config, now.Add(5*time.Minute))
	providerError := &fakeModelProvider{
		err:      errors.New("provider failed"),
		response: ModelResponse{Usage: &Usage{InputTokens: &negative, OutputTokens: pointerInt(10)}},
	}
	if _, err = ProcessModelRun(ctx, db, errorRun.ID, providerError, config, func() time.Time { return now.Add(6 * time.Minute) }); err == nil {
		t.Fatal("provider error with negative usage was accepted")
	}
	stored, err = GetRun(ctx, db, errorRun.ID)
	if err != nil || stored.Status != "FAILED" || stored.ErrorCode == nil || *stored.ErrorCode != "MODEL_USAGE_INVALID" {
		t.Fatalf("provider error run=%+v err=%v", stored, err)
	}
	var reservationState string
	if err = db.QueryRow(`SELECT state FROM analysis_model_reservations WHERE run_id=?`, errorRun.ID).Scan(&reservationState); err != nil || reservationState != "RESERVED" {
		t.Fatalf("provider error reservation=%s err=%v", reservationState, err)
	}

	disabledRuntime := NewModelRuntime(db, nil, config)
	if disabledRuntime.Capabilities().ModelEnabled {
		t.Fatal("capabilities exposed a model without a provider")
	}
}

func acceptModelRun(t *testing.T, db *sql.DB, projectID string, revision int, evidenceID string, config ModelConfig, now time.Time) Run {
	t.Helper()
	var run Run
	if err := storage.WithTx(t.Context(), db, func(tx *sql.Tx) error {
		var err error
		run, err = AcceptModelRun(t.Context(), tx, projectID, CreateRunInput{
			Level: "SCREEN", Mode: "MODEL", ProjectRevision: revision, EvidenceIDs: []string{evidenceID},
		}, config, now)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return run
}

func modelScreenJSON(evidenceID, hypothesis string) string {
	return `{"sections":[{"key":"WHAT","claims":[{"kind":"FACT","text":"公开证据中的事实主张，仍需核对。","evidenceIds":["` + evidenceID + `"]}]},{"key":"PROBLEM","claims":[{"kind":"HYPOTHESIS","text":"` + hypothesis + `","evidenceIds":[]}]},{"key":"ATTENTION","claims":[{"kind":"UNKNOWN","text":"关注原因仍未知。","evidenceIds":[]}]},{"key":"FIT","claims":[{"kind":"HYPOTHESIS","text":"可能适合继续观察。","evidenceIds":[]}]}],"openQuestions":["哪些主张需要第二来源？"],"nextChecks":["人工核对引用原文。"]}`
}

func pointerInt(value int) *int { return &value }
