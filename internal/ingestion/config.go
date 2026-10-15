package ingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"tradar/internal/catalog"
	"tradar/internal/fault"
)

type RemoteSource string

const (
	GitHub      RemoteSource = "GITHUB"
	HackerNews  RemoteSource = "HACKER_NEWS"
	ProductHunt RemoteSource = "PRODUCT_HUNT"
)

const (
	AvailabilityAvailable          = "AVAILABLE"
	AvailabilityDisabled           = "DISABLED"
	AvailabilityMissingCredentials = "MISSING_CREDENTIALS"
)

type SourceConfig struct {
	Source                 RemoteSource
	IsEnabled              bool
	IsScheduleEnabled      bool
	IntervalMinutes        int
	Keywords               []string
	DiscoveryDays          int
	CandidatesPerKeyword   int
	TrackedSourceRecordIDs []string
	RequestBudgetPerHour   int
	Feeds                  []string
	ItemsPerFeed           int
	MaxPages               int
	Revision               int
	NextScheduledAt        *string
	NextRetryAt            *string
}

func (c SourceConfig) MarshalJSON() ([]byte, error) {
	common := map[string]any{
		"source": c.Source, "isEnabled": c.IsEnabled,
		"isScheduleEnabled": c.IsScheduleEnabled, "intervalMinutes": c.IntervalMinutes,
	}
	switch c.Source {
	case GitHub:
		common["keywords"] = nonNilStrings(c.Keywords)
		common["discoveryDays"] = c.DiscoveryDays
		common["candidatesPerKeyword"] = c.CandidatesPerKeyword
		common["trackedSourceRecordIds"] = nonNilStrings(c.TrackedSourceRecordIDs)
		common["requestBudgetPerHour"] = c.RequestBudgetPerHour
	case HackerNews:
		common["feeds"] = nonNilStrings(c.Feeds)
		common["itemsPerFeed"] = c.ItemsPerFeed
		common["trackedSourceRecordIds"] = nonNilStrings(c.TrackedSourceRecordIDs)
	case ProductHunt:
		common["discoveryDays"] = c.DiscoveryDays
		common["maxPages"] = c.MaxPages
	}
	return json.Marshal(common)
}

func (c *SourceConfig) UnmarshalJSON(data []byte) error {
	type wire struct {
		Source                 RemoteSource `json:"source"`
		IsEnabled              bool         `json:"isEnabled"`
		IsScheduleEnabled      bool         `json:"isScheduleEnabled"`
		IntervalMinutes        int          `json:"intervalMinutes"`
		Keywords               []string     `json:"keywords"`
		DiscoveryDays          int          `json:"discoveryDays"`
		CandidatesPerKeyword   int          `json:"candidatesPerKeyword"`
		TrackedSourceRecordIDs []string     `json:"trackedSourceRecordIds"`
		RequestBudgetPerHour   int          `json:"requestBudgetPerHour"`
		Feeds                  []string     `json:"feeds"`
		ItemsPerFeed           int          `json:"itemsPerFeed"`
		MaxPages               int          `json:"maxPages"`
	}
	var value wire
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*c = SourceConfig{
		Source: value.Source, IsEnabled: value.IsEnabled, IsScheduleEnabled: value.IsScheduleEnabled,
		IntervalMinutes: value.IntervalMinutes, Keywords: value.Keywords, DiscoveryDays: value.DiscoveryDays,
		CandidatesPerKeyword: value.CandidatesPerKeyword, TrackedSourceRecordIDs: value.TrackedSourceRecordIDs,
		RequestBudgetPerHour: value.RequestBudgetPerHour, Feeds: value.Feeds,
		ItemsPerFeed: value.ItemsPerFeed, MaxPages: value.MaxPages,
	}
	return nil
}

type SourceConfigPatch struct {
	IsEnabled              *bool     `json:"isEnabled"`
	IsScheduleEnabled      *bool     `json:"isScheduleEnabled"`
	IntervalMinutes        *int      `json:"intervalMinutes"`
	Keywords               *[]string `json:"keywords"`
	DiscoveryDays          *int      `json:"discoveryDays"`
	CandidatesPerKeyword   *int      `json:"candidatesPerKeyword"`
	TrackedSourceRecordIDs *[]string `json:"trackedSourceRecordIds"`
	RequestBudgetPerHour   *int      `json:"requestBudgetPerHour"`
	Feeds                  *[]string `json:"feeds"`
	ItemsPerFeed           *int      `json:"itemsPerFeed"`
	MaxPages               *int      `json:"maxPages"`
}

type Credentials struct {
	GitHub, ProductHunt bool
}

type SourceView struct {
	Source            RemoteSource `json:"source"`
	Config            SourceConfig `json:"config"`
	Revision          int          `json:"revision"`
	HasCredentials    bool         `json:"hasCredentials"`
	Availability      string       `json:"availability"`
	LastRunID         *string      `json:"lastRunId"`
	LastSuccessfulAt  *string      `json:"lastSuccessfulAt"`
	LastObservationAt *string      `json:"lastObservationAt"`
	NextScheduledAt   *string      `json:"nextScheduledAt"`
	NextRetryAt       *string      `json:"nextRetryAt"`
	Budget            BudgetView   `json:"budget"`
}

type BudgetView struct {
	RemainingRequests *int    `json:"remainingRequests"`
	ResetAt           *string `json:"resetAt"`
}

type SourceViewFilter struct{ Page, PageSize int }

func IsRemoteSource(source RemoteSource) bool {
	return source == GitHub || source == HackerNews || source == ProductHunt
}

func GetSourceConfig(ctx context.Context, q catalog.Querier, source RemoteSource) (SourceConfig, error) {
	if !IsRemoteSource(source) {
		return SourceConfig{}, fault.Missing("来源")
	}
	var raw string
	var revision int
	var nextScheduled, nextRetry sql.NullString
	err := q.QueryRowContext(ctx, `SELECT config_json,revision,next_scheduled_at,next_retry_at FROM source_configs WHERE source=?`, source).Scan(&raw, &revision, &nextScheduled, &nextRetry)
	if errors.Is(err, sql.ErrNoRows) {
		return SourceConfig{}, fault.Missing("来源")
	}
	if err != nil {
		return SourceConfig{}, err
	}
	var config SourceConfig
	if err = json.Unmarshal([]byte(raw), &config); err != nil {
		return SourceConfig{}, err
	}
	if config.Source != source {
		return SourceConfig{}, errors.New("stored source config has mismatched source")
	}
	config.Revision = revision
	config.NextScheduledAt = nullStringPointer(nextScheduled)
	config.NextRetryAt = nullStringPointer(nextRetry)
	return config, nil
}

func UpdateSourceConfig(ctx context.Context, tx *sql.Tx, source RemoteSource, expected int, patch SourceConfigPatch, hasCredentials bool, now time.Time) (SourceConfig, error) {
	if patch.empty() {
		return SourceConfig{}, fault.Field("body", "至少提供一个修改字段")
	}
	config, err := GetSourceConfig(ctx, tx, source)
	if err != nil {
		return SourceConfig{}, err
	}
	if config.Revision != expected {
		return SourceConfig{}, fault.New("VERSION_CONFLICT", "来源设置已被修改，请加载最新版本后重试")
	}
	if err = patch.validateFields(source); err != nil {
		return SourceConfig{}, err
	}
	patch.apply(&config)
	if err = validateSourceConfig(ctx, tx, config, hasCredentials); err != nil {
		return SourceConfig{}, err
	}
	config.Revision++
	if !config.IsEnabled || !config.IsScheduleEnabled {
		config.NextScheduledAt = nil
	} else if patch.IsEnabled != nil || patch.IsScheduleEnabled != nil || patch.IntervalMinutes != nil {
		next := catalog.Timestamp(now.Add(time.Duration(config.IntervalMinutes) * time.Minute))
		config.NextScheduledAt = &next
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return SourceConfig{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE source_configs SET config_json=?,revision=?,next_scheduled_at=?,updated_at=? WHERE source=? AND revision=?`, string(raw), config.Revision, config.NextScheduledAt, catalog.Timestamp(now), source, expected)
	if err != nil {
		return SourceConfig{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return SourceConfig{}, err
	}
	if changed != 1 {
		return SourceConfig{}, fault.New("VERSION_CONFLICT", "来源设置已被修改")
	}
	return config, nil
}

func GetSourceView(ctx context.Context, q catalog.Querier, source RemoteSource, credentials Credentials) (SourceView, error) {
	config, err := GetSourceConfig(ctx, q, source)
	if err != nil {
		return SourceView{}, err
	}
	hasCredentials := source == GitHub && credentials.GitHub || source == ProductHunt && credentials.ProductHunt
	availability := AvailabilityAvailable
	if !config.IsEnabled {
		availability = AvailabilityDisabled
	} else if source == ProductHunt && !hasCredentials {
		availability = AvailabilityMissingCredentials
	}
	view := SourceView{
		Source: source, Config: config, Revision: config.Revision,
		HasCredentials: hasCredentials, Availability: availability,
		NextScheduledAt: config.NextScheduledAt, NextRetryAt: config.NextRetryAt,
	}
	var lastRun, lastSuccess, lastObservation sql.NullString
	if err = q.QueryRowContext(ctx, `SELECT id FROM ingestion_runs WHERE source=? ORDER BY created_at DESC,id DESC LIMIT 1`, source).Scan(&lastRun); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SourceView{}, err
	}
	if err = q.QueryRowContext(ctx, `SELECT max(finished_at) FROM ingestion_runs WHERE source=? AND status IN ('SUCCEEDED','SUCCEEDED_EMPTY')`, source).Scan(&lastSuccess); err != nil {
		return SourceView{}, err
	}
	if err = q.QueryRowContext(ctx, `SELECT max(o.observed_at) FROM observations o JOIN source_records sr ON sr.id=o.source_record_id WHERE sr.source=?`, source).Scan(&lastObservation); err != nil {
		return SourceView{}, err
	}
	view.LastRunID = nullStringPointer(lastRun)
	view.LastSuccessfulAt = nullStringPointer(lastSuccess)
	view.LastObservationAt = nullStringPointer(lastObservation)
	return view, nil
}

func ListSourceViews(ctx context.Context, q catalog.Querier, filter SourceViewFilter, credentials Credentials) ([]SourceView, int, error) {
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return nil, 0, fault.Field("page", "分页参数无效")
	}
	sources := []RemoteSource{GitHub, HackerNews, ProductHunt}
	start := (filter.Page - 1) * filter.PageSize
	if start >= len(sources) {
		return []SourceView{}, len(sources), nil
	}
	end := min(start+filter.PageSize, len(sources))
	items := make([]SourceView, 0, end-start)
	for _, source := range sources[start:end] {
		view, err := GetSourceView(ctx, q, source, credentials)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, view)
	}
	return items, len(sources), nil
}

func (p SourceConfigPatch) empty() bool {
	return p.IsEnabled == nil && p.IsScheduleEnabled == nil && p.IntervalMinutes == nil && p.Keywords == nil && p.DiscoveryDays == nil && p.CandidatesPerKeyword == nil && p.TrackedSourceRecordIDs == nil && p.RequestBudgetPerHour == nil && p.Feeds == nil && p.ItemsPerFeed == nil && p.MaxPages == nil
}

func (p SourceConfigPatch) validateFields(source RemoteSource) error {
	invalid := false
	switch source {
	case GitHub:
		invalid = p.Feeds != nil || p.ItemsPerFeed != nil || p.MaxPages != nil
	case HackerNews:
		invalid = p.Keywords != nil || p.DiscoveryDays != nil || p.CandidatesPerKeyword != nil || p.RequestBudgetPerHour != nil || p.MaxPages != nil
	case ProductHunt:
		invalid = p.Keywords != nil || p.CandidatesPerKeyword != nil || p.TrackedSourceRecordIDs != nil || p.RequestBudgetPerHour != nil || p.Feeds != nil || p.ItemsPerFeed != nil
	default:
		return fault.Missing("来源")
	}
	if invalid {
		return fault.Field("body", "包含不属于此来源的设置")
	}
	return nil
}

func (p SourceConfigPatch) apply(config *SourceConfig) {
	if p.IsEnabled != nil {
		config.IsEnabled = *p.IsEnabled
	}
	if p.IsScheduleEnabled != nil {
		config.IsScheduleEnabled = *p.IsScheduleEnabled
	}
	if p.IntervalMinutes != nil {
		config.IntervalMinutes = *p.IntervalMinutes
	}
	if p.Keywords != nil {
		config.Keywords = append([]string(nil), (*p.Keywords)...)
	}
	if p.DiscoveryDays != nil {
		config.DiscoveryDays = *p.DiscoveryDays
	}
	if p.CandidatesPerKeyword != nil {
		config.CandidatesPerKeyword = *p.CandidatesPerKeyword
	}
	if p.TrackedSourceRecordIDs != nil {
		config.TrackedSourceRecordIDs = append([]string(nil), (*p.TrackedSourceRecordIDs)...)
	}
	if p.RequestBudgetPerHour != nil {
		config.RequestBudgetPerHour = *p.RequestBudgetPerHour
	}
	if p.Feeds != nil {
		config.Feeds = append([]string(nil), (*p.Feeds)...)
	}
	if p.ItemsPerFeed != nil {
		config.ItemsPerFeed = *p.ItemsPerFeed
	}
	if p.MaxPages != nil {
		config.MaxPages = *p.MaxPages
	}
}

func validateSourceConfig(ctx context.Context, q catalog.Querier, config SourceConfig, hasCredentials bool) error {
	if config.IntervalMinutes < 15 || config.IntervalMinutes > 1440 {
		return fault.Field("intervalMinutes", "间隔需要在 15–1,440 分钟之间")
	}
	switch config.Source {
	case GitHub:
		if len(config.Keywords) < 1 || len(config.Keywords) > 5 {
			return fault.Field("keywords", "关键词需要 1–5 项")
		}
		seen := map[string]bool{}
		for index, keyword := range config.Keywords {
			keyword = strings.TrimSpace(keyword)
			if utf8.RuneCountInString(keyword) < 1 || utf8.RuneCountInString(keyword) > 80 || seen[keyword] {
				return fault.Field("keywords", "关键词需唯一且每项 1–80 字")
			}
			config.Keywords[index] = keyword
			seen[keyword] = true
		}
		if config.DiscoveryDays < 1 || config.DiscoveryDays > 365 {
			return fault.Field("discoveryDays", "GitHub 发现天数需要在 1–365 之间")
		}
		if config.CandidatesPerKeyword < 2 || config.CandidatesPerKeyword > 30 {
			return fault.Field("candidatesPerKeyword", "每词候选需要在 2–30 之间")
		}
		maxBudget := 60
		if hasCredentials {
			maxBudget = 5000
		}
		if config.RequestBudgetPerHour < 1 || config.RequestBudgetPerHour > maxBudget {
			return fault.Field("requestBudgetPerHour", "请求预算超过当前凭据允许范围")
		}
		return validateTrackedSources(ctx, q, config.Source, config.TrackedSourceRecordIDs)
	case HackerNews:
		if len(config.Feeds) < 1 || len(config.Feeds) > 3 {
			return fault.Field("feeds", "HN 榜单需要 1–3 项")
		}
		seen := map[string]bool{}
		for _, feed := range config.Feeds {
			if (feed != "TOP" && feed != "NEW" && feed != "SHOW") || seen[feed] {
				return fault.Field("feeds", "HN 榜单无效或重复")
			}
			seen[feed] = true
		}
		if config.ItemsPerFeed < 1 || config.ItemsPerFeed > 50 || config.ItemsPerFeed*len(config.Feeds) > 60 {
			return fault.Field("itemsPerFeed", "HN 单榜 1–50 且合计不能超过 60 条")
		}
		return validateTrackedSources(ctx, q, config.Source, config.TrackedSourceRecordIDs)
	case ProductHunt:
		if config.DiscoveryDays < 1 || config.DiscoveryDays > 30 {
			return fault.Field("discoveryDays", "Product Hunt 发现天数需要在 1–30 之间")
		}
		if config.MaxPages < 1 || config.MaxPages > 3 {
			return fault.Field("maxPages", "Product Hunt 页数需要在 1–3 之间")
		}
	default:
		return fault.Missing("来源")
	}
	return nil
}

func validateTrackedSources(ctx context.Context, q catalog.Querier, source RemoteSource, ids []string) error {
	if len(ids) > 100 {
		return fault.Field("trackedSourceRecordIds", "最多跟踪 100 条来源记录")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || seen[id] {
			return fault.Field("trackedSourceRecordIds", "跟踪 ID 不能为空或重复")
		}
		seen[id] = true
		var storedSource string
		if err := q.QueryRowContext(ctx, `SELECT source FROM source_records WHERE id=?`, id).Scan(&storedSource); err != nil || storedSource != string(source) {
			return fault.Field("trackedSourceRecordIds", "跟踪记录不存在或不属于当前来源")
		}
	}
	return nil
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nullStringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}
