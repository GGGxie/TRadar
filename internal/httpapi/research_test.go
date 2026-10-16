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
