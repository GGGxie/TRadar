package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"tradar/internal/fault"
)

type Evidence struct {
	ID             string   `json:"id"`
	ProjectID      string   `json:"projectId"`
	SourceRecordID *string  `json:"sourceRecordId"`
	URL            string   `json:"url"`
	Title          string   `json:"title"`
	Excerpt        string   `json:"excerpt"`
	Kind           string   `json:"kind"`
	Publisher      *string  `json:"publisher"`
	PublishedAt    *string  `json:"publishedAt"`
	CollectedAt    string   `json:"collectedAt"`
	IsPublic       bool     `json:"isPublic"`
	ObservationIDs []string `json:"observationIds"`
	SupersedesID   *string  `json:"supersedesId"`
}

type CreateEvidenceInput struct {
	URL            string   `json:"url"`
	Title          string   `json:"title"`
	Excerpt        string   `json:"excerpt"`
	Kind           string   `json:"kind"`
	Publisher      *string  `json:"publisher"`
	PublishedAt    *string  `json:"publishedAt"`
	IsPublic       *bool    `json:"isPublic"`
	SourceRecordID *string  `json:"sourceRecordId"`
	ObservationIDs []string `json:"observationIds"`
	SupersedesID   *string  `json:"supersedesId"`
}

type EvidenceFilter struct {
	AsOf       *time.Time
	PublicOnly bool
	Page       int
	PageSize   int
}

const evidenceColumns = "id,project_id,source_record_id,url,title,excerpt,kind,publisher,published_at,collected_at,is_public,supersedes_id"

func CreateManualEvidence(ctx context.Context, q *sql.Tx, projectID string, input CreateEvidenceInput, now time.Time) (Evidence, error) {
	if input.Kind == "METRIC_OBSERVATION" || len(input.ObservationIDs) != 0 {
		return Evidence{}, fault.Field("kind", "人工证据不能伪造指标观测")
	}
	return CreateEvidence(ctx, q, projectID, input, now)
}

// CreateEvidence is the catalog boundary used by trusted collectors as well as
// manual entry. HTTP handlers use CreateManualEvidence to exclude metric claims.
func CreateEvidence(ctx context.Context, q *sql.Tx, projectID string, input CreateEvidenceInput, now time.Time) (Evidence, error) {
	if _, err := GetProject(ctx, q, projectID); err != nil {
		return Evidence{}, err
	}
	title := strings.TrimSpace(input.Title)
	excerpt := strings.TrimSpace(input.Excerpt)
	if n := utf8.RuneCountInString(title); n < 1 || n > 500 {
		return Evidence{}, fault.Field("title", "标题需要 1–500 字")
	}
	if n := utf8.RuneCountInString(excerpt); n < 1 || n > 16000 {
		return Evidence{}, fault.Field("excerpt", "摘录需要 1–16000 字")
	}
	if !isEvidenceKind(input.Kind) {
		return Evidence{}, fault.Field("kind", "未知证据种类")
	}
	if input.Kind == "METRIC_OBSERVATION" && len(input.ObservationIDs) == 0 {
		return Evidence{}, fault.Field("observationIds", "指标证据必须引用观测")
	}
	if input.Kind != "METRIC_OBSERVATION" && len(input.ObservationIDs) != 0 {
		return Evidence{}, fault.Field("observationIds", "只有指标证据可以引用观测")
	}
	canonical, err := NormalizeURL(input.URL)
	if err != nil {
		return Evidence{}, fault.Field("url", "需要不含用户名和密码的完整 HTTP(S) 链接")
	}
	publisher, err := optionalText(input.Publisher, 200, "publisher")
	if err != nil {
		return Evidence{}, err
	}
	publishedAt, err := optionalTimestamp(input.PublishedAt, "publishedAt")
	if err != nil {
		return Evidence{}, err
	}
	if err = validateOwnedReference(ctx, q, "source_records", input.SourceRecordID, projectID, "sourceRecordId"); err != nil {
		return Evidence{}, err
	}
	if err = validateOwnedReference(ctx, q, "evidence", input.SupersedesID, projectID, "supersedesId"); err != nil {
		return Evidence{}, err
	}
	if len(input.ObservationIDs) > 100 {
		return Evidence{}, fault.Field("observationIds", "一次最多引用 100 条观测")
	}
	seen := make(map[string]struct{}, len(input.ObservationIDs))
	for _, observationID := range input.ObservationIDs {
		if observationID == "" {
			return Evidence{}, fault.Field("observationIds", "观测 ID 不能为空")
		}
		if _, duplicate := seen[observationID]; duplicate {
			return Evidence{}, fault.Field("observationIds", "观测 ID 不能重复")
		}
		seen[observationID] = struct{}{}
		var owner string
		err = q.QueryRowContext(ctx, `SELECT sr.project_id FROM observations o JOIN source_records sr ON sr.id=o.source_record_id WHERE o.id=?`, observationID).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && owner != projectID) {
			return Evidence{}, fault.Field("observationIds", "观测不存在或不属于此项目")
		}
		if err != nil {
			return Evidence{}, err
		}
	}
	isPublic := false
	if input.IsPublic != nil {
		isPublic = *input.IsPublic
	}
	observationIDs := make([]string, len(input.ObservationIDs))
	copy(observationIDs, input.ObservationIDs)
	evidence := Evidence{
		ID: uuid.NewString(), ProjectID: projectID, SourceRecordID: input.SourceRecordID,
		URL: canonical, Title: title, Excerpt: excerpt, Kind: input.Kind,
		Publisher: publisher, PublishedAt: publishedAt, CollectedAt: Timestamp(now),
		IsPublic: isPublic, ObservationIDs: observationIDs,
		SupersedesID: input.SupersedesID,
	}
	_, err = q.ExecContext(ctx, `INSERT INTO evidence (`+evidenceColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		evidence.ID, evidence.ProjectID, evidence.SourceRecordID, evidence.URL, evidence.Title,
		evidence.Excerpt, evidence.Kind, evidence.Publisher, evidence.PublishedAt,
		evidence.CollectedAt, evidence.IsPublic, evidence.SupersedesID)
	if err != nil {
		return Evidence{}, err
	}
	for _, observationID := range evidence.ObservationIDs {
		if _, err = q.ExecContext(ctx, `INSERT INTO evidence_observations(evidence_id,observation_id) VALUES(?,?)`, evidence.ID, observationID); err != nil {
			return Evidence{}, err
		}
	}
	return evidence, nil
}

func ListEvidence(ctx context.Context, q Querier, projectID string, filter EvidenceFilter) ([]Evidence, int, error) {
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return nil, 0, fault.Field("page", "分页参数无效")
	}
	if _, err := GetProject(ctx, q, projectID); err != nil {
		return nil, 0, err
	}
	where := " WHERE project_id=?"
	args := []any{projectID}
	if filter.AsOf != nil {
		where += " AND collected_at<=?"
		args = append(args, Timestamp(*filter.AsOf))
	}
	if filter.PublicOnly {
		where += " AND is_public=1"
	}
	var total int
	if err := q.QueryRowContext(ctx, "SELECT count(*) FROM evidence"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := q.QueryContext(ctx, `SELECT `+evidenceColumns+`,
		COALESCE((SELECT json_group_array(observation_id) FROM evidence_observations eo WHERE eo.evidence_id=evidence.id),'[]')
		FROM evidence`+where+` ORDER BY collected_at DESC,id ASC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Evidence, 0)
	for rows.Next() {
		item, err := scanEvidence(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func GetEvidence(ctx context.Context, q Querier, id string) (Evidence, error) {
	item, err := scanEvidence(q.QueryRowContext(ctx, `SELECT `+evidenceColumns+`,
		COALESCE((SELECT json_group_array(observation_id) FROM evidence_observations eo WHERE eo.evidence_id=evidence.id),'[]')
		FROM evidence WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Evidence{}, fault.Missing("证据")
	}
	return item, err
}

func scanEvidence(row scanner) (Evidence, error) {
	var item Evidence
	var observationJSON string
	err := row.Scan(&item.ID, &item.ProjectID, &item.SourceRecordID, &item.URL, &item.Title,
		&item.Excerpt, &item.Kind, &item.Publisher, &item.PublishedAt, &item.CollectedAt,
		&item.IsPublic, &item.SupersedesID, &observationJSON)
	if err != nil {
		return Evidence{}, err
	}
	if err = json.Unmarshal([]byte(observationJSON), &item.ObservationIDs); err != nil {
		return Evidence{}, err
	}
	return item, nil
}

func validateOwnedReference(ctx context.Context, q Querier, table string, id *string, projectID, field string) error {
	if id == nil {
		return nil
	}
	if *id == "" {
		return fault.Field(field, "引用 ID 不能为空")
	}
	var owner string
	query := "SELECT project_id FROM " + table + " WHERE id=?" // table is selected only by catalog code.
	err := q.QueryRowContext(ctx, query, *id).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && owner != projectID) {
		return fault.Field(field, "引用不存在或不属于此项目")
	}
	return err
}

func optionalText(value *string, limit int, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if n := utf8.RuneCountInString(trimmed); n < 1 || n > limit {
		return nil, fault.Field(field, "可选文字为空或过长")
	}
	return &trimmed, nil
}

func optionalTimestamp(value *string, field string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, *value)
	if err != nil {
		return nil, fault.Field(field, "需要 RFC3339 时间")
	}
	formatted := Timestamp(parsed)
	return &formatted, nil
}

func isEvidenceKind(kind string) bool {
	switch kind {
	case "METRIC_OBSERVATION", "OFFICIAL_DESCRIPTION", "OFFICIAL_PRICING", "FOUNDER_CLAIM", "THIRD_PARTY_ESTIMATE", "PUBLIC_EXCERPT":
		return true
	default:
		return false
	}
}
