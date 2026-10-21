package httpapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testAccessToken = "0123456789abcdef0123456789abcdef"

func protectedServerWithWeb(t *testing.T) http.Handler {
	t.Helper()
	_, db := testServerWithDB(t)
	webDir := t.TempDir()
	files := map[string]string{
		"index.html":    "<!doctype html><title>shell</title>",
		"assets/app.js": "console.log('app')",
		".secret":       "do-not-serve",
	}
	for name, content := range files {
		path := filepath.Join(webDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return New(db, Config{AccessToken: testAccessToken, WebDir: webDir})
}

func get(h http.Handler, method, path, authorization string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "http://localhost"+path, nil)
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	return response
}

func TestStaticShellIsReachableWithoutTokenWhileAPIStaysProtected(t *testing.T) {
	h := protectedServerWithWeb(t)
	valid := "Bearer " + testAccessToken

	for name, tc := range map[string]struct {
		method, path, authorization string
		want                        int
	}{
		"index without token":       {http.MethodGet, "/", "", http.StatusOK},
		"spa route without token":   {http.MethodGet, "/radar", "", http.StatusOK},
		"project route":             {http.MethodGet, "/projects/abc", "", http.StatusOK},
		"asset without token":       {http.MethodGet, "/assets/app.js", "", http.StatusOK},
		"head on shell":             {http.MethodHead, "/radar", "", http.StatusOK},
		"api without token":         {http.MethodGet, "/api/v1/projects", "", http.StatusUnauthorized},
		"api with wrong token":      {http.MethodGet, "/api/v1/projects", "Bearer wrong", http.StatusUnauthorized},
		"api with token":            {http.MethodGet, "/api/v1/projects", valid, http.StatusOK},
		"unknown api without token": {http.MethodGet, "/api/v1/missing", "", http.StatusUnauthorized},
		"healthz without token":     {http.MethodGet, "/healthz", "", http.StatusUnauthorized},
		"healthz with token":        {http.MethodGet, "/healthz", valid, http.StatusOK},
		"write to shell path":       {http.MethodPost, "/radar", "", http.StatusUnauthorized},
		"dotfile stays hidden":      {http.MethodGet, "/.secret", "", http.StatusNotFound},
		"unknown path":              {http.MethodGet, "/nope", "", http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			response := get(h, tc.method, tc.path, tc.authorization)
			if response.Code != tc.want {
				t.Fatalf("%s %s: status=%d want=%d body=%s", tc.method, tc.path, response.Code, tc.want, response.Body)
			}
		})
	}
}

func TestStaticShellServesBuiltFilesWithoutLeakingToken(t *testing.T) {
	h := protectedServerWithWeb(t)
	response := get(h, http.MethodGet, "/radar", "")
	if !strings.Contains(response.Body.String(), "<title>shell</title>") {
		t.Fatalf("expected index.html, got %q", response.Body)
	}
	if strings.Contains(response.Body.String(), testAccessToken) || strings.Contains(response.Header().Get("Set-Cookie"), testAccessToken) {
		t.Fatal("access token leaked into the static response")
	}
	if response.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("security headers must still apply to the static shell")
	}
}

func TestStaticShellStillRejectsUnauthorizedHost(t *testing.T) {
	h := protectedServerWithWeb(t)
	request := httptest.NewRequest(http.MethodGet, "http://evil.example/radar", nil)
	request.Host = "evil.example"
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d", response.Code)
	}
}

func TestStaticRejectsPathTraversal(t *testing.T) {
	h := protectedServerWithWeb(t)
	for _, path := range []string{"/../../etc/passwd", "/assets/../../etc/passwd", "/%2e%2e/%2e%2e/etc/passwd"} {
		response := get(h, http.MethodGet, path, "")
		if strings.Contains(response.Body.String(), "root:") {
			t.Fatalf("%s leaked a system file", path)
		}
	}
}
