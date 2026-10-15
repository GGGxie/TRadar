package ingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"tradar/internal/catalog"
	"tradar/internal/fault"
)

const (
	githubAPIBase    = "https://api.github.com"
	githubAPIVersion = "2026-03-10"
)

// GitHub REST contracts used here:
// https://docs.github.com/en/rest/search/search#search-repositories
// https://docs.github.com/en/rest/repos/repos#get-a-repository
// https://docs.github.com/en/rest/activity/starring#get-repository-star-history
type GitHubExecutor struct {
	db     *sql.DB
	client *http.Client
	token  string
	now    func() time.Time
}

type githubSearchResponse struct {
	TotalCount        *int64             `json:"total_count"`
	IncompleteResults *bool              `json:"incomplete_results"`
	Items             []githubRepository `json:"items"`
}

type githubRepository struct {
	ID              int64   `json:"id"`
	FullName        string  `json:"full_name"`
	HTMLURL         string  `json:"html_url"`
	Description     *string `json:"description"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
	PushedAt        *string `json:"pushed_at"`
	StargazersCount *int64  `json:"stargazers_count"`
	ForksCount      *int64  `json:"forks_count"`
}

type githubStarWeek struct {
	Week  int64   `json:"week"`
	Total int64   `json:"total"`
	Days  []int64 `json:"days"`
}

func NewGitHubExecutor(db *sql.DB, client *http.Client, token string, now func() time.Time) *GitHubExecutor {
	if client == nil {
		client = newSourceHTTPClient(GitHub)
	}
	if now == nil {
		now = time.Now
	}
	return &GitHubExecutor{db: db, client: client, token: token, now: now}
}

func (e *GitHubExecutor) Execute(ctx context.Context, run IngestionRun, repository *Repository) (ExecutionResult, error) {
	result := ExecutionResult{Errors: []RunError{}}
	if run.Source != GitHub || run.Config.Source != GitHub {
		return result, fault.Field("source", "GitHub 执行器收到其他来源任务")
	}
	controller := requestController{
		db: e.db, runID: run.ID, client: e.client, source: GitHub,
		budget: &BudgetPolicy{HourlyLimit: run.Config.RequestBudgetPerHour, SearchMinuteLimit: 10},
		now:    e.now, deadline: e.now().Add(5 * time.Minute),
	}
	tracked, err := catalog.ListSourcesByObservationAge(ctx, e.db, string(GitHub), run.Config.TrackedSourceRecordIDs, "CUMULATIVE", 20)
	if err != nil {
		return result, err
	}
	historyTracked, err := catalog.ListSourcesByObservationAge(ctx, e.db, string(GitHub), run.Config.TrackedSourceRecordIDs, "SOURCE_DAILY", 5)
	if err != nil {
		return result, err
	}
	windowEnd, err := time.Parse(time.RFC3339Nano, run.WindowEnd)
	if err != nil {
		return result, err
	}
	discoveryStart := windowEnd.AddDate(0, 0, -run.Config.DiscoveryDays).Format("2006-01-02")
	recentStart := windowEnd.AddDate(0, 0, -7).Format("2006-01-02")
	newCount := (run.Config.CandidatesPerKeyword + 1) / 2
	updatedCount := run.Config.CandidatesPerKeyword - newCount
	seen := make(map[string]bool)

	for keywordIndex, keyword := range run.Config.Keywords {
		queries := []struct {
			name, qualifier, sort string
			count                 int
		}{
			{"created", "created:>=" + discoveryStart, "stars", newCount},
			{"updated", "pushed:>=" + recentStart, "updated", updatedCount},
		}
		for _, query := range queries {
			values := url.Values{}
			values.Set("q", literalGitHubKeyword(keyword)+" "+query.qualifier)
			values.Set("sort", query.sort)
			values.Set("order", "desc")
			values.Set("per_page", strconv.Itoa(query.count))
			request, requestErr := e.request(ctx, "/search/repositories", values)
			if requestErr != nil {
				return result, requestErr
			}
			var response githubSearchResponse
			report, requestErr := controller.JSON(ctx, request, RequestSearch, &response)
			applyRequestReport(&result, report)
			if requestErr != nil {
				if !isRemoteFailure(requestErr) {
					return result, requestErr
				}
				code := remoteErrorCode(requestErr)
				appendRunError(&result, code, "GitHub 候选查询失败", nil, true)
				if code == "AUTH_FAILED" || code == "RATE_LIMITED" || code == "TASK_DEADLINE_EXCEEDED" {
					return result, nil
				}
				continue
			}
			if response.TotalCount == nil || response.IncompleteResults == nil || response.Items == nil || *response.TotalCount < 0 || *response.TotalCount > maxSafeInteger || len(response.Items) > query.count {
				appendRunError(&result, "MALFORMED_RESPONSE", "GitHub 候选响应形状无效", nil, true)
				continue
			}
			valid := make([]githubRepository, 0, len(response.Items))
			skipped, failed := 0, 0
			ids := make([]string, 0, len(response.Items))
			for _, item := range response.Items {
				externalID, validationErr := validateGitHubRepository(item)
				if validationErr != nil {
					failed++
					appendRunError(&result, "MALFORMED_ITEM", "GitHub 仓库字段无效", nil, true)
					continue
				}
				ids = append(ids, externalID)
				if seen[externalID] {
					skipped++
					continue
				}
				seen[externalID] = true
				valid = append(valid, item)
			}
			if *response.IncompleteResults {
				result.IsTruncated = true
				appendRunError(&result, "INCOMPLETE_RESULTS", "GitHub 搜索结果不完整", nil, true)
			}
			checkpoint, _ := json.Marshal(map[string]any{"query": values.Get("q"), "itemIds": ids})
			batch := Batch{
				Key: fmt.Sprintf("search:%d:%s", keywordIndex, query.name), Checkpoint: checkpoint,
				Stats: BatchStats{FetchedItems: len(response.Items), AcceptedItems: len(valid), SkippedItems: skipped, FailedItems: failed, ObservationsWritten: len(valid) * 2},
			}
			if _, requestErr = repository.CommitBatch(ctx, run.ID, batch, func(tx *sql.Tx) error {
				for _, item := range valid {
					if _, writeErr := writeGitHubRepository(ctx, tx, run.ID, item, e.now()); writeErr != nil {
						return writeErr
					}
				}
				return nil
			}); requestErr != nil {
				return result, requestErr
			}
		}
	}

	for _, source := range tracked {
		if seen[source.ExternalID] {
			continue
		}
		owner, name, parseErr := githubOwnerRepo(source.TargetURL)
		if parseErr != nil {
			itemID := source.ID
			appendRunError(&result, "TRACKED_SOURCE_INVALID", "GitHub 跟踪记录缺少有效仓库路径", &itemID, true)
			continue
		}
		request, requestErr := e.request(ctx, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name), nil)
		if requestErr != nil {
			return result, requestErr
		}
		var item githubRepository
		report, requestErr := controller.JSON(ctx, request, RequestGeneral, &item)
		applyRequestReport(&result, report)
		if requestErr != nil {
			if !isRemoteFailure(requestErr) {
				return result, requestErr
			}
			itemID := source.ID
			code := remoteErrorCode(requestErr)
			appendRunError(&result, code, "GitHub 跟踪仓库读取失败", &itemID, true)
			if code == "AUTH_FAILED" || code == "RATE_LIMITED" || code == "TASK_DEADLINE_EXCEEDED" {
				return result, nil
			}
			continue
		}
		externalID, validationErr := validateGitHubRepository(item)
		if validationErr != nil || externalID != source.ExternalID {
			itemID := source.ID
			appendRunError(&result, "MALFORMED_ITEM", "GitHub 跟踪仓库身份无效", &itemID, true)
			continue
		}
		seen[externalID] = true
		checkpoint, _ := json.Marshal(map[string]any{"sourceRecordId": source.ID, "repositoryId": externalID})
		batch := Batch{Key: "tracked:" + source.ID, Checkpoint: checkpoint, Stats: BatchStats{FetchedItems: 1, AcceptedItems: 1, ObservationsWritten: 2}}
		if _, requestErr = repository.CommitBatch(ctx, run.ID, batch, func(tx *sql.Tx) error {
			_, writeErr := writeGitHubRepository(ctx, tx, run.ID, item, e.now())
			return writeErr
		}); requestErr != nil {
			return result, requestErr
		}
	}

	for _, trackedSource := range historyTracked {
		source, getErr := catalog.GetSource(ctx, e.db, trackedSource.ID)
		if getErr != nil {
			return result, getErr
		}
		owner, name, parseErr := githubOwnerRepo(source.TargetURL)
		if parseErr != nil {
			itemID := source.ID
			appendRunError(&result, "HISTORY_UNAVAILABLE", "GitHub Star 历史缺少有效仓库路径", &itemID, true)
			continue
		}
		values := url.Values{"per_page": []string{"1"}, "page": []string{"1"}}
		request, requestErr := e.request(ctx, "/repos/"+url.PathEscape(owner)+"/"+url.PathEscape(name)+"/stargazers/history", values)
		if requestErr != nil {
			return result, requestErr
		}
		var weeks []githubStarWeek
		report, requestErr := controller.JSON(ctx, request, RequestGeneral, &weeks)
		applyRequestReport(&result, report)
		if requestErr != nil {
			if !isRemoteFailure(requestErr) {
				return result, requestErr
			}
			itemID := source.ID
			code := remoteErrorCode(requestErr)
			if code == "AUTH_FAILED" || code == "RATE_LIMITED" || code == "TASK_DEADLINE_EXCEEDED" {
				appendRunError(&result, code, "GitHub 历史请求停止", &itemID, true)
				return result, nil
			}
			appendRunError(&result, "HISTORY_UNAVAILABLE", "GitHub Star 历史暂不可用", &itemID, true)
			continue
		}
		observations, validationErr := prepareGitHubHistory(weeks, e.now())
		if validationErr != nil {
			itemID := source.ID
			appendRunError(&result, "MALFORMED_HISTORY", "GitHub Star 历史字段无效", &itemID, true)
			continue
		}
		checkpoint, _ := json.Marshal(map[string]any{"sourceRecordId": source.ID, "weeks": len(weeks)})
		batch := Batch{Key: "history:" + source.ID, Checkpoint: checkpoint, Stats: BatchStats{FetchedItems: len(weeks), ObservationsWritten: len(observations)}}
		if _, requestErr = repository.CommitBatch(ctx, run.ID, batch, func(tx *sql.Tx) error {
			_, writeErr := catalog.AppendObservations(ctx, tx, run.ID, source.ID, observations)
			return writeErr
		}); requestErr != nil {
			return result, requestErr
		}
	}
	return result, nil
}

func (e *GitHubExecutor) request(ctx context.Context, path string, values url.Values) (*http.Request, error) {
	target := githubAPIBase + path
	if len(values) > 0 {
		target += "?" + values.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	request.Header.Set("User-Agent", "TRadar/0.1")
	if e.token != "" {
		request.Header.Set("Authorization", "Bearer "+e.token)
	}
	return request, nil
}

func validateGitHubRepository(item githubRepository) (string, error) {
	if item.ID <= 0 || item.ID > maxSafeInteger || item.StargazersCount == nil || item.ForksCount == nil || *item.StargazersCount < 0 || *item.ForksCount < 0 || *item.StargazersCount > maxSafeInteger || *item.ForksCount > maxSafeInteger {
		return "", fault.Field("repository", "仓库身份或指标无效")
	}
	owner, name, err := splitGitHubFullName(item.FullName)
	if err != nil {
		return "", err
	}
	wantHTML := "https://github.com/" + owner + "/" + name
	if item.HTMLURL != wantHTML {
		return "", fault.Field("htmlUrl", "仓库主页与名称不匹配")
	}
	if _, err = time.Parse(time.RFC3339, item.CreatedAt); err != nil {
		return "", fault.Field("createdAt", "仓库创建时间无效")
	}
	if _, err = time.Parse(time.RFC3339, item.UpdatedAt); err != nil {
		return "", fault.Field("updatedAt", "仓库更新时间无效")
	}
	if item.PushedAt != nil {
		if _, err = time.Parse(time.RFC3339, *item.PushedAt); err != nil {
			return "", fault.Field("pushedAt", "仓库推送时间无效")
		}
	}
	if item.Description != nil && utf8.RuneCountInString(*item.Description) > 4000 {
		return "", fault.Field("description", "仓库描述过长")
	}
	return strconv.FormatInt(item.ID, 10), nil
}

func literalGitHubKeyword(keyword string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(keyword)
	return `"` + escaped + `"`
}

func splitGitHubFullName(fullName string) (string, string, error) {
	parts := strings.Split(fullName, "/")
	if len(parts) != 2 || !validGitHubPathPart(parts[0]) || !validGitHubPathPart(parts[1]) {
		return "", "", fault.Field("fullName", "GitHub 仓库全名无效")
	}
	return parts[0], parts[1], nil
}

func validGitHubPathPart(value string) bool {
	if value == "" || len(value) > 100 || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '-' && character != '_' && character != '.' {
			return false
		}
	}
	return true
}

func githubOwnerRepo(target *string) (string, string, error) {
	if target == nil {
		return "", "", fault.Field("targetUrl", "缺少 GitHub 仓库主页")
	}
	parsed, err := url.Parse(*target)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") || parsed.Port() != "" {
		return "", "", fault.Field("targetUrl", "GitHub 仓库主页无效")
	}
	return splitGitHubFullName(strings.Trim(parsed.EscapedPath(), "/"))
}

func writeGitHubRepository(ctx context.Context, tx *sql.Tx, runID string, item githubRepository, observedAt time.Time) (catalog.SourceRecord, error) {
	externalID := strconv.FormatInt(item.ID, 10)
	existing, found, err := catalog.FindSourceByExternalID(ctx, tx, string(GitHub), externalID)
	if err != nil {
		return catalog.SourceRecord{}, err
	}
	projectID := existing.ProjectID
	if !found {
		description := ""
		if item.Description != nil {
			description = *item.Description
		}
		project, _, createErr := catalog.CreateProject(ctx, tx, catalog.CreateProjectInput{
			Name: item.FullName, CanonicalURL: item.HTMLURL, Summary: description, Category: "OTHER",
		}, observedAt)
		if createErr != nil {
			return catalog.SourceRecord{}, createErr
		}
		projectID = project.ID
	}
	published := item.CreatedAt
	description := ""
	if item.Description != nil {
		description = *item.Description
	}
	sourceURL := githubAPIBase + "/repositories/" + externalID
	targetURL := item.HTMLURL
	source, _, err := catalog.UpsertSource(ctx, tx, catalog.UpsertSourceInput{
		ProjectID: projectID, Source: string(GitHub), ExternalID: externalID, RecordType: "PROJECT",
		URL: sourceURL, TargetURL: &targetURL, Title: item.FullName, Excerpt: description,
		PublishedAt: &published, FetchedAt: observedAt,
	})
	if err != nil {
		return catalog.SourceRecord{}, err
	}
	_, err = catalog.AppendObservations(ctx, tx, runID, source.ID, []catalog.ObservationInput{
		{Metric: "STARS", Basis: "CUMULATIVE", Value: *item.StargazersCount, ObservedAt: observedAt, FetchedAt: observedAt},
		{Metric: "FORKS", Basis: "CUMULATIVE", Value: *item.ForksCount, ObservedAt: observedAt, FetchedAt: observedAt},
	})
	return source, err
}

func prepareGitHubHistory(weeks []githubStarWeek, observedAt time.Time) ([]catalog.ObservationInput, error) {
	if len(weeks) > 1 {
		return nil, fault.Field("history", "请求单周却返回多周")
	}
	if len(weeks) == 0 {
		return []catalog.ObservationInput{}, nil
	}
	week := weeks[0]
	if week.Week < 0 || week.Week > maxUnixTimestamp || week.Total < 0 || week.Total > maxSafeInteger || len(week.Days) != 7 {
		return nil, fault.Field("history", "Star 历史周字段无效")
	}
	items := make([]catalog.ObservationInput, 0, 7)
	var sum int64
	for dayIndex, value := range week.Days {
		if value < 0 || value > maxSafeInteger {
			return nil, fault.Field("history", "Star 历史日值无效")
		}
		sum += value
		items = append(items, catalog.ObservationInput{
			Metric: "STARS", Basis: "SOURCE_DAILY", PeriodKey: fmt.Sprintf("%d:%d", week.Week, dayIndex),
			Value: value, ObservedAt: observedAt, FetchedAt: observedAt,
			ProviderPeriod: &catalog.ProviderPeriod{WeekUnix: week.Week, DayIndex: dayIndex, TimeZone: "UNSPECIFIED"},
		})
	}
	if sum != week.Total {
		return nil, fault.Field("history", "Star 历史周总数与日值不一致")
	}
	return items, nil
}
