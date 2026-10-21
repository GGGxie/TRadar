package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestResearchHTTPCreateListUpdateAndConflict(t *testing.T) {
	handler := testServer(t)
	projectResponse := call(handler, http.MethodPost, "/api/v1/projects", `{"name":"Research Target","canonicalUrl":"https://example.com/research-target"}`, "research-project", "")
	var project struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(projectResponse.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}
	missing := call(handler, http.MethodGet, "/api/v1/projects/"+project.Data.ID+"/research", "", "", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing=%d %s", missing.Code, missing.Body)
	}
	createBody := `{"isStarred":true,"tags":[" Agent ","agent","商业"],"stage":"DEEP_RESEARCH","decision":"OBSERVE","notes":"保留的个人笔记","nextAction":"核对留存","nextActionAt":"2026-10-20T09:30:00+08:00"}`
	created := call(handler, http.MethodPost, "/api/v1/projects/"+project.Data.ID+"/research", createBody, "research-create", "")
	replayed := call(handler, http.MethodPost, "/api/v1/projects/"+project.Data.ID+"/research", createBody, "research-create", "")
	if created.Code != http.StatusCreated || created.Header().Get("ETag") != `"1"` || replayed.Body.String() != created.Body.String() || !strings.Contains(created.Body.String(), `"tags":["Agent","商业"]`) {
		t.Fatalf("created=%d %s headers=%v replay=%d %s", created.Code, created.Body, created.Header(), replayed.Code, replayed.Body)
	}
	duplicate := call(handler, http.MethodPost, "/api/v1/projects/"+project.Data.ID+"/research", `{}`, "research-create-another", "")
	if duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), "RESEARCH_ALREADY_EXISTS") {
		t.Fatalf("duplicate=%d %s", duplicate.Code, duplicate.Body)
	}
	listed := call(handler, http.MethodGet, "/api/v1/research?isStarred=true&tag=agent&q=保留&page=1&pageSize=20", "", "", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"totalItems":1`) || !strings.Contains(listed.Body.String(), project.Data.ID) {
		t.Fatalf("listed=%d %s", listed.Code, listed.Body)
	}
	withoutCondition := call(handler, http.MethodPatch, "/api/v1/projects/"+project.Data.ID+"/research", `{"notes":"修改"}`, "", "")
	if withoutCondition.Code != http.StatusPreconditionRequired {
		t.Fatalf("without condition=%d %s", withoutCondition.Code, withoutCondition.Body)
	}
	updated := call(handler, http.MethodPatch, "/api/v1/projects/"+project.Data.ID+"/research", `{"notes":"服务器新内容","nextActionAt":null}`, "", `"1"`)
	if updated.Code != http.StatusOK || updated.Header().Get("ETag") != `"2"` || !strings.Contains(updated.Body.String(), `"nextActionAt":null`) {
		t.Fatalf("updated=%d %s headers=%v", updated.Code, updated.Body, updated.Header())
	}
	stale := call(handler, http.MethodPatch, "/api/v1/projects/"+project.Data.ID+"/research", `{"notes":"本地旧内容"}`, "", `"1"`)
	if stale.Code != http.StatusPreconditionFailed || !strings.Contains(stale.Body.String(), "VERSION_CONFLICT") {
		t.Fatalf("stale=%d %s", stale.Code, stale.Body)
	}
	read := call(handler, http.MethodGet, "/api/v1/projects/"+project.Data.ID+"/research", "", "", "")
	if read.Code != http.StatusOK || read.Header().Get("ETag") != `"2"` || !strings.Contains(read.Body.String(), "服务器新内容") || strings.Contains(read.Body.String(), "本地旧内容") {
		t.Fatalf("read=%d %s headers=%v", read.Code, read.Body, read.Header())
	}
}

func TestResearchHTTPRejectsInvalidFiltersFieldsAndNulls(t *testing.T) {
	handler := testServer(t)
	project := call(handler, http.MethodPost, "/api/v1/projects", `{"name":"Invalid Research","canonicalUrl":"https://example.com/invalid-research"}`, "invalid-research-project", "")
	var response struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(project.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"unknown":   `{"modelNotes":"do not accept"}`,
		"null note": `{"notes":null}`,
		"bad stage": `{"stage":"AUTO_APPROVED"}`,
	} {
		t.Run(name, func(t *testing.T) {
			result := call(handler, http.MethodPost, "/api/v1/projects/"+response.Data.ID+"/research", body, "invalid-"+name, "")
			if result.Code != http.StatusBadRequest && result.Code != http.StatusUnprocessableEntity {
				t.Fatalf("result=%d %s", result.Code, result.Body)
			}
		})
	}
	badBoolean := call(handler, http.MethodGet, "/api/v1/research?isStarred=yes", "", "", "")
	if badBoolean.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad boolean=%d %s", badBoolean.Code, badBoolean.Body)
	}
	badStage := call(handler, http.MethodGet, "/api/v1/research?stage=INVALID", "", "", "")
	if badStage.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad stage=%d %s", badStage.Code, badStage.Body)
	}
	tooLong := call(handler, http.MethodGet, "/api/v1/research?q="+strings.Repeat("a", 201), "", "", "")
	if tooLong.Code != http.StatusUnprocessableEntity {
		t.Fatalf("long q=%d %s", tooLong.Code, tooLong.Body)
	}
}

func TestResearchPreferencesHTTPDefaultsUpdateAndConflict(t *testing.T) {
	handler := testServer(t)
	read := call(handler, http.MethodGet, "/api/v1/research-preferences", "", "", "")
	if read.Code != http.StatusOK || read.Header().Get("ETag") != `"1"` || !strings.Contains(read.Body.String(), `"focusTopics":["AI Agent","开发者工具","SaaS"]`) || !strings.Contains(read.Body.String(), `"dailySaveTarget":3`) {
		t.Fatalf("read=%d %s headers=%v", read.Code, read.Body, read.Header())
	}
	missing := call(handler, http.MethodPatch, "/api/v1/research-preferences", `{"dailySaveTarget":0}`, "", "")
	if missing.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing=%d %s", missing.Code, missing.Body)
	}
	updated := call(handler, http.MethodPatch, "/api/v1/research-preferences", `{"focusTopics":["AI Agent","ai agent","独立开发"],"dailySaveTarget":0}`, "", `"1"`)
	if updated.Code != http.StatusOK || updated.Header().Get("ETag") != `"2"` || !strings.Contains(updated.Body.String(), `"focusTopics":["AI Agent","独立开发"]`) {
		t.Fatalf("updated=%d %s headers=%v", updated.Code, updated.Body, updated.Header())
	}
	stale := call(handler, http.MethodPatch, "/api/v1/research-preferences", `{"weeklyDeepTarget":4}`, "", `"1"`)
	if stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale=%d %s", stale.Code, stale.Body)
	}
	invalid := call(handler, http.MethodPatch, "/api/v1/research-preferences", `{"monthlyPracticeTarget":101}`, "", `"2"`)
	if invalid.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid=%d %s", invalid.Code, invalid.Body)
	}
}

func TestExperimentHTTPCreateReplayListCompleteAndClear(t *testing.T) {
	handler := testServer(t)
	projectResponse := call(handler, http.MethodPost, "/api/v1/projects", `{"name":"Practice Target","canonicalUrl":"https://example.com/practice-target"}`, "practice-project", "")
	var project struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(projectResponse.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/projects/" + project.Data.ID + "/experiments"
	body := `{"title":"验证首次使用路径","hypothesis":"无需文档即可配置","method":"在隔离环境中记录阻塞点","status":"PLANNED","timeBudgetMinutes":120,"actualMinutes":null,"result":"","outcome":null,"evidenceIds":[],"artifactUrls":["https://example.com/demo"],"nextAction":"完成后复盘"}`
	created := call(handler, http.MethodPost, path, body, "practice-create", "")
	replayed := call(handler, http.MethodPost, path, body, "practice-create", "")
	if created.Code != http.StatusCreated || created.Header().Get("ETag") != `"1"` || replayed.Body.String() != created.Body.String() {
		t.Fatalf("created=%d %s headers=%v replay=%d %s", created.Code, created.Body, created.Header(), replayed.Code, replayed.Body)
	}
	var response struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	mismatch := call(handler, http.MethodPost, path, `{"title":"不同内容"}`, "practice-create", "")
	if mismatch.Code != http.StatusUnprocessableEntity || !strings.Contains(mismatch.Body.String(), "IDEMPOTENCY_MISMATCH") {
		t.Fatalf("mismatch=%d %s", mismatch.Code, mismatch.Body)
	}
	listed := call(handler, http.MethodGet, path+"?status=PLANNED&page=1&pageSize=20", "", "", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"totalItems":1`) || !strings.Contains(listed.Body.String(), response.Data.ID) {
		t.Fatalf("listed=%d %s", listed.Code, listed.Body)
	}
	readPath := "/api/v1/experiments/" + response.Data.ID
	read := call(handler, http.MethodGet, readPath, "", "", "")
	if read.Code != http.StatusOK || read.Header().Get("ETag") != `"1"` {
		t.Fatalf("read=%d %s headers=%v", read.Code, read.Body, read.Header())
	}
	missingCondition := call(handler, http.MethodPatch, readPath, `{"status":"RUNNING"}`, "", "")
	if missingCondition.Code != http.StatusPreconditionRequired {
		t.Fatalf("missing condition=%d %s", missingCondition.Code, missingCondition.Body)
	}
	invalidComplete := call(handler, http.MethodPatch, readPath, `{"status":"COMPLETED"}`, "", `"1"`)
	if invalidComplete.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid complete=%d %s", invalidComplete.Code, invalidComplete.Body)
	}
	completed := call(handler, http.MethodPatch, readPath, `{"status":"COMPLETED","actualMinutes":95,"result":"需要文档才能完成","outcome":"REFUTED"}`, "", `"1"`)
	if completed.Code != http.StatusOK || completed.Header().Get("ETag") != `"2"` || !strings.Contains(completed.Body.String(), `"outcome":"REFUTED"`) {
		t.Fatalf("completed=%d %s headers=%v", completed.Code, completed.Body, completed.Header())
	}
	stale := call(handler, http.MethodPatch, readPath, `{"result":"旧页面内容"}`, "", `"1"`)
	if stale.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale=%d %s", stale.Code, stale.Body)
	}
	cleared := call(handler, http.MethodPatch, readPath, `{"status":"RUNNING","actualMinutes":null,"outcome":null}`, "", `"2"`)
	if cleared.Code != http.StatusOK || cleared.Header().Get("ETag") != `"3"` || !strings.Contains(cleared.Body.String(), `"actualMinutes":null`) || !strings.Contains(cleared.Body.String(), `"outcome":null`) {
		t.Fatalf("cleared=%d %s headers=%v", cleared.Code, cleared.Body, cleared.Header())
	}
}

func TestExperimentHTTPRejectsUnknownFieldsNullsAndInvalidFilters(t *testing.T) {
	handler := testServer(t)
	projectResponse := call(handler, http.MethodPost, "/api/v1/projects", `{"name":"Invalid Practice","canonicalUrl":"https://example.com/invalid-practice"}`, "invalid-practice-project", "")
	var project struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(projectResponse.Body.Bytes(), &project); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/projects/" + project.Data.ID + "/experiments"
	for name, body := range map[string]string{
		"missing title": `{}`,
		"unknown":       `{"title":"x","runCode":true}`,
		"null title":    `{"title":null}`,
		"unsafe URL":    `{"title":"x","artifactUrls":["javascript:alert(1)"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			result := call(handler, http.MethodPost, path, body, "invalid-practice-"+name, "")
			if result.Code != http.StatusBadRequest && result.Code != http.StatusUnprocessableEntity {
				t.Fatalf("result=%d %s", result.Code, result.Body)
			}
		})
	}
	badFilter := call(handler, http.MethodGet, path+"?status=EXECUTING", "", "", "")
	if badFilter.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad filter=%d %s", badFilter.Code, badFilter.Body)
	}
}
