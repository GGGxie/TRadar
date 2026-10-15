package trends

import (
	"context"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/fault"
)

type RadarFilter struct {
	Query, Category, Source string
	Window                  Window
	AsOf                    time.Time
	Page, PageSize          int
}

func GetProjectTrend(ctx context.Context, q catalog.Querier, projectID string, asOf time.Time, window Window) (TrendResult, error) {
	project, err := catalog.GetProject(ctx, q, projectID)
	if err != nil {
		return TrendResult{}, err
	}
	return loadProjectTrend(ctx, q, project, asOf, window)
}

func ListRadar(ctx context.Context, q catalog.Querier, filter RadarFilter) ([]ProjectTrend, int, error) {
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 {
		return nil, 0, fault.Field("page", "分页参数无效")
	}
	if filter.Source != "" && primaryMetric(filter.Source) == "" {
		return nil, 0, fault.Field("source", "未知自动来源")
	}
	if _, err := defineWindow(filter.Window); err != nil {
		return nil, 0, fault.Field("window", "仅支持 24h 或 7d")
	}
	projects := make([]catalog.Project, 0)
	for page := 1; ; page++ {
		batch, total, err := catalog.ListProjects(ctx, q, catalog.ProjectFilter{Query: filter.Query, Category: filter.Category, Page: page, PageSize: 100})
		if err != nil {
			return nil, 0, err
		}
		projects = append(projects, batch...)
		if len(projects) >= total {
			break
		}
	}
	items := make([]ProjectTrend, 0, len(projects))
	for _, project := range projects {
		trend, err := loadProjectTrend(ctx, q, project, filter.AsOf, filter.Window)
		if err != nil {
			return nil, 0, err
		}
		if filter.Source != "" && !contains(trend.CrossSource.Platforms, filter.Source) {
			continue
		}
		items = append(items, ProjectTrend{Project: project, Trend: trend})
	}
	SortProjectTrends(items)
	total := len(items)
	start := (filter.Page - 1) * filter.PageSize
	if start >= total {
		return []ProjectTrend{}, total, nil
	}
	end := min(start+filter.PageSize, total)
	return items[start:end], total, nil
}

func loadProjectTrend(ctx context.Context, q catalog.Querier, project catalog.Project, asOf time.Time, window Window) (TrendResult, error) {
	records := make([]catalog.SourceRecord, 0)
	for page := 1; ; page++ {
		batch, total, err := catalog.ListSources(ctx, q, project.ID, catalog.SourceFilter{Page: page, PageSize: 100})
		if err != nil {
			return TrendResult{}, err
		}
		records = append(records, batch...)
		if len(records) >= total {
			break
		}
	}
	observations := make([]catalog.Observation, 0)
	for page := 1; ; page++ {
		batch, total, err := catalog.ListObservations(ctx, q, project.ID, catalog.ObservationFilter{From: asOf.Add(-90 * 24 * time.Hour), To: asOf, Page: page, PageSize: 100})
		if err != nil {
			return TrendResult{}, err
		}
		observations = append(observations, batch...)
		if len(observations) >= total {
			break
		}
	}
	return ComputeProject(project, records, observations, asOf, window)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
