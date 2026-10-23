package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// DerivedEvidence is the result of materializing source records as evidence.
type DerivedEvidence struct {
	Created  int        `json:"created"`
	Evidence []Evidence `json:"evidence"`
}

var sourceEvidenceLabels = map[string]string{"GITHUB": "GitHub", "HACKER_NEWS": "Hacker News", "PRODUCT_HUNT": "Product Hunt"}
var sourceEvidenceMetrics = map[string][]string{
	"GITHUB":       {"STARS", "FORKS"},
	"HACKER_NEWS":  {"POINTS", "COMMENTS"},
	"PRODUCT_HUNT": {"VOTES", "COMMENTS"},
}
var sourceEvidenceMetricLabels = map[string]string{"STARS": "Stars", "FORKS": "Forks", "POINTS": "分数", "COMMENTS": "评论", "VOTES": "票数"}

// DeriveSourceEvidence freezes the project's current automatic source records
// into immutable, public evidence: the source description (or HN title) and the
// latest cumulative metrics, which cite real observation IDs. Source records are
// updated in place by collectors, so analyses must cite these frozen copies.
// Identical content is not stored twice.
func DeriveSourceEvidence(ctx context.Context, tx *sql.Tx, projectID string, now time.Time) (DerivedEvidence, error) {
	result := DerivedEvidence{Evidence: []Evidence{}}
	if _, err := GetProject(ctx, tx, projectID); err != nil {
		return result, err
	}
	records := make([]SourceRecord, 0)
	for page := 1; ; page++ {
		items, total, err := ListSources(ctx, tx, projectID, SourceFilter{Page: page, PageSize: 100})
		if err != nil {
			return result, err
		}
		records = append(records, items...)
		if page*100 >= total {
			break
		}
	}
	public := true
	for _, record := range records {
		label, automatic := sourceEvidenceLabels[record.Source]
		if !automatic {
			continue
		}
		recordID := record.ID
		inputs := make([]CreateEvidenceInput, 0, 2)
		text := CreateEvidenceInput{
			URL: humanSourceURL(record), Publisher: &label, PublishedAt: record.PublishedAt,
			IsPublic: &public, SourceRecordID: &recordID,
		}
		if excerpt := strings.TrimSpace(record.Excerpt); excerpt != "" {
			text.Kind, text.Title, text.Excerpt = "OFFICIAL_DESCRIPTION", clip(label+" 来源描述："+record.Title, 500), clip(excerpt, 16000)
		} else {
			text.Kind, text.Title, text.Excerpt = "PUBLIC_EXCERPT", clip(label+" 讨论标题", 500), clip(strings.TrimSpace(record.Title), 16000)
		}
		if text.Excerpt != "" {
			inputs = append(inputs, text)
		}
		metric, ok, err := latestMetricEvidence(ctx, tx, record, label, now)
		if err != nil {
			return result, err
		}
		if ok {
			metric.IsPublic, metric.SourceRecordID, metric.Publisher = &public, &recordID, &label
			inputs = append(inputs, metric)
		}
		for _, input := range inputs {
			existing, found, err := findDerivedEvidence(ctx, tx, projectID, recordID, input.Kind, input.Excerpt)
			if err != nil {
				return result, err
			}
			if found {
				result.Evidence = append(result.Evidence, existing)
				continue
			}
			created, err := CreateEvidence(ctx, tx, projectID, input, now)
			if err != nil {
				return result, err
			}
			result.Created++
			result.Evidence = append(result.Evidence, created)
		}
	}
	return result, nil
}

func humanSourceURL(record SourceRecord) string {
	// HN 的标题和分数属于讨论页；GitHub/PH 的 API 地址不适合阅读，使用主页。
	if record.Source != "HACKER_NEWS" && record.TargetURL != nil {
		return *record.TargetURL
	}
	return record.URL
}

func latestMetricEvidence(ctx context.Context, tx *sql.Tx, record SourceRecord, label string, now time.Time) (CreateEvidenceInput, bool, error) {
	parts := make([]string, 0, 2)
	ids := make([]string, 0, 2)
	var observedAt string
	for _, metric := range sourceEvidenceMetrics[record.Source] {
		var id, observed string
		var value int64
		err := tx.QueryRowContext(ctx, `SELECT id,value,observed_at FROM observations
			WHERE source_record_id=? AND metric=? AND basis='CUMULATIVE' AND observed_at<=?
			ORDER BY observed_at DESC,fetched_at DESC,id DESC LIMIT 1`, record.ID, metric, Timestamp(now)).Scan(&id, &value, &observed)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return CreateEvidenceInput{}, false, err
		}
		parts = append(parts, sourceEvidenceMetricLabels[metric]+" "+groupDigits(value))
		ids = append(ids, id)
		if observed > observedAt {
			observedAt = observed
		}
	}
	if len(ids) == 0 {
		return CreateEvidenceInput{}, false, nil
	}
	when := observedAt
	if parsed, err := time.Parse(time.RFC3339Nano, observedAt); err == nil {
		when = parsed.UTC().Format("2006-01-02 15:04 UTC")
	}
	return CreateEvidenceInput{
		URL: humanSourceURL(record), Kind: "METRIC_OBSERVATION",
		Title:          clip(label+" 累计指标："+record.Title, 500),
		Excerpt:        fmt.Sprintf("%s 累计 %s（观测于 %s）。累计量只说明关注度，不代表使用或付费。", label, strings.Join(parts, "、"), when),
		ObservationIDs: ids,
	}, true, nil
}

func findDerivedEvidence(ctx context.Context, tx *sql.Tx, projectID, sourceRecordID, kind, excerpt string) (Evidence, bool, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM evidence WHERE project_id=? AND source_record_id=? AND kind=? AND excerpt=? ORDER BY collected_at DESC,id ASC LIMIT 1`,
		projectID, sourceRecordID, kind, excerpt).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Evidence{}, false, nil
	}
	if err != nil {
		return Evidence{}, false, err
	}
	item, err := GetEvidence(ctx, tx, id)
	return item, err == nil, err
}

func groupDigits(value int64) string {
	raw := strconv.FormatInt(value, 10)
	negative := strings.HasPrefix(raw, "-")
	raw = strings.TrimPrefix(raw, "-")
	var builder strings.Builder
	for index, digit := range raw {
		if index > 0 && (len(raw)-index)%3 == 0 {
			builder.WriteByte(',')
		}
		builder.WriteRune(digit)
	}
	if negative {
		return "-" + builder.String()
	}
	return builder.String()
}

func clip(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit-1]) + "…"
}
