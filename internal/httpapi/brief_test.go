package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"tradar/internal/brief"
)

func TestBriefHTTPIdempotencyVersionsMarkdownAndSettings(t *testing.T) {
	handler, db := testServerWithDB(t)
	createdProject := call(handler, http.MethodPost, "/api/v1/projects", `{"name":"Daily Signal","canonicalUrl":"https://example.com/daily","category":"AI_AGENT"}`, "brief-project", "")
	if createdProject.Code != http.StatusCreated {
		t.Fatalf("project=%d %s", createdProject.Code, createdProject.Body)
	}
	date := time.Now().In(mustLocation(t, "Asia/Shanghai")).Format("2006-01-02")
	body := `{"date":"` + date + `","timeZone":"Asia/Shanghai"}`
	created := call(handler, http.MethodPost, "/api/v1/briefs", body, "brief-intent", "")
	replayed := call(handler, http.MethodPost, "/api/v1/briefs", body, "brief-intent", "")
	if created.Code != http.StatusAccepted || replayed.Body.String() != created.Body.String() || !strings.HasPrefix(created.Header().Get("Location"), "/api/v1/brief-runs/") {
		t.Fatalf("created=%d %s headers=%v replay=%d %s", created.Code, created.Body, created.Header(), replayed.Code, replayed.Body)
	}
	blocked := call(handler, http.MethodPost, "/api/v1/briefs", body, "second-intent", "")
	if blocked.Code != http.StatusConflict || !strings.Contains(blocked.Body.String(), "BRIEF_ALREADY_RUNNING") {
		t.Fatalf("blocked=%d %s", blocked.Code, blocked.Body)
	}
	var accepted struct {
		Data brief.Run `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &accepted); err != nil {
		t.Fatal(err)
	}
	if _, err := brief.ProcessRun(t.Context(), db, accepted.Data.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	run := call(handler, http.MethodGet, "/api/v1/brief-runs/"+accepted.Data.ID, "", "", "")
	if run.Code != http.StatusOK || run.Header().Get("ETag") != `"2"` || !strings.Contains(run.Body.String(), `"status":"SUCCEEDED"`) {
		t.Fatalf("run=%d %s headers=%v", run.Code, run.Body, run.Header())
	}
	var completed struct {
		Data brief.Run `json:"data"`
	}
	if err := json.Unmarshal(run.Body.Bytes(), &completed); err != nil || completed.Data.BriefID == nil {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	listed := call(handler, http.MethodGet, "/api/v1/briefs?date="+date+"&timeZone=Asia%2FShanghai&page=1&pageSize=20", "", "", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), `"totalItems":1`) {
		t.Fatalf("listed=%d %s", listed.Code, listed.Body)
	}
	markdown := call(handler, http.MethodGet, "/api/v1/briefs/"+*completed.Data.BriefID+"/markdown", "", "", "")
	if markdown.Code != http.StatusOK || markdown.Header().Get("Content-Type") != "text/markdown; charset=utf-8" || !strings.Contains(markdown.Header().Get("Content-Disposition"), "tradar-brief-") || !strings.Contains(markdown.Body.String(), "Daily Signal") {
		t.Fatalf("markdown=%d headers=%v body=%s", markdown.Code, markdown.Header(), markdown.Body)
	}
	settings := call(handler, http.MethodGet, "/api/v1/brief-settings", "", "", "")
	if settings.Code != http.StatusOK || settings.Header().Get("ETag") != `"1"` {
		t.Fatalf("settings=%d %s headers=%v", settings.Code, settings.Body, settings.Header())
	}
	updated := call(handler, http.MethodPatch, "/api/v1/brief-settings", `{"maxItems":8}`, "", `"1"`)
	if updated.Code != http.StatusOK || updated.Header().Get("ETag") != `"2"` || !strings.Contains(updated.Body.String(), `"maxItems":8`) {
		t.Fatalf("updated=%d %s headers=%v", updated.Code, updated.Body, updated.Header())
	}
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return location
}
