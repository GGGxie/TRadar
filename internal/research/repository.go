package research

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/text/unicode/norm"
	"tradar/internal/catalog"
	"tradar/internal/fault"
)

const entryColumns = `id,project_id,is_starred,tags_json,stage,decision,rationale,notes,next_action,next_action_at,revision,created_at,updated_at`

func CreateEntry(ctx context.Context, q catalog.Querier, projectID string, patch EntryPatch, now time.Time) (Entry, error) {
	if _, err := catalog.GetProject(ctx, q, projectID); err != nil {
		return Entry{}, err
	}
	entry := Entry{
		ID:        uuid.NewString(),
		ProjectID: projectID,
		Tags:      []string{},
		Stage:     "SCREENING",
		Decision:  "UNDECIDED",
		Revision:  1,
		CreatedAt: catalog.Timestamp(now),
		UpdatedAt: catalog.Timestamp(now),
	}
	applyPatch(&entry, patch)
	if err := validateEntry(&entry); err != nil {
		return Entry{}, err
	}
	tags, _ := json.Marshal(entry.Tags)
	result, err := q.ExecContext(ctx, `INSERT INTO research_entries (`+entryColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(project_id) DO NOTHING`, entry.ID, entry.ProjectID, entry.IsStarred, string(tags), entry.Stage, entry.Decision, entry.Rationale, entry.Notes, entry.NextAction, entry.NextActionAt, entry.Revision, entry.CreatedAt, entry.UpdatedAt)
	if err != nil {
		return Entry{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Entry{}, err
	}
	if count == 0 {
		return Entry{}, fault.New("RESEARCH_ALREADY_EXISTS", "此项目已有研究记录")
	}
	return entry, nil
}

func GetEntry(ctx context.Context, q catalog.Querier, projectID string) (Entry, error) {
	entry, err := scanEntry(q.QueryRowContext(ctx, `SELECT `+entryColumns+` FROM research_entries WHERE project_id=?`, projectID))
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, fault.Missing("研究记录")
	}
	return entry, err
}

func UpdateEntry(ctx context.Context, q catalog.Querier, projectID string, expected int, patch EntryPatch, now time.Time) (Entry, error) {
	if patch.empty() {
		return Entry{}, fault.Field("body", "至少提供一个修改字段")
	}
	entry, err := GetEntry(ctx, q, projectID)
	if err != nil {
		return Entry{}, err
	}
	if entry.Revision != expected {
		return Entry{}, fault.New("VERSION_CONFLICT", "研究记录已被修改，请加载最新版本并手工合并")
	}
	applyPatch(&entry, patch)
	if err = validateEntry(&entry); err != nil {
		return Entry{}, err
	}
	entry.Revision++
	entry.UpdatedAt = catalog.Timestamp(now)
	tags, _ := json.Marshal(entry.Tags)
	result, err := q.ExecContext(ctx, `UPDATE research_entries SET is_starred=?,tags_json=?,stage=?,decision=?,rationale=?,notes=?,next_action=?,next_action_at=?,revision=?,updated_at=? WHERE project_id=? AND revision=?`, entry.IsStarred, string(tags), entry.Stage, entry.Decision, entry.Rationale, entry.Notes, entry.NextAction, entry.NextActionAt, entry.Revision, entry.UpdatedAt, projectID, expected)
	if err != nil {
		return Entry{}, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Entry{}, err
	}
	if count == 0 {
		return Entry{}, fault.New("VERSION_CONFLICT", "研究记录已被修改，请加载最新版本并手工合并")
	}
	return entry, nil
}

func ListEntries(ctx context.Context, q catalog.Querier, filter EntryFilter) ([]Entry, int, error) {
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return nil, 0, fault.Field("page", "分页参数无效")
	}
	if filter.Stage != "" && !validStage(filter.Stage) {
		return nil, 0, fault.Field("stage", "未知研究阶段")
	}
	if filter.Decision != "" && !validDecision(filter.Decision) {
		return nil, 0, fault.Field("decision", "未知研究判断")
	}
	if utf8.RuneCountInString(filter.Query) > 200 {
		return nil, 0, fault.Field("q", "搜索词不能超过 200 字")
	}
	if filter.Tag != "" {
		filter.Tag = norm.NFC.String(strings.TrimSpace(filter.Tag))
		if n := utf8.RuneCountInString(filter.Tag); n < 1 || n > 32 {
			return nil, 0, fault.Field("tag", "标签需要 1–32 字")
		}
	}
	where, args := ` WHERE 1=1`, []any{}
	if filter.IsStarred != nil {
		where += ` AND re.is_starred=?`
		args = append(args, *filter.IsStarred)
	}
	if filter.Stage != "" {
		where += ` AND re.stage=?`
		args = append(args, filter.Stage)
	}
	if filter.Decision != "" {
		where += ` AND re.decision=?`
		args = append(args, filter.Decision)
	}
	if filter.Tag != "" {
		where += ` AND EXISTS (SELECT 1 FROM json_each(re.tags_json) WHERE lower(value)=lower(?))`
		args = append(args, filter.Tag)
	}
	if filter.Query != "" {
		term := "%" + escapeLike(filter.Query) + "%"
		where += ` AND (p.name LIKE ? ESCAPE '\' OR re.rationale LIKE ? ESCAPE '\' OR re.notes LIKE ? ESCAPE '\' OR re.next_action LIKE ? ESCAPE '\' OR EXISTS (SELECT 1 FROM json_each(re.tags_json) WHERE value LIKE ? ESCAPE '\'))`
		args = append(args, term, term, term, term, term)
	}
	var total int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM research_entries re JOIN projects p ON p.id=re.project_id`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := q.QueryContext(ctx, `SELECT `+prefixedColumns("re")+` FROM research_entries re JOIN projects p ON p.id=re.project_id`+where+` ORDER BY re.updated_at DESC,re.id ASC LIMIT ? OFFSET ?`, append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Entry, 0)
	for rows.Next() {
		entry, scanErr := scanEntry(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, entry)
	}
	return items, total, rows.Err()
}

func applyPatch(entry *Entry, patch EntryPatch) {
	if patch.IsStarred != nil {
		entry.IsStarred = *patch.IsStarred
	}
	if patch.Tags != nil {
		entry.Tags = append([]string{}, (*patch.Tags)...)
	}
	if patch.Stage != nil {
		entry.Stage = *patch.Stage
	}
	if patch.Decision != nil {
		entry.Decision = *patch.Decision
	}
	if patch.Rationale != nil {
		entry.Rationale = *patch.Rationale
	}
	if patch.Notes != nil {
		entry.Notes = *patch.Notes
	}
	if patch.NextAction != nil {
		entry.NextAction = *patch.NextAction
	}
	if patch.NextActionAtSet {
		entry.NextActionAt = patch.NextActionAt
	}
}

func validateEntry(entry *Entry) error {
	tags, err := normalizeTags(entry.Tags)
	if err != nil {
		return err
	}
	entry.Tags = tags
	if !validStage(entry.Stage) {
		return fault.Field("stage", "未知研究阶段")
	}
	if !validDecision(entry.Decision) {
		return fault.Field("decision", "未知研究判断")
	}
	if utf8.RuneCountInString(entry.Rationale) > 10000 {
		return fault.Field("rationale", "判断依据不能超过 10000 字")
	}
	if utf8.RuneCountInString(entry.Notes) > 50000 {
		return fault.Field("notes", "研究笔记不能超过 50000 字")
	}
	if utf8.RuneCountInString(entry.NextAction) > 2000 {
		return fault.Field("nextAction", "下一步不能超过 2000 字")
	}
	if entry.NextActionAt != nil {
		parsed, err := time.Parse(time.RFC3339Nano, *entry.NextActionAt)
		if err != nil {
			return fault.Field("nextActionAt", "需要 RFC3339 时间或 null")
		}
		normalized := catalog.Timestamp(parsed)
		entry.NextActionAt = &normalized
	}
	return nil
}

func normalizeTags(values []string) ([]string, error) {
	if len(values) > 20 {
		return nil, fault.Field("tags", "标签最多 20 项")
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = norm.NFC.String(strings.TrimSpace(value))
		if n := utf8.RuneCountInString(value); n < 1 || n > 32 {
			return nil, fault.Field("tags", "每个标签需要 1–32 字")
		}
		duplicate := false
		for _, existing := range result {
			if strings.EqualFold(existing, value) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, value)
		}
	}
	return result, nil
}

func validStage(value string) bool {
	return value == "SCREENING" || value == "DEEP_RESEARCH" || value == "PRACTICE" || value == "REVIEWED" || value == "ARCHIVED"
}

func validDecision(value string) bool {
	return value == "UNDECIDED" || value == "OBSERVE" || value == "LEARN" || value == "USE" || value == "VALIDATE" || value == "PASS"
}

func (patch EntryPatch) empty() bool {
	return patch.IsStarred == nil && patch.Tags == nil && patch.Stage == nil && patch.Decision == nil && patch.Rationale == nil && patch.Notes == nil && patch.NextAction == nil && !patch.NextActionAtSet
}

func scanEntry(row interface{ Scan(...any) error }) (Entry, error) {
	var entry Entry
	var tags string
	var next sql.NullString
	if err := row.Scan(&entry.ID, &entry.ProjectID, &entry.IsStarred, &tags, &entry.Stage, &entry.Decision, &entry.Rationale, &entry.Notes, &entry.NextAction, &next, &entry.Revision, &entry.CreatedAt, &entry.UpdatedAt); err != nil {
		return Entry{}, err
	}
	if err := json.Unmarshal([]byte(tags), &entry.Tags); err != nil {
		return Entry{}, err
	}
	if entry.Tags == nil {
		entry.Tags = []string{}
	}
	if next.Valid {
		entry.NextActionAt = &next.String
	}
	return entry, nil
}

func prefixedColumns(prefix string) string {
	parts := strings.Split(entryColumns, ",")
	for index := range parts {
		parts[index] = prefix + "." + parts[index]
	}
	return strings.Join(parts, ",")
}

func escapeLike(value string) string {
	return strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(value)
}
