package ingestion

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"tradar/internal/catalog"
	"tradar/internal/fault"
)

const productHuntAPIURL = "https://api.producthunt.com/v2/api/graphql"

// Product Hunt API V2 contracts used here:
// https://api.producthunt.com/v2/docs
// https://github.com/producthunt/producthunt-api/blob/master/schema.graphql
const productHuntPostsQuery = `query ProductHuntPosts($first: Int!, $after: String, $postedAfter: DateTime!, $postedBefore: DateTime!) {
  posts(first: $first, after: $after, order: NEWEST, postedAfter: $postedAfter, postedBefore: $postedBefore) {
    totalCount
    pageInfo { hasNextPage endCursor }
    edges {
      cursor
      node { id name tagline description url website createdAt featuredAt votesCount commentsCount }
    }
  }
}`

type ProductHuntExecutor struct {
	db     *sql.DB
	client *http.Client
	token  string
	now    func() time.Time
}

type productHuntResponse struct {
	Data   *productHuntData  `json:"data"`
	Errors []json.RawMessage `json:"errors"`
}

type productHuntData struct {
	Posts *productHuntConnection `json:"posts"`
}

type productHuntConnection struct {
	TotalCount *int64            `json:"totalCount"`
	PageInfo   *productHuntPage  `json:"pageInfo"`
	Edges      []productHuntEdge `json:"edges"`
}

type productHuntPage struct {
	HasNextPage *bool   `json:"hasNextPage"`
	EndCursor   *string `json:"endCursor"`
}

type productHuntEdge struct {
	Cursor string           `json:"cursor"`
	Node   *productHuntPost `json:"node"`
}

type productHuntPost struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Tagline       string  `json:"tagline"`
	Description   *string `json:"description"`
	URL           string  `json:"url"`
	Website       string  `json:"website"`
	CreatedAt     string  `json:"createdAt"`
	FeaturedAt    *string `json:"featuredAt"`
	VotesCount    *int64  `json:"votesCount"`
	CommentsCount *int64  `json:"commentsCount"`
}

func NewProductHuntExecutor(db *sql.DB, client *http.Client, token string, now func() time.Time) *ProductHuntExecutor {
	if client == nil {
		client = newSourceHTTPClient(ProductHunt)
	}
	if now == nil {
		now = time.Now
	}
	return &ProductHuntExecutor{db: db, client: client, token: token, now: now}
}

func (e *ProductHuntExecutor) Execute(ctx context.Context, run IngestionRun, repository *Repository) (ExecutionResult, error) {
	result := ExecutionResult{Errors: []RunError{}}
	if run.Source != ProductHunt || run.Config.Source != ProductHunt {
		return result, fault.Field("source", "Product Hunt 执行器收到其他来源任务")
	}
	if strings.TrimSpace(e.token) == "" {
		return result, fault.New("SOURCE_CREDENTIALS_REQUIRED", "Product Hunt 来源需要配置凭据")
	}
	controller := requestController{db: e.db, runID: run.ID, client: e.client, source: ProductHunt, now: e.now, deadline: e.now().Add(5 * time.Minute)}
	seen := make(map[string]bool)
	var after *string
	for page := 1; page <= run.Config.MaxPages; page++ {
		request, err := e.request(ctx, run, after)
		if err != nil {
			return result, err
		}
		var response productHuntResponse
		report, requestErr := controller.JSON(ctx, request, RequestGeneral, &response)
		applyRequestReport(&result, report)
		if requestErr != nil {
			if !isRemoteFailure(requestErr) {
				return result, requestErr
			}
			code := remoteErrorCode(requestErr)
			appendRunError(&result, code, "Product Hunt 请求失败", nil, true)
			return result, nil
		}
		if response.Data == nil || response.Data.Posts == nil {
			code := "MALFORMED_RESPONSE"
			if len(response.Errors) > 0 {
				code = "GRAPHQL_ERROR"
			}
			appendRunError(&result, code, "Product Hunt 未返回可用数据", nil, true)
			return result, nil
		}
		if len(response.Errors) > 0 {
			appendRunError(&result, "GRAPHQL_PARTIAL", "Product Hunt 返回部分数据", nil, true)
		}
		posts := response.Data.Posts
		if err = validateProductHuntConnection(posts); err != nil {
			appendRunError(&result, "MALFORMED_RESPONSE", "Product Hunt 分页响应无效", nil, true)
			return result, nil
		}
		valid := make([]productHuntPost, 0, len(posts.Edges))
		ids := make([]string, 0, len(posts.Edges))
		skipped, failed := 0, 0
		for _, edge := range posts.Edges {
			if edge.Node == nil {
				failed++
				appendRunError(&result, "MALFORMED_ITEM", "Product Hunt 项目字段无效", nil, true)
				continue
			}
			post := *edge.Node
			if err = validateProductHuntPost(&post); err != nil {
				failed++
				itemID := strings.TrimSpace(post.ID)
				appendRunError(&result, "MALFORMED_ITEM", "Product Hunt 项目字段无效", optionalItemID(itemID), true)
				continue
			}
			ids = append(ids, post.ID)
			if seen[post.ID] {
				skipped++
				continue
			}
			seen[post.ID] = true
			valid = append(valid, post)
		}
		checkpoint, _ := json.Marshal(map[string]any{"page": page, "endCursor": posts.PageInfo.EndCursor, "itemIds": ids})
		batch := Batch{
			Key: fmt.Sprintf("posts:%d", page), Checkpoint: checkpoint,
			Stats: BatchStats{FetchedItems: len(posts.Edges), AcceptedItems: len(valid), SkippedItems: skipped, FailedItems: failed, ObservationsWritten: len(valid) * 2},
		}
		if _, err = repository.CommitBatch(ctx, run.ID, batch, func(tx *sql.Tx) error {
			for index := range valid {
				if writeErr := writeProductHuntPost(ctx, tx, run.ID, &valid[index], e.now()); writeErr != nil {
					return writeErr
				}
			}
			return nil
		}); err != nil {
			return result, err
		}
		if !*posts.PageInfo.HasNextPage {
			break
		}
		if page == run.Config.MaxPages {
			result.IsTruncated = true
			break
		}
		next := strings.TrimSpace(*posts.PageInfo.EndCursor)
		after = &next
	}
	return result, nil
}

func (e *ProductHuntExecutor) request(ctx context.Context, run IngestionRun, after *string) (*http.Request, error) {
	body, err := json.Marshal(map[string]any{
		"query": productHuntPostsQuery,
		"variables": map[string]any{
			"first": 20, "after": after,
			"postedAfter": run.WindowStart, "postedBefore": run.WindowEnd,
		},
	})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, productHuntAPIURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+e.token)
	request.Header.Set("User-Agent", "TRadar/0.1")
	return request, nil
}

func validateProductHuntConnection(posts *productHuntConnection) error {
	if posts.TotalCount == nil || *posts.TotalCount < 0 || *posts.TotalCount > maxSafeInteger || posts.PageInfo == nil || posts.PageInfo.HasNextPage == nil || posts.Edges == nil || len(posts.Edges) > 20 {
		return fault.Field("posts", "分页字段无效")
	}
	if *posts.PageInfo.HasNextPage && (posts.PageInfo.EndCursor == nil || strings.TrimSpace(*posts.PageInfo.EndCursor) == "" || utf8.RuneCountInString(*posts.PageInfo.EndCursor) > 1000) {
		return fault.Field("cursor", "分页游标无效")
	}
	for _, edge := range posts.Edges {
		if strings.TrimSpace(edge.Cursor) == "" || utf8.RuneCountInString(edge.Cursor) > 1000 {
			return fault.Field("edge", "分页边字段无效")
		}
	}
	return nil
}

func validateProductHuntPost(post *productHuntPost) error {
	post.ID = strings.TrimSpace(post.ID)
	post.Name = strings.TrimSpace(post.Name)
	post.Tagline = strings.TrimSpace(post.Tagline)
	if utf8.RuneCountInString(post.ID) < 1 || utf8.RuneCountInString(post.ID) > 200 || strings.IndexFunc(post.ID, unicode.IsControl) >= 0 || utf8.RuneCountInString(post.Name) < 1 || utf8.RuneCountInString(post.Name) > 300 || utf8.RuneCountInString(post.Tagline) > 1000 {
		return fault.Field("post", "项目身份或文本无效")
	}
	if post.Description != nil && utf8.RuneCountInString(*post.Description) > 4000 {
		return fault.Field("description", "项目描述过长")
	}
	if post.VotesCount == nil || post.CommentsCount == nil || *post.VotesCount < 0 || *post.CommentsCount < 0 || *post.VotesCount > maxSafeInteger || *post.CommentsCount > maxSafeInteger {
		return fault.Field("metrics", "项目指标无效")
	}
	created, err := time.Parse(time.RFC3339, post.CreatedAt)
	if err != nil || created.Unix() < 0 {
		return fault.Field("createdAt", "项目创建时间无效")
	}
	if post.FeaturedAt != nil {
		if _, err = time.Parse(time.RFC3339, *post.FeaturedAt); err != nil {
			return fault.Field("featuredAt", "项目发布时间无效")
		}
	}
	post.URL, err = catalog.NormalizeURL(post.URL)
	if err != nil {
		return fault.Field("url", "Product Hunt URL 无效")
	}
	parsed, err := url.Parse(post.URL)
	if err != nil || parsed.Scheme != "https" || parsed.Port() != "" || (parsed.Hostname() != "www.producthunt.com" && parsed.Hostname() != "producthunt.com") || !strings.HasPrefix(parsed.EscapedPath(), "/posts/") {
		return fault.Field("url", "Product Hunt URL 不属于项目页")
	}
	post.Website, err = catalog.NormalizeURL(post.Website)
	if err != nil {
		return fault.Field("website", "项目网站无效")
	}
	return nil
}

func writeProductHuntPost(ctx context.Context, tx *sql.Tx, runID string, post *productHuntPost, observedAt time.Time) error {
	existing, found, err := catalog.FindSourceByExternalID(ctx, tx, string(ProductHunt), post.ID)
	if err != nil {
		return err
	}
	projectID := existing.ProjectID
	description := post.Tagline
	if post.Description != nil && strings.TrimSpace(*post.Description) != "" {
		description = strings.TrimSpace(*post.Description)
	}
	if !found {
		project, _, createErr := catalog.CreateProject(ctx, tx, catalog.CreateProjectInput{
			Name: post.Name, CanonicalURL: post.Website, Summary: description, Category: "OTHER",
		}, observedAt)
		if createErr != nil {
			return createErr
		}
		projectID = project.ID
	}
	publishedAt := post.CreatedAt
	if post.FeaturedAt != nil {
		publishedAt = *post.FeaturedAt
	}
	target := post.Website
	source, _, err := catalog.UpsertSource(ctx, tx, catalog.UpsertSourceInput{
		ProjectID: projectID, Source: string(ProductHunt), ExternalID: post.ID, RecordType: "PROJECT",
		URL: post.URL, TargetURL: &target, Title: post.Name, Excerpt: description,
		PublishedAt: &publishedAt, FetchedAt: observedAt,
	})
	if err != nil {
		return err
	}
	_, err = catalog.AppendObservations(ctx, tx, runID, source.ID, []catalog.ObservationInput{
		{Metric: "VOTES", Basis: "CUMULATIVE", Value: *post.VotesCount, ObservedAt: observedAt, FetchedAt: observedAt},
		{Metric: "COMMENTS", Basis: "CUMULATIVE", Value: *post.CommentsCount, ObservedAt: observedAt, FetchedAt: observedAt},
	})
	return err
}

func optionalItemID(value string) *string {
	if value == "" || utf8.RuneCountInString(value) > 200 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return nil
	}
	return &value
}
