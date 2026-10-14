package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"tradar/internal/storage"
)

func testServer(t *testing.T) http.Handler {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, Config{})
}
func call(h http.Handler, method, path, body, key, rev string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://localhost"+path, bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if rev != "" {
		r.Header.Set("If-Match", rev)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestProjectsHTTPIdempotencyAndVersion(t *testing.T) {
	h := testServer(t)
	body := `{"name":"测试雷达","canonicalUrl":"https://example.com/?utm_source=x"}`
	first := call(h, "POST", "/api/v1/projects", body, "intent-1", "")
	if first.Code != 201 {
		t.Fatalf("%d %s", first.Code, first.Body)
	}
	again := call(h, "POST", "/api/v1/projects", body, "intent-1", "")
	if again.Code != 201 || again.Body.String() != first.Body.String() {
		t.Fatalf("replay differs: %s", again.Body)
	}
	mismatch := call(h, "POST", "/api/v1/projects", `{"name":"other","canonicalUrl":"https://example.org"}`, "intent-1", "")
	if mismatch.Code != 422 {
		t.Fatalf("mismatched key: %d", mismatch.Code)
	}
	var created struct {
		Data struct {
			ID string `json:"id"`
		}
	}
	json.Unmarshal(first.Body.Bytes(), &created)
	path := "/api/v1/projects/" + created.Data.ID
	updated := call(h, "PATCH", path, `{"name":"已更新"}`, "", `"1"`)
	if updated.Code != 200 || updated.Header().Get("ETag") != `"2"` {
		t.Fatalf("update: %d %s", updated.Code, updated.Body)
	}
	stale := call(h, "PATCH", path, `{"name":"不应覆盖"}`, "", `"1"`)
	if stale.Code != 412 {
		t.Fatalf("stale=%d", stale.Code)
	}
	missing := call(h, "PATCH", path, `{"name":"missing"}`, "", "")
	if missing.Code != 428 {
		t.Fatalf("missing=%d", missing.Code)
	}
	list := call(h, "GET", "/api/v1/projects?pageSize=1", "", "", "")
	var page struct {
		Data       []json.RawMessage
		Pagination struct{ TotalItems int }
	}
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil || list.Code != 200 || len(page.Data) != 1 || page.Pagination.TotalItems != 1 {
		t.Fatalf("list %s %v", list.Body, err)
	}
}
func TestConcurrentRetryCreatesOneProject(t *testing.T) {
	h := testServer(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := call(h, "POST", "/api/v1/projects", `{"name":"one","canonicalUrl":"https://example.com"}`, "same", "")
			if w.Code != 201 && w.Code != 409 {
				t.Errorf("code=%d %s", w.Code, w.Body)
			}
		}()
	}
	wg.Wait()
	w := call(h, "GET", "/api/v1/projects", "", "", "")
	var page struct{ Pagination struct{ TotalItems int } }
	json.Unmarshal(w.Body.Bytes(), &page)
	if page.Pagination.TotalItems != 1 {
		t.Fatalf("created %d", page.Pagination.TotalItems)
	}
}
func TestHTTPBoundaries(t *testing.T) {
	h := testServer(t)
	cases := []struct {
		method, path, body, origin, host, typ string
		want                                  int
	}{
		{"POST", "/api/v1/projects", `{}`, "https://evil.example", "localhost", "application/json", 403},
		{"GET", "/api/v1/projects", "", "", "evil.example", "", 403},
		{"POST", "/api/v1/projects", `{}`, "", "localhost", "text/plain", 415},
		{"POST", "/api/v1/projects", `{"name":"ok","canonicalUrl":"https://e.test","unknown":true}`, "", "localhost", "application/json", 400},
		{"POST", "/api/v1/projects", `{"name":"ok","canonicalUrl":"https://e.test"} {}`, "", "localhost", "application/json", 400},
		{"POST", "/api/v1/projects", `null`, "", "localhost", "application/json", 400},
		{"GET", "/api/v1/projects?page=-1", "", "", "localhost", "", 422},
		{"GET", "/api/v1/projects/missing", "", "", "localhost", "", 404},
		{"GET", "/api/v1/missing", "", "", "localhost", "", 404},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, "http://localhost"+tc.path, bytes.NewBufferString(tc.body))
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Content-Type", tc.typ)
		r.Header.Set("Idempotency-Key", "test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s %s host=%s body=%s got %d want %d: %s", tc.method, tc.path, tc.host, tc.body, w.Code, tc.want, w.Body)
		}
	}
}

func TestCreateReturnsRevisionAndLimitsPayload(t *testing.T) {
	h := testServer(t)
	created := call(h, "POST", "/api/v1/projects", `{"name":"v","canonicalUrl":"https://version.test"}`, "version-intent", "")
	if created.Header().Get("ETag") != `"1"` {
		t.Fatalf("missing creation revision: %v", created.Header())
	}
	replay := call(h, "POST", "/api/v1/projects", `{"name":"v","canonicalUrl":"https://version.test"}`, "version-intent", "")
	if replay.Header().Get("ETag") != created.Header().Get("ETag") {
		t.Fatal("replay changed revision")
	}
	oversized := `{"name":"x","canonicalUrl":"https://limit.test","summary":"` + string(bytes.Repeat([]byte("a"), 1<<20)) + `"}`
	result := call(h, "POST", "/api/v1/projects", oversized, "oversized", "")
	if result.Code != 413 {
		t.Fatalf("oversized: %d", result.Code)
	}
}

func TestSecurityHeadersApplyToEveryResponse(t *testing.T) {
	h := testServer(t)
	response := call(h, "GET", "/healthz", "", "", "")
	for name, want := range map[string]string{
		"Content-Security-Policy": "frame-ancestors 'none'",
		"Permissions-Policy":      "camera=(), microphone=(), geolocation=()",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
	} {
		if got := response.Header().Get(name); !strings.Contains(got, want) {
			t.Errorf("%s=%q, want it to contain %q", name, got, want)
		}
	}
}
