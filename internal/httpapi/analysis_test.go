package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestAnalysisHTTPManualRulesIdempotencyAndCapabilities(t *testing.T) {
	handler := testServer(t)
	createdProject := call(handler, http.MethodPost, "/api/v1/projects", `{"name":"Analysis Target","canonicalUrl":"https://example.com/analysis"}`, "analysis-project", "")
	var project struct {
		Data struct {
			ID       string `json:"id"`
			Revision int    `json:"revision"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createdProject.Body.Bytes(), &project); err != nil || createdProject.Code != http.StatusCreated {
		t.Fatalf("project: %d %s err=%v", createdProject.Code, createdProject.Body, err)
	}
	evidenceBody := `{"url":"https://example.com/founder","title":"创始人收入自述","excerpt":"创始人称某月收入为 1 万美元。","kind":"FOUNDER_CLAIM","publisher":"Founder","isPublic":false}`
	createdEvidence := call(handler, http.MethodPost, "/api/v1/projects/"+project.Data.ID+"/evidence", evidenceBody, "analysis-evidence", "")
	var evidence struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createdEvidence.Body.Bytes(), &evidence); err != nil || createdEvidence.Code != http.StatusCreated {
		t.Fatalf("evidence: %d %s err=%v", createdEvidence.Code, createdEvidence.Body, err)
	}
	manualBody := fmt.Sprintf(`{"level":"SCREEN","projectRevision":%d,"sections":[{"key":"WHAT","claims":[{"kind":"FACT","text":"创始人自述月收入为 1 万美元，尚未独立核实。","evidenceIds":[%q]}]},{"key":"PROBLEM","claims":[{"kind":"UNKNOWN","text":"待核实","evidenceIds":[]}]},{"key":"ATTENTION","claims":[{"kind":"HYPOTHESIS","text":"可能受到关注","evidenceIds":[]}]},{"key":"FIT","claims":[{"kind":"JUDGMENT","text":"继续观察","evidenceIds":[]}]}],"openQuestions":["收入是否持续？"],"nextChecks":["核对期间和口径。"]}`, project.Data.Revision, evidence.Data.ID)
	created := call(handler, http.MethodPost, "/api/v1/projects/"+project.Data.ID+"/analyses", manualBody, "manual-analysis", "")
	replayed := call(handler, http.MethodPost, "/api/v1/projects/"+project.Data.ID+"/analyses", manualBody, "manual-analysis", "")
	if created.Code != http.StatusCreated || replayed.Code != http.StatusCreated || replayed.Body.String() != created.Body.String() || !strings.Contains(created.Body.String(), `"origin":"MANUAL"`) || !strings.Contains(created.Body.String(), `"kind":"FOUNDER_CLAIM"`) || !strings.Contains(created.Body.String(), `"isPublic":false`) {
		t.Fatalf("created=%d %s replay=%d %s", created.Code, created.Body, replayed.Code, replayed.Body)
	}
	mismatch := call(handler, http.MethodPost, "/api/v1/projects/"+project.Data.ID+"/analyses", strings.Replace(manualBody, "继续观察", "停止观察", 1), "manual-analysis", "")
	if mismatch.Code != http.StatusUnprocessableEntity || !strings.Contains(mismatch.Body.String(), "IDEMPOTENCY_MISMATCH") {
		t.Fatalf("mismatch: %d %s", mismatch.Code, mismatch.Body)
	}
	listed := call(handler, http.MethodGet, "/api/v1/projects/"+project.Data.ID+"/analyses?level=SCREEN&page=1&pageSize=20", "", "", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"totalItems":1`) {
		t.Fatalf("listed: %d %s", listed.Code, listed.Body)
	}
	rulesBody := fmt.Sprintf(`{"level":"DEEP","mode":"RULES","projectRevision":%d,"evidenceIds":[%q]}`, project.Data.Revision, evidence.Data.ID)
	rules := call(handler, http.MethodPost, "/api/v1/projects/"+project.Data.ID+"/analysis-runs", rulesBody, "rules-analysis", "")
	if rules.Code != http.StatusAccepted || rules.Header().Get("Location") == "" || !strings.Contains(rules.Body.String(), `"status":"SUCCEEDED"`) {
		t.Fatalf("rules: %d %s %v", rules.Code, rules.Body, rules.Header())
	}
	readRun := call(handler, http.MethodGet, rules.Header().Get("Location"), "", "", "")
	if readRun.Code != http.StatusOK || readRun.Header().Get("ETag") != `"2"` {
		t.Fatalf("run: %d %s %v", readRun.Code, readRun.Body, readRun.Header())
	}
	model := call(handler, http.MethodPost, "/api/v1/projects/"+project.Data.ID+"/analysis-runs", fmt.Sprintf(`{"level":"SCREEN","mode":"MODEL","projectRevision":%d,"evidenceIds":[%q]}`, project.Data.Revision, evidence.Data.ID), "model-analysis", "")
	if model.Code != http.StatusServiceUnavailable || !strings.Contains(model.Body.String(), "MODEL_DISABLED") {
		t.Fatalf("model: %d %s", model.Code, model.Body)
	}
	capabilities := call(handler, http.MethodGet, "/api/v1/analysis-capabilities", "", "", "")
	if capabilities.Code != http.StatusOK || !strings.Contains(capabilities.Body.String(), `"rulesEnabled":true`) || !strings.Contains(capabilities.Body.String(), `"modelEnabled":false`) {
		t.Fatalf("capabilities: %d %s", capabilities.Code, capabilities.Body)
	}
}

func TestAnalysisHTTPRejectsInvalidFactsAndStaleProject(t *testing.T) {
	handler := testServer(t)
	createdProject := call(handler, http.MethodPost, "/api/v1/projects", `{"name":"Invalid Analysis","canonicalUrl":"https://example.com/invalid-analysis"}`, "invalid-analysis-project", "")
	var project struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(createdProject.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}
	invalid := `{"level":"SCREEN","projectRevision":1,"sections":[{"key":"WHAT","claims":[{"kind":"FACT","text":"无证据事实","evidenceIds":[]}]},{"key":"PROBLEM","claims":[]},{"key":"ATTENTION","claims":[]},{"key":"FIT","claims":[]}],"openQuestions":[],"nextChecks":[]}`
	response := call(handler, http.MethodPost, "/api/v1/projects/"+project.Data.ID+"/analyses", invalid, "invalid-fact", "")
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"evidenceIds"`) {
		t.Fatalf("invalid fact: %d %s", response.Code, response.Body)
	}
	updated := call(handler, http.MethodPatch, "/api/v1/projects/"+project.Data.ID, `{"name":"Changed"}`, "", `"1"`)
	if updated.Code != http.StatusOK {
		t.Fatalf("update: %d %s", updated.Code, updated.Body)
	}
	stale := strings.Replace(invalid, `"kind":"FACT","text":"无证据事实"`, `"kind":"UNKNOWN","text":"待核实"`, 1)
	response = call(handler, http.MethodPost, "/api/v1/projects/"+project.Data.ID+"/analyses", stale, "stale-analysis", "")
	if response.Code != http.StatusPreconditionFailed || !strings.Contains(response.Body.String(), "PROJECT_CONTEXT_CHANGED") {
		t.Fatalf("stale: %d %s", response.Code, response.Body)
	}
}
