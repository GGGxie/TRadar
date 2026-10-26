package httpapi

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"tradar/internal/analysis"
	"tradar/internal/brief"
	"tradar/internal/fault"
	"tradar/internal/ingestion"
)

type Config struct {
	WebDir, AccessToken, AllowedHost, DevOrigin     string
	HasGitHubCredentials, HasProductHuntCredentials bool
	QueueIngestionRun                               func(ingestion.IngestionRun) error
	QueueBriefRun                                   func(brief.Run) error
	AnalysisModel                                   *analysis.ModelRuntime
}
type Server struct {
	db              *sql.DB
	config          Config
	now             func() time.Time
	mutationMu      sync.Mutex
	activeMutations map[string]struct{}
}
type requestIDKey struct{}

type pageInfo struct {
	Page       int `json:"page"`
	PageSize   int `json:"pageSize"`
	TotalItems int `json:"totalItems"`
	TotalPages int `json:"totalPages"`
}
type response struct {
	Data       any       `json:"data"`
	Pagination *pageInfo `json:"pagination,omitempty"`
}

func New(db *sql.DB, config Config) http.Handler {
	s := &Server{db: db, config: config, now: time.Now, activeMutations: make(map[string]struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/projects", s.projects)
	mux.HandleFunc("/api/v1/projects/{id}", s.project)
	mux.HandleFunc("/api/v1/projects/{id}/evidence", s.projectEvidence)
	mux.HandleFunc("/api/v1/projects/{id}/evidence/from-sources", s.projectEvidenceFromSources)
	mux.HandleFunc("/api/v1/projects/{id}/sources", s.projectSources)
	mux.HandleFunc("/api/v1/projects/{id}/observations", s.projectObservations)
	mux.HandleFunc("/api/v1/projects/{id}/trend", s.projectTrend)
	mux.HandleFunc("/api/v1/projects/{id}/analyses", s.projectAnalyses)
	mux.HandleFunc("/api/v1/projects/{id}/analysis-runs", s.projectAnalysisRuns)
	mux.HandleFunc("/api/v1/analyses/{id}", s.analysisVersion)
	mux.HandleFunc("/api/v1/analysis-runs/{id}", s.analysisRun)
	mux.HandleFunc("/api/v1/analysis-capabilities", s.analysisCapabilities)
	mux.HandleFunc("/api/v1/research", s.researchEntries)
	mux.HandleFunc("/api/v1/projects/{id}/research", s.projectResearch)
	mux.HandleFunc("/api/v1/projects/{id}/experiments", s.projectExperiments)
	mux.HandleFunc("/api/v1/experiments/{id}", s.experiment)
	mux.HandleFunc("/api/v1/research-preferences", s.researchPreferences)
	mux.HandleFunc("/api/v1/research-rhythm", s.researchRhythm)
	mux.HandleFunc("/api/v1/source-records/{id}", s.sourceRecord)
	mux.HandleFunc("/api/v1/sources", s.remoteSources)
	mux.HandleFunc("/api/v1/sources/{source}", s.remoteSource)
	mux.HandleFunc("/api/v1/ingestion-runs", s.ingestionRuns)
	mux.HandleFunc("/api/v1/ingestion-runs/{id}", s.ingestionRun)
	mux.HandleFunc("/api/v1/radar", s.radar)
	mux.HandleFunc("/api/v1/briefs", s.briefs)
	mux.HandleFunc("/api/v1/briefs/{id}", s.briefByID)
	mux.HandleFunc("/api/v1/briefs/{id}/markdown", s.briefMarkdown)
	mux.HandleFunc("/api/v1/brief-runs/{id}", s.briefRun)
	mux.HandleFunc("/api/v1/brief-settings", s.briefSettings)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeError(w, r, fault.Missing("接口")) })
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			methodError(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, 200, response{Data: map[string]string{"status": "ok"}})
	})
	mux.HandleFunc("/", s.static)
	return s.guard(mux)
}

func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		w.Header().Set("X-Request-Id", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		ip := net.ParseIP(strings.Trim(host, "[]"))
		if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) && (s.config.AllowedHost == "" || !strings.EqualFold(host, s.config.AllowedHost)) {
			writeError(w, r, fault.New("FORBIDDEN", "请求主机未授权"))
			return
		}
		if s.config.AccessToken != "" && !isStaticShellRequest(r) {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, "Bearer ")), []byte(s.config.AccessToken)) != 1 {
				writeError(w, r, fault.New("UNAUTHORIZED", "需要访问凭据"))
				return
			}
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			origin := r.Header.Get("Origin")
			if origin != "" {
				u, err := url.Parse(origin)
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				if (err != nil || u.Scheme != scheme || u.Host != r.Host || u.Path != "") && (s.config.DevOrigin == "" || origin != s.config.DevOrigin) {
					writeError(w, r, fault.New("FORBIDDEN", "只允许同源写入"))
					return
				}
			}
			if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				writeError(w, r, fault.New("FORBIDDEN", "不接受跨站写入"))
				return
			}
			typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || typ != "application/json" {
				writeError(w, r, fault.New("UNSUPPORTED_MEDIA_TYPE", "请求正文必须是 application/json"))
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("request panic", "requestId", id)
				writeError(w, r, errors.New("internal panic"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// isStaticShellRequest 判断请求是否只是读取前端构建产物。
// 构建产物不含任何数据，放行后浏览器才能加载页面并提示输入令牌；
// 所有 /api 路径、/healthz 以及非读取方法仍然必须携带令牌。
// 路径清理方式与 static 保持一致，避免 "//api/..." 之类写法绕过判断。
func isStaticShellRequest(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	clean := filepath.Clean("/" + r.URL.Path)
	return clean != "/healthz" && clean != "/api" && !strings.HasPrefix(clean, "/api/")
}

func decode(r *http.Request, dest any) error {
	_, err := decodeObject(r, dest, nil)
	return err
}

func decodeObject(r *http.Request, dest any, allowedNull map[string]bool) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return nil, decodeError(err)
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil, fault.New("BAD_REQUEST", "正文必须为 JSON 对象")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fault.New("BAD_REQUEST", "只允许一个 JSON 对象")
	}
	bodyDecoder := json.NewDecoder(strings.NewReader(string(raw)))
	bodyDecoder.DisallowUnknownFields()
	if err := bodyDecoder.Decode(dest); err != nil {
		return nil, fault.New("BAD_REQUEST", "JSON 字段或格式不正确")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fault.New("BAD_REQUEST", "JSON 无效")
	}
	for key, value := range fields {
		if string(value) == "null" && !allowedNull[key] {
			return nil, fault.Field(key, "该字段不接受 null")
		}
	}
	return fields, nil
}
func decodeError(err error) error {
	var max *http.MaxBytesError
	if errors.As(err, &max) {
		return fault.New("PAYLOAD_TOO_LARGE", "请求正文超过 1 MiB")
	}
	return fault.New("BAD_REQUEST", "无法解析 JSON 正文")
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	code, message, fields := "INTERNAL_ERROR", "服务暂时无法完成请求", map[string]string(nil)
	status := 500
	var app *fault.Error
	if errors.As(err, &app) {
		code, message, fields = app.Code, app.Message, app.Fields
		if app.RetryAt != nil {
			w.Header().Set("Retry-After", app.RetryAt.UTC().Format(http.TimeFormat))
		}
		statuses := map[string]int{"VALIDATION_ERROR": 422, "NOT_FOUND": 404, "VERSION_CONFLICT": 412, "PROJECT_CONTEXT_CHANGED": 412, "PRECONDITION_REQUIRED": 428, "IDEMPOTENCY_MISMATCH": 422, "IDEMPOTENCY_IN_PROGRESS": 409, "RESEARCH_ALREADY_EXISTS": 409, "BRIEF_ALREADY_RUNNING": 409, "BRIEF_RUN_FINISHED": 409, "ANALYSIS_RUN_FINISHED": 409, "SOURCE_BUSY": 409, "SOURCE_DISABLED": 422, "SOURCE_CREDENTIALS_REQUIRED": 422, "SOURCE_RATE_LIMITED": 429, "MODEL_BUDGET_EXCEEDED": 429, "MODEL_DISABLED": 503, "MODEL_INPUT_TOO_LARGE": 422, "BAD_REQUEST": 400, "FORBIDDEN": 403, "UNAUTHORIZED": 401, "UNSUPPORTED_MEDIA_TYPE": 415, "PAYLOAD_TOO_LARGE": 413, "METHOD_NOT_ALLOWED": 405, "CONFLICT": 409}
		if n, ok := statuses[code]; ok {
			status = n
		}
	}
	id, _ := r.Context().Value(requestIDKey{}).(string)
	if status == 500 {
		slog.Error("request failed", "requestId", id, "error", err.Error())
	}
	writeJSON(w, status, map[string]any{"error": struct {
		Code      string            `json:"code"`
		Message   string            `json:"message"`
		RequestID string            `json:"requestId"`
		Fields    map[string]string `json:"fields,omitempty"`
	}{code, message, id, fields}})
}
func methodError(w http.ResponseWriter, r *http.Request) {
	writeError(w, r, fault.New("METHOD_NOT_ALLOWED", "此接口不支持该方法"))
}
func pagination(r *http.Request) (int, int, error) {
	page, size := 1, 20
	for key, p := range map[string]*int{"page": &page, "pageSize": &size} {
		if v := r.URL.Query().Get(key); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return 0, 0, fault.Field(key, "需要正整数")
			}
			*p = n
		}
	}
	if page > 1000000 || size > 100 {
		return 0, 0, fault.Field("pageSize", "pageSize 不能超过 100，page 不能超过 1000000")
	}
	return page, size, nil
}
func revision(r *http.Request) (int, error) {
	raw := r.Header.Get("If-Match")
	if raw == "" {
		return 0, fault.New("PRECONDITION_REQUIRED", "编辑需要 If-Match 版本")
	}
	if len(raw) < 3 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return 0, fault.New("BAD_REQUEST", "If-Match 格式无效")
	}
	n, err := strconv.Atoi(raw[1 : len(raw)-1])
	if err != nil || n < 1 {
		return 0, fault.New("BAD_REQUEST", "If-Match 版本无效")
	}
	return n, nil
}
func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		methodError(w, r)
		return
	}
	if s.config.WebDir == "" {
		http.Error(w, "前端尚未构建；请先运行 npm --prefix web run build", 503)
		return
	}
	clean := filepath.Clean("/" + r.URL.Path)
	for _, part := range strings.Split(clean, "/") {
		if strings.HasPrefix(part, ".") {
			http.NotFound(w, r)
			return
		}
	}
	file := filepath.Join(s.config.WebDir, clean)
	info, err := os.Stat(file)
	if err != nil || info.IsDir() {
		if clean != "/" && clean != "/radar" && clean != "/library" && clean != "/settings" && clean != "/briefs" && !strings.HasPrefix(clean, "/projects/") {
			http.NotFound(w, r)
			return
		}
		file = filepath.Join(s.config.WebDir, "index.html")
	}
	http.ServeFile(w, r, file)
}
