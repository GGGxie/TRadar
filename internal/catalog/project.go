package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"tradar/internal/fault"
)

type Querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Project struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	CanonicalURL string `json:"canonicalUrl"`
	Summary      string `json:"summary"`
	Category     string `json:"category"`
	FirstSeenAt  string `json:"firstSeenAt"`
	LastSeenAt   string `json:"lastSeenAt"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
	Revision     int    `json:"revision"`
}
type CreateProjectInput struct {
	Name         string `json:"name"`
	CanonicalURL string `json:"canonicalUrl"`
	Summary      string `json:"summary"`
	Category     string `json:"category"`
}
type ProjectPatch struct {
	Name     *string `json:"name"`
	Summary  *string `json:"summary"`
	Category *string `json:"category"`
}
type ProjectFilter struct {
	Query, Category string
	Page, PageSize  int
}

const projectColumns = "id,name,canonical_url,summary,category,first_seen_at,last_seen_at,created_at,updated_at,revision"
const timestampFormat = "2006-01-02T15:04:05.000000000Z"

func Timestamp(t time.Time) string { return t.UTC().Format(timestampFormat) }
func IsCategory(s string) bool {
	return s == "AI_AGENT" || s == "DEVTOOLS" || s == "SAAS" || s == "OTHER"
}

func CreateProject(ctx context.Context, q Querier, in CreateProjectInput, now time.Time) (Project, bool, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Category == "" {
		in.Category = "OTHER"
	}
	if err := validateProject(in.Name, in.Summary, in.Category); err != nil {
		return Project{}, false, err
	}
	canonical, err := NormalizeURL(in.CanonicalURL)
	if err != nil {
		return Project{}, false, fault.Field("canonicalUrl", "需要不含用户名和密码的完整 HTTP(S) 链接")
	}
	p := Project{ID: uuid.NewString(), Name: in.Name, CanonicalURL: canonical, Summary: in.Summary, Category: in.Category, FirstSeenAt: Timestamp(now), LastSeenAt: Timestamp(now), CreatedAt: Timestamp(now), UpdatedAt: Timestamp(now), Revision: 1}
	result, err := q.ExecContext(ctx, `INSERT INTO projects (`+projectColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(canonical_url) DO NOTHING`, p.ID, p.Name, p.CanonicalURL, p.Summary, p.Category, p.FirstSeenAt, p.LastSeenAt, p.CreatedAt, p.UpdatedAt, p.Revision)
	if err != nil {
		return Project{}, false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Project{}, false, err
	}
	if count == 0 {
		existing, err := scanProject(q.QueryRowContext(ctx, "SELECT "+projectColumns+" FROM projects WHERE canonical_url=?", canonical))
		return existing, false, err
	}
	return p, true, nil
}

func GetProject(ctx context.Context, q Querier, id string) (Project, error) {
	p, err := scanProject(q.QueryRowContext(ctx, "SELECT "+projectColumns+" FROM projects WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, fault.Missing("项目")
	}
	return p, err
}

// searchLink 把看起来像链接的搜索词规范化；省略协议时按 https 处理。不发起任何请求。
func searchLink(query string) (string, bool) {
	query = strings.TrimSpace(query)
	if strings.ContainsAny(query, " \t") || !strings.Contains(query, ".") {
		return "", false
	}
	if !strings.Contains(query, "://") {
		host, _, _ := strings.Cut(query, "/")
		if !strings.Contains(host, ".") {
			return "", false
		}
		query = "https://" + query
	}
	normalized, err := NormalizeURL(query)
	if err != nil {
		return "", false
	}
	return normalized, true
}

func ListProjects(ctx context.Context, q Querier, f ProjectFilter) ([]Project, int, error) {
	if f.Page < 1 || f.PageSize < 1 || f.PageSize > 100 {
		return nil, 0, fault.Field("page", "分页参数无效")
	}
	if f.Category != "" && !IsCategory(f.Category) {
		return nil, 0, fault.Field("category", "未知分类")
	}
	if utf8.RuneCountInString(f.Query) > 200 {
		return nil, 0, fault.Field("q", "搜索词不能超过 200 字")
	}
	where := " WHERE 1=1"
	args := []any{}
	if f.Category != "" {
		where += " AND category=?"
		args = append(args, f.Category)
	}
	if f.Query != "" {
		term := "%" + strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(f.Query) + "%"
		if link, ok := searchLink(f.Query); ok {
			// #60：粘贴的链接按规范化结果匹配项目链接或别名，同时保留文字匹配。
			where += ` AND (name LIKE ? ESCAPE '\' OR summary LIKE ? ESCAPE '\' OR canonical_url=? OR id IN (SELECT project_id FROM project_aliases WHERE normalized_url=?))`
			args = append(args, term, term, link, link)
		} else {
			where += ` AND (name LIKE ? ESCAPE '\' OR summary LIKE ? ESCAPE '\')`
			args = append(args, term, term)
		}
	}
	var total int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM projects"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, f.PageSize, (f.Page-1)*f.PageSize)
	rows, err := q.QueryContext(ctx, "SELECT "+projectColumns+" FROM projects"+where+" ORDER BY first_seen_at DESC,id ASC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, p)
	}
	return items, total, rows.Err()
}

func UpdateProject(ctx context.Context, q Querier, id string, expected int, patch ProjectPatch, now time.Time) (Project, error) {
	if patch.Name == nil && patch.Summary == nil && patch.Category == nil {
		return Project{}, fault.Field("body", "至少提供一个修改字段")
	}
	p, err := GetProject(ctx, q, id)
	if err != nil {
		return Project{}, err
	}
	if p.Revision != expected {
		return Project{}, fault.New("VERSION_CONFLICT", "项目已被修改，请加载最新版本后重试")
	}
	if patch.Name != nil {
		p.Name = strings.TrimSpace(*patch.Name)
	}
	if patch.Summary != nil {
		p.Summary = *patch.Summary
	}
	if patch.Category != nil {
		p.Category = *patch.Category
	}
	if err = validateProject(p.Name, p.Summary, p.Category); err != nil {
		return Project{}, err
	}
	p.UpdatedAt = Timestamp(now)
	p.Revision++
	result, err := q.ExecContext(ctx, "UPDATE projects SET name=?,summary=?,category=?,updated_at=?,revision=? WHERE id=? AND revision=?", p.Name, p.Summary, p.Category, p.UpdatedAt, p.Revision, id, expected)
	if err != nil {
		return Project{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Project{}, err
	}
	if n == 0 {
		return Project{}, fault.New("VERSION_CONFLICT", "项目已被修改")
	}
	return p, nil
}

func validateProject(name, summary, category string) error {
	if n := utf8.RuneCountInString(name); n < 1 || n > 200 {
		return fault.Field("name", "名称需要 1–200 字")
	}
	if utf8.RuneCountInString(summary) > 4000 {
		return fault.Field("summary", "摘要不能超过 4000 字")
	}
	if !IsCategory(category) {
		return fault.Field("category", "未知项目分类")
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanProject(row scanner) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.Name, &p.CanonicalURL, &p.Summary, &p.Category, &p.FirstSeenAt, &p.LastSeenAt, &p.CreatedAt, &p.UpdatedAt, &p.Revision)
	return p, err
}
