package ingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"tradar/internal/catalog"
	"tradar/internal/fault"
)

const hackerNewsAPIBase = "https://hacker-news.firebaseio.com/v0"

// Hacker News v0 explicitly permits additional response fields and documents
// the item/feed shapes used here: https://github.com/HackerNews/API
type HackerNewsExecutor struct {
	db     *sql.DB
	client *http.Client
	now    func() time.Time
}

type hackerNewsItem struct {
	ID          int64   `json:"id"`
	Type        string  `json:"type"`
	Title       string  `json:"title"`
	URL         *string `json:"url"`
	Score       *int64  `json:"score"`
	Descendants *int64  `json:"descendants"`
	Time        *int64  `json:"time"`
	Deleted     bool    `json:"deleted"`
	Dead        bool    `json:"dead"`
}

func NewHackerNewsExecutor(db *sql.DB, client *http.Client, now func() time.Time) *HackerNewsExecutor {
	if client == nil {
		client = newSourceHTTPClient(HackerNews)
	}
	if now == nil {
		now = time.Now
	}
	return &HackerNewsExecutor{db: db, client: client, now: now}
}

func (e *HackerNewsExecutor) Execute(ctx context.Context, run IngestionRun, repository *Repository) (ExecutionResult, error) {
	result := ExecutionResult{Errors: []RunError{}}
	if run.Source != HackerNews || run.Config.Source != HackerNews {
		return result, fault.Field("source", "Hacker News 执行器收到其他来源任务")
	}
	controller := requestController{client: e.client, source: HackerNews, now: e.now, deadline: e.now().Add(5 * time.Minute)}
	orderedIDs := make([]int64, 0, 60)
	seen := make(map[int64]bool)
	addID := func(id int64) {
		if len(orderedIDs) < 60 && !seen[id] {
			seen[id] = true
			orderedIDs = append(orderedIDs, id)
		}
	}
	for _, sourceID := range run.Config.TrackedSourceRecordIDs {
		source, err := catalog.GetSource(ctx, e.db, sourceID)
		if err != nil {
			return result, err
		}
		itemID, err := strconv.ParseInt(source.ExternalID, 10, 64)
		if err != nil || itemID <= 0 || itemID > maxSafeInteger {
			id := source.ID
			appendRunError(&result, "TRACKED_SOURCE_INVALID", "HN 跟踪记录的 item ID 无效", &id, true)
			continue
		}
		addID(itemID)
	}

	feedPaths := map[string]string{"TOP": "topstories", "NEW": "newstories", "SHOW": "showstories"}
	for _, feed := range run.Config.Feeds {
		request, err := e.request(ctx, "/"+feedPaths[feed]+".json")
		if err != nil {
			return result, err
		}
		var ids []int64
		report, requestErr := controller.JSON(ctx, request, RequestGeneral, &ids)
		applyRequestReport(&result, report)
		if requestErr != nil {
			code := remoteErrorCode(requestErr)
			appendRunError(&result, code, "HN 榜单读取失败", nil, true)
			if code == "RATE_LIMITED" || code == "TASK_DEADLINE_EXCEEDED" {
				return result, nil
			}
			continue
		}
		if ids == nil {
			appendRunError(&result, "MALFORMED_RESPONSE", "HN 榜单响应不是 ID 数组", nil, true)
			continue
		}
		limit := min(run.Config.ItemsPerFeed, len(ids))
		for _, id := range ids[:limit] {
			if id <= 0 || id > maxSafeInteger {
				appendRunError(&result, "MALFORMED_ITEM_ID", "HN 榜单包含无效 item ID", nil, true)
				continue
			}
			addID(id)
		}
	}

	for _, id := range orderedIDs {
		idString := strconv.FormatInt(id, 10)
		request, err := e.request(ctx, "/item/"+idString+".json")
		if err != nil {
			return result, err
		}
		var item *hackerNewsItem
		report, requestErr := controller.JSON(ctx, request, RequestGeneral, &item)
		applyRequestReport(&result, report)
		if requestErr != nil {
			code := remoteErrorCode(requestErr)
			appendRunError(&result, code, "HN item 读取失败", &idString, true)
			if code == "RATE_LIMITED" || code == "TASK_DEADLINE_EXCEEDED" {
				return result, nil
			}
			continue
		}
		if item == nil {
			appendRunError(&result, "ITEM_NULL", "HN item 已不可用", &idString, false)
			if err = commitHNSkip(ctx, repository, run.ID, idString, "NULL"); err != nil {
				return result, err
			}
			continue
		}
		if item.ID != id || item.ID <= 0 {
			appendRunError(&result, "MALFORMED_ITEM", "HN item 身份无效", &idString, true)
			if err = commitHNFailure(ctx, repository, run.ID, idString, "IDENTITY"); err != nil {
				return result, err
			}
			continue
		}
		if item.Deleted {
			appendRunError(&result, "ITEM_DELETED", "HN item 已删除", &idString, false)
			if err = commitHNSkip(ctx, repository, run.ID, idString, "DELETED"); err != nil {
				return result, err
			}
			continue
		}
		if item.Dead {
			appendRunError(&result, "ITEM_DEAD", "HN item 已标记为 dead", &idString, false)
			if err = commitHNSkip(ctx, repository, run.ID, idString, "DEAD"); err != nil {
				return result, err
			}
			continue
		}
		if item.Type != "story" {
			appendRunError(&result, "ITEM_NOT_STORY", "HN item 不是 story", &idString, false)
			if err = commitHNSkip(ctx, repository, run.ID, idString, "NOT_STORY"); err != nil {
				return result, err
			}
			continue
		}
		targetURL, warningCode, validationErr := validateHackerNewsStory(item)
		if validationErr != nil {
			appendRunError(&result, "MALFORMED_ITEM", "HN story 字段无效", &idString, true)
			if err = commitHNFailure(ctx, repository, run.ID, idString, "FIELDS"); err != nil {
				return result, err
			}
			continue
		}
		if warningCode != "" {
			appendRunError(&result, warningCode, "HN 外链无效，已保留讨论链接", &idString, false)
		}
		observations := hnObservationCount(item)
		checkpoint, _ := json.Marshal(map[string]any{"itemId": idString, "state": "IMPORTED"})
		batch := Batch{Key: "item:" + idString, Checkpoint: checkpoint, Stats: BatchStats{FetchedItems: 1, AcceptedItems: 1, ObservationsWritten: observations}}
		if _, err = repository.CommitBatch(ctx, run.ID, batch, func(tx *sql.Tx) error {
			return writeHackerNewsStory(ctx, tx, run.ID, item, targetURL, e.now())
		}); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (e *HackerNewsExecutor) request(ctx context.Context, path string) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, hackerNewsAPIBase+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "TRadar/0.1")
	return request, nil
}

func validateHackerNewsStory(item *hackerNewsItem) (*string, string, error) {
	item.Title = strings.TrimSpace(item.Title)
	if utf8.RuneCountInString(item.Title) < 1 || utf8.RuneCountInString(item.Title) > 500 {
		return nil, "", fault.Field("title", "HN story 标题无效")
	}
	for _, metric := range []*int64{item.Score, item.Descendants} {
		if metric != nil && (*metric < 0 || *metric > maxSafeInteger) {
			return nil, "", fault.Field("metric", "HN story 指标无效")
		}
	}
	if item.Time != nil && (*item.Time <= 0 || *item.Time > maxUnixTimestamp) {
		return nil, "", fault.Field("time", "HN story 时间无效")
	}
	if item.URL == nil || strings.TrimSpace(*item.URL) == "" {
		return nil, "", nil
	}
	normalized, err := catalog.NormalizeURL(*item.URL)
	if err != nil {
		return nil, "ITEM_URL_INVALID", nil
	}
	return &normalized, "", nil
}

func writeHackerNewsStory(ctx context.Context, tx *sql.Tx, runID string, item *hackerNewsItem, targetURL *string, observedAt time.Time) error {
	externalID := strconv.FormatInt(item.ID, 10)
	discussionURL := "https://news.ycombinator.com/item?id=" + externalID
	existing, found, err := catalog.FindSourceByExternalID(ctx, tx, string(HackerNews), externalID)
	if err != nil {
		return err
	}
	projectID := existing.ProjectID
	if !found {
		canonicalURL := discussionURL
		if targetURL != nil {
			canonicalURL = *targetURL
		}
		project, _, createErr := catalog.CreateProject(ctx, tx, catalog.CreateProjectInput{
			Name: item.Title, CanonicalURL: canonicalURL, Category: "OTHER",
		}, observedAt)
		if createErr != nil {
			return createErr
		}
		projectID = project.ID
	}
	var publishedAt *string
	if item.Time != nil {
		value := catalog.Timestamp(time.Unix(*item.Time, 0))
		publishedAt = &value
	}
	source, _, err := catalog.UpsertSource(ctx, tx, catalog.UpsertSourceInput{
		ProjectID: projectID, Source: string(HackerNews), ExternalID: externalID,
		RecordType: "DISCUSSION", URL: discussionURL, TargetURL: targetURL,
		Title: item.Title, PublishedAt: publishedAt, FetchedAt: observedAt,
	})
	if err != nil {
		return err
	}
	observations := make([]catalog.ObservationInput, 0, 2)
	if item.Score != nil {
		observations = append(observations, catalog.ObservationInput{Metric: "POINTS", Basis: "CUMULATIVE", Value: *item.Score, ObservedAt: observedAt, FetchedAt: observedAt})
	}
	if item.Descendants != nil {
		observations = append(observations, catalog.ObservationInput{Metric: "COMMENTS", Basis: "CUMULATIVE", Value: *item.Descendants, ObservedAt: observedAt, FetchedAt: observedAt})
	}
	_, err = catalog.AppendObservations(ctx, tx, runID, source.ID, observations)
	return err
}

func hnObservationCount(item *hackerNewsItem) int {
	count := 0
	if item.Score != nil {
		count++
	}
	if item.Descendants != nil {
		count++
	}
	return count
}

func commitHNSkip(ctx context.Context, repository *Repository, runID, itemID, reason string) error {
	checkpoint, _ := json.Marshal(map[string]string{"itemId": itemID, "state": reason})
	_, err := repository.CommitBatch(ctx, runID, Batch{Key: "item:" + itemID, Checkpoint: checkpoint, Stats: BatchStats{FetchedItems: 1, SkippedItems: 1}}, nil)
	return err
}

func commitHNFailure(ctx context.Context, repository *Repository, runID, itemID, reason string) error {
	checkpoint, _ := json.Marshal(map[string]string{"itemId": itemID, "state": reason})
	_, err := repository.CommitBatch(ctx, runID, Batch{Key: "item:" + itemID, Checkpoint: checkpoint, Stats: BatchStats{FetchedItems: 1, FailedItems: 1}}, nil)
	return err
}
