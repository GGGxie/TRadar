package trends

import (
	"fmt"
	"math"
	"sort"
	"time"

	"tradar/internal/catalog"
)

const AlgorithmVersion = "trend-v1"

type PrimarySignal struct {
	SourceRecordID   string `json:"sourceRecordId"`
	Source           string `json:"source"`
	Metric           string `json:"metric"`
	LatestValue      int64  `json:"latestValue"`
	LatestObservedAt string `json:"latestObservedAt"`
}

type CrossSourceResult struct {
	Platforms       []string `json:"platforms"`
	SourceRecordIDs []string `json:"sourceRecordIds"`
}

type ScoreComponents struct {
	Growth      *float64 `json:"growth"`
	CrossSource float64  `json:"crossSource"`
	Persistence *float64 `json:"persistence"`
}

type TrendAlert struct {
	Kind           string   `json:"kind"`
	Threshold      float64  `json:"threshold"`
	ObservationIDs []string `json:"observationIds"`
}

type TrendResult struct {
	ProjectID        string             `json:"projectId"`
	AsOf             string             `json:"asOf"`
	AlgorithmVersion string             `json:"algorithmVersion"`
	Status           Status             `json:"status"`
	Score            *float64           `json:"score"`
	IsPartial        bool               `json:"isPartial"`
	PrimarySignal    *PrimarySignal     `json:"primarySignal"`
	Growth           GrowthResult       `json:"growth"`
	Acceleration     AccelerationResult `json:"acceleration"`
	Persistence      PersistenceResult  `json:"persistence"`
	CrossSource      CrossSourceResult  `json:"crossSource"`
	Components       ScoreComponents    `json:"components"`
	Explanations     []string           `json:"explanations"`
	Alert            *TrendAlert        `json:"alert"`
}

type ProjectTrend struct {
	Project catalog.Project `json:"project"`
	Trend   TrendResult     `json:"trend"`
}

type signalCandidate struct {
	record catalog.SourceRecord
	metric string
	trend  SignalTrend
	growth float64
}

func ComputeProject(project catalog.Project, records []catalog.SourceRecord, observations []catalog.Observation, asOf time.Time, window Window) (TrendResult, error) {
	if asOf.IsZero() {
		return TrendResult{}, fmt.Errorf("asOf is required")
	}
	if _, err := defineWindow(window); err != nil {
		return TrendResult{}, err
	}
	result := TrendResult{
		ProjectID: project.ID, AsOf: catalog.Timestamp(asOf), AlgorithmVersion: AlgorithmVersion,
		Status: StatusInsufficientHistory, Growth: GrowthResult{Status: StatusInsufficientHistory, ObservationIDs: []string{}},
		Acceleration: AccelerationResult{Status: StatusInsufficientHistory, ObservationIDs: []string{}},
		Persistence:  PersistenceResult{Status: StatusInsufficientHistory, TotalWindows: 7},
		CrossSource:  crossSource(records, project.ID, asOf.UTC()), Explanations: []string{},
	}
	result.Components.CrossSource = 15 * float64(max(0, min(len(result.CrossSource.Platforms), 3)-1))

	candidates := make([]signalCandidate, 0, len(records))
	hasSupported, hasStale := false, false
	for _, record := range records {
		if record.ProjectID != project.ID {
			continue
		}
		metric := primaryMetric(record.Source)
		if metric == "" {
			continue
		}
		hasSupported = true
		trend, err := Calculate(SignalInput{SourceRecordID: record.ID, Source: record.Source, Metric: metric, Observations: observations}, asOf, window)
		if err != nil {
			return TrendResult{}, err
		}
		if trend.Status == StatusStale {
			hasStale = true
		}
		if trend.Status != StatusReady || trend.Growth.RatePerDay == nil {
			continue
		}
		candidates = append(candidates, signalCandidate{record: record, metric: metric, trend: trend, growth: growthComponent(record.Source, *trend.Growth.RatePerDay)})
	}
	if len(candidates) == 0 {
		if hasStale {
			result.Status = StatusStale
		} else if !hasSupported {
			result.Status = StatusUnsupported
		}
		result.IsPartial = true
		result.Explanations = append(result.Explanations, "尚无新鲜且窗口完整的累计观测，暂不评分。")
		return result, nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].growth != candidates[j].growth {
			return candidates[i].growth > candidates[j].growth
		}
		return candidates[i].record.ID < candidates[j].record.ID
	})
	primary := candidates[0]
	result.Status = StatusReady
	result.Growth = primary.trend.Growth
	result.Acceleration = primary.trend.Acceleration
	result.Persistence = primary.trend.Persistence
	result.PrimarySignal = &PrimarySignal{
		SourceRecordID: primary.record.ID, Source: primary.record.Source, Metric: primary.metric,
		LatestValue: *primary.trend.LatestValue, LatestObservedAt: *primary.trend.LatestObservedAt,
	}
	growthScore := primary.growth
	result.Components.Growth = &growthScore
	persistenceScore := 0.0
	if primary.trend.Persistence.Status == StatusReady && primary.trend.Persistence.PositiveWindows != nil && primary.trend.Persistence.ValidWindows > 0 {
		persistenceScore = 20 * float64(*primary.trend.Persistence.PositiveWindows) / float64(primary.trend.Persistence.ValidWindows)
		result.Components.Persistence = &persistenceScore
	}
	result.IsPartial = result.Components.Persistence == nil
	score := math.Round((growthScore+result.Components.CrossSource+persistenceScore)*10) / 10
	result.Score = &score
	result.Explanations = append(result.Explanations,
		fmt.Sprintf("主信号采用 %s %s 的观测期净变化。", primary.record.Source, primary.metric),
		fmt.Sprintf("过去 7 天关联到 %d 个独立自动来源平台。", len(result.CrossSource.Platforms)),
	)
	if result.IsPartial {
		result.Explanations = append(result.Explanations, "完整日窗不足，持续性暂不计分。")
	}
	result.Alert, _ = acceleratingAlert(primary, observations, asOf.UTC(), window)
	return result, nil
}

func growthComponent(source string, rate float64) float64 {
	scale := map[string]float64{"GITHUB": 1000, "HACKER_NEWS": 100, "PRODUCT_HUNT": 100}[source]
	return 50 * math.Min(1, math.Log1p(math.Max(rate, 0))/math.Log1p(scale))
}

func primaryMetric(source string) string {
	return map[string]string{"GITHUB": "STARS", "HACKER_NEWS": "POINTS", "PRODUCT_HUNT": "VOTES"}[source]
}

func crossSource(records []catalog.SourceRecord, projectID string, asOf time.Time) CrossSourceResult {
	type included struct{ source, id string }
	items := make([]included, 0, len(records))
	seenPlatforms := make(map[string]bool)
	for _, record := range records {
		if record.ProjectID != projectID || primaryMetric(record.Source) == "" {
			continue
		}
		fetched, err := time.Parse(time.RFC3339Nano, record.FetchedAt)
		if err != nil || fetched.After(asOf) || fetched.Before(asOf.Add(-7*24*time.Hour)) {
			continue
		}
		items = append(items, included{record.Source, record.ID})
		seenPlatforms[record.Source] = true
	}
	order := map[string]int{"GITHUB": 0, "HACKER_NEWS": 1, "PRODUCT_HUNT": 2}
	sort.Slice(items, func(i, j int) bool {
		if order[items[i].source] != order[items[j].source] {
			return order[items[i].source] < order[items[j].source]
		}
		return items[i].id < items[j].id
	})
	result := CrossSourceResult{Platforms: []string{}, SourceRecordIDs: []string{}}
	for _, source := range []string{"GITHUB", "HACKER_NEWS", "PRODUCT_HUNT"} {
		if seenPlatforms[source] {
			result.Platforms = append(result.Platforms, source)
		}
	}
	for _, item := range items {
		result.SourceRecordIDs = append(result.SourceRecordIDs, item.id)
	}
	return result
}

func acceleratingAlert(candidate signalCandidate, observations []catalog.Observation, asOf time.Time, window Window) (*TrendAlert, error) {
	if candidate.trend.Status != StatusReady || candidate.trend.Acceleration.Status != StatusReady {
		return nil, nil
	}
	definition, _ := defineWindow(window)
	samples, err := normalizeSamples(SignalInput{SourceRecordID: candidate.record.ID, Source: candidate.record.Source, Metric: candidate.metric, Observations: observations}, asOf)
	if err != nil || len(samples) < 3 {
		return nil, err
	}
	latest := samples[len(samples)-1]
	middle := selectBoundary(samples, latest.observed.Add(-definition.duration), definition.tolerance)
	if middle == nil {
		return nil, nil
	}
	oldest := selectBoundary(samples, middle.observed.Add(-definition.duration), definition.tolerance)
	if oldest == nil {
		return nil, nil
	}
	oldDays := middle.observed.Sub(oldest.observed).Hours() / 24
	newDays := latest.observed.Sub(middle.observed).Hours() / 24
	if oldDays <= 0 || newDays <= 0 {
		return nil, nil
	}
	oldRate := float64(middle.value-oldest.value) / oldDays
	newRate := float64(latest.value-middle.value) / newDays
	threshold := map[string]float64{"GITHUB": 100, "HACKER_NEWS": 30, "PRODUCT_HUNT": 30}[candidate.record.Source]
	if newRate < threshold || newRate < 2*math.Max(oldRate, 0) {
		return nil, nil
	}
	return &TrendAlert{Kind: "ACCELERATING_GROWTH", Threshold: threshold, ObservationIDs: []string{oldest.id, middle.id, latest.id}}, nil
}

func SortProjectTrends(items []ProjectTrend) {
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		if left.Trend.Score != nil && right.Trend.Score == nil {
			return true
		}
		if left.Trend.Score == nil && right.Trend.Score != nil {
			return false
		}
		if left.Trend.Score != nil && right.Trend.Score != nil && *left.Trend.Score != *right.Trend.Score {
			return *left.Trend.Score > *right.Trend.Score
		}
		if left.Project.FirstSeenAt != right.Project.FirstSeenAt {
			return left.Project.FirstSeenAt > right.Project.FirstSeenAt
		}
		return left.Project.ID < right.Project.ID
	})
}
