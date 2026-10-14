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

type SourceRecord struct {
	ID          string  `json:"id"`
	ProjectID   string  `json:"projectId"`
	Source      string  `json:"source"`
	ExternalID  string  `json:"externalId"`
	RecordType  string  `json:"recordType"`
	URL         string  `json:"url"`
	TargetURL   *string `json:"targetUrl"`
	Title       string  `json:"title"`
	Excerpt     string  `json:"excerpt"`
	PublishedAt *string `json:"publishedAt"`
	FetchedAt   string  `json:"fetchedAt"`
	Revision    int     `json:"revision"`
}

type UpsertSourceInput struct {
	ProjectID   string
	Source      string
	ExternalID  string
	RecordType  string
	URL         string
	TargetURL   *string
	Title       string
	Excerpt     string
	PublishedAt *string
	FetchedAt   time.Time
}

type SourceFilter struct {
	Page, PageSize int
}

const sourceColumns = "id,project_id,source,external_id,record_type,url,target_url,title,excerpt,published_at,fetched_at,revision"

func UpsertSource(ctx context.Context, q *sql.Tx, input UpsertSourceInput) (SourceRecord, bool, error) {
	if _, err := GetProject(ctx, q, input.ProjectID); err != nil {
		return SourceRecord{}, false, err
	}
	input.ExternalID = strings.TrimSpace(input.ExternalID)
	input.Title = strings.TrimSpace(input.Title)
	if !isSource(input.Source) {
		return SourceRecord{}, false, fault.Field("source", "未知来源")
	}
	if !isRecordType(input.RecordType) {
		return SourceRecord{}, false, fault.Field("recordType", "未知来源记录类型")
	}
	if n := utf8.RuneCountInString(input.ExternalID); n < 1 || n > 500 {
		return SourceRecord{}, false, fault.Field("externalId", "外部 ID 需要 1–500 字")
	}
	if n := utf8.RuneCountInString(input.Title); n < 1 || n > 500 {
		return SourceRecord{}, false, fault.Field("title", "标题需要 1–500 字")
	}
	if utf8.RuneCountInString(input.Excerpt) > 16000 {
		return SourceRecord{}, false, fault.Field("excerpt", "摘录不能超过 16000 字")
	}
	if input.FetchedAt.IsZero() {
		return SourceRecord{}, false, fault.Field("fetchedAt", "采集时间不能为空")
	}
	url, err := NormalizeURL(input.URL)
	if err != nil {
		return SourceRecord{}, false, fault.Field("url", "来源 URL 无效")
	}
	targetURL, err := normalizeOptionalURL(input.TargetURL, "targetUrl")
	if err != nil {
		return SourceRecord{}, false, err
	}
	publishedAt, err := optionalTimestamp(input.PublishedAt, "publishedAt")
	if err != nil {
		return SourceRecord{}, false, err
	}
	fetchedAt := Timestamp(input.FetchedAt)
	candidate := SourceRecord{
		ID: uuid.NewString(), ProjectID: input.ProjectID, Source: input.Source,
		ExternalID: input.ExternalID, RecordType: input.RecordType, URL: url,
		TargetURL: targetURL, Title: input.Title, Excerpt: input.Excerpt,
		PublishedAt: publishedAt, FetchedAt: fetchedAt, Revision: 1,
	}
	result, err := q.ExecContext(ctx, `INSERT INTO source_records (`+sourceColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(source,external_id) DO NOTHING`,
		candidate.ID, candidate.ProjectID, candidate.Source, candidate.ExternalID,
		candidate.RecordType, candidate.URL, candidate.TargetURL, candidate.Title,
		candidate.Excerpt, candidate.PublishedAt, candidate.FetchedAt, candidate.Revision)
	if err != nil {
		return SourceRecord{}, false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return SourceRecord{}, false, err
	}
	created := count == 1
	if !created {
		existing, err := scanSource(q.QueryRowContext(ctx, "SELECT "+sourceColumns+" FROM source_records WHERE source=? AND external_id=?", input.Source, input.ExternalID))
		if err != nil {
			return SourceRecord{}, false, err
		}
		candidate.ID = existing.ID
		candidate.ProjectID = existing.ProjectID
		candidate.Revision = existing.Revision + 1
		update, err := q.ExecContext(ctx, `UPDATE source_records SET record_type=?,url=?,target_url=?,title=?,excerpt=?,published_at=?,fetched_at=?,revision=? WHERE id=? AND revision=?`,
			candidate.RecordType, candidate.URL, candidate.TargetURL, candidate.Title,
			candidate.Excerpt, candidate.PublishedAt, candidate.FetchedAt,
			candidate.Revision, candidate.ID, existing.Revision)
		if err != nil {
			return SourceRecord{}, false, err
		}
		updated, err := update.RowsAffected()
		if err != nil {
			return SourceRecord{}, false, err
		}
		if updated != 1 {
			return SourceRecord{}, false, fault.New("VERSION_CONFLICT", "来源记录已被修改")
		}
	}
	if candidate.RecordType == "PROJECT" {
		alias := candidate.URL
		if candidate.TargetURL != nil {
			alias = *candidate.TargetURL
		}
		if err = storeProjectAlias(ctx, q, candidate.ProjectID, alias, fetchedAt); err != nil {
			return SourceRecord{}, false, err
		}
	}
	if _, err = q.ExecContext(ctx, `UPDATE projects SET last_seen_at=? WHERE id=? AND last_seen_at<?`, fetchedAt, candidate.ProjectID, fetchedAt); err != nil {
		return SourceRecord{}, false, err
	}
	return candidate, created, nil
}

func GetSource(ctx context.Context, q Querier, id string) (SourceRecord, error) {
	item, err := scanSource(q.QueryRowContext(ctx, "SELECT "+sourceColumns+" FROM source_records WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return SourceRecord{}, fault.Missing("来源记录")
	}
	return item, err
}

func ListSources(ctx context.Context, q Querier, projectID string, filter SourceFilter) ([]SourceRecord, int, error) {
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return nil, 0, fault.Field("page", "分页参数无效")
	}
	if _, err := GetProject(ctx, q, projectID); err != nil {
		return nil, 0, err
	}
	var total int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM source_records WHERE project_id=?`, projectID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.QueryContext(ctx, `SELECT `+sourceColumns+` FROM source_records WHERE project_id=? ORDER BY source ASC,id ASC LIMIT ? OFFSET ?`, projectID, filter.PageSize, (filter.Page-1)*filter.PageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]SourceRecord, 0)
	for rows.Next() {
		item, err := scanSource(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func ReassociateSource(ctx context.Context, q *sql.Tx, id string, expected int, projectID, reason string, now time.Time) (SourceRecord, error) {
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 1 || n > 1000 {
		return SourceRecord{}, fault.Field("reason", "关联原因需要 1–1000 字")
	}
	if _, err := GetProject(ctx, q, projectID); err != nil {
		return SourceRecord{}, err
	}
	item, err := GetSource(ctx, q, id)
	if err != nil {
		return SourceRecord{}, err
	}
	if item.Revision != expected {
		return SourceRecord{}, fault.New("VERSION_CONFLICT", "来源记录已被修改，请加载最新版本后重试")
	}
	if item.ProjectID == projectID {
		return SourceRecord{}, fault.Field("projectId", "来源记录已经属于此项目")
	}
	oldProjectID := item.ProjectID
	item.ProjectID = projectID
	item.Revision++
	result, err := q.ExecContext(ctx, `UPDATE source_records SET project_id=?,revision=? WHERE id=? AND revision=?`, projectID, item.Revision, id, expected)
	if err != nil {
		return SourceRecord{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return SourceRecord{}, err
	}
	if count != 1 {
		return SourceRecord{}, fault.New("VERSION_CONFLICT", "来源记录已被修改")
	}
	_, err = q.ExecContext(ctx, `INSERT INTO association_events(id,source_record_id,old_project_id,new_project_id,reason,changed_at) VALUES(?,?,?,?,?,?)`,
		uuid.NewString(), id, oldProjectID, projectID, reason, Timestamp(now))
	if err != nil {
		return SourceRecord{}, err
	}
	return item, nil
}

func scanSource(row scanner) (SourceRecord, error) {
	var item SourceRecord
	err := row.Scan(&item.ID, &item.ProjectID, &item.Source, &item.ExternalID,
		&item.RecordType, &item.URL, &item.TargetURL, &item.Title, &item.Excerpt,
		&item.PublishedAt, &item.FetchedAt, &item.Revision)
	return item, err
}

func storeProjectAlias(ctx context.Context, q Querier, projectID, normalizedURL, createdAt string) error {
	_, err := q.ExecContext(ctx, `INSERT INTO project_aliases(normalized_url,project_id,created_at) VALUES(?,?,?) ON CONFLICT(normalized_url) DO NOTHING`, normalizedURL, projectID, createdAt)
	if err != nil {
		return err
	}
	var owner string
	if err = q.QueryRowContext(ctx, `SELECT project_id FROM project_aliases WHERE normalized_url=?`, normalizedURL).Scan(&owner); err != nil {
		return err
	}
	if owner != projectID {
		return fault.Field("targetUrl", "来源链接已属于另一个项目，需要人工确认")
	}
	return nil
}

func normalizeOptionalURL(value *string, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	normalized, err := NormalizeURL(*value)
	if err != nil {
		return nil, fault.Field(field, "URL 无效")
	}
	return &normalized, nil
}

func isSource(value string) bool {
	return value == "GITHUB" || value == "HACKER_NEWS" || value == "PRODUCT_HUNT" || value == "MANUAL"
}

func isRecordType(value string) bool {
	return value == "PROJECT" || value == "DISCUSSION" || value == "LAUNCH" || value == "STORY"
}
