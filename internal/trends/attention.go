package trends

import (
	"math"
	"sort"
	"time"

	"tradar/internal/catalog"
)

// AttentionMetric is the latest cumulative value of one metric on one source
// record. It describes current attention, not growth.
type AttentionMetric struct {
	SourceRecordID string `json:"sourceRecordId"`
	Source         string `json:"source"`
	Metric         string `json:"metric"`
	Value          int64  `json:"value"`
	ObservedAt     string `json:"observedAt"`
	ObservationID  string `json:"observationId"`
}

// AttentionResult keeps per-platform totals side by side. Index is only a
// cold-start ordering aid: the maximum of log-normalized primary metrics, so
// values from different platforms are never added together.
type AttentionResult struct {
	Index   *float64          `json:"index"`
	Metrics []AttentionMetric `json:"metrics"`
}

var attentionMetrics = map[string][]string{
	"GITHUB":       {"STARS", "FORKS"},
	"HACKER_NEWS":  {"POINTS", "COMMENTS"},
	"PRODUCT_HUNT": {"VOTES", "COMMENTS"},
}

// 参考规模：约等于该平台「明显受关注」的累计量，使指数接近 1。
var attentionScale = map[string]float64{"GITHUB": 10000, "HACKER_NEWS": 500, "PRODUCT_HUNT": 500}

func computeAttention(records []catalog.SourceRecord, projectID string, observations []catalog.Observation, asOf time.Time) AttentionResult {
	result := AttentionResult{Metrics: []AttentionMetric{}}
	latest := latestCumulative(observations, asOf)
	for _, source := range []string{"GITHUB", "HACKER_NEWS", "PRODUCT_HUNT"} {
		primary := primaryMetric(source)
		var best *catalog.SourceRecord
		var bestValue int64
		for index := range records {
			record := &records[index]
			if record.ProjectID != projectID || record.Source != source {
				continue
			}
			observation, ok := latest[attentionKey{record.ID, primary}]
			if !ok {
				continue
			}
			if best == nil || observation.Value > bestValue || (observation.Value == bestValue && record.ID < best.ID) {
				best, bestValue = record, observation.Value
			}
		}
		if best == nil {
			continue
		}
		for _, metric := range attentionMetrics[source] {
			observation, ok := latest[attentionKey{best.ID, metric}]
			if !ok {
				continue
			}
			result.Metrics = append(result.Metrics, AttentionMetric{
				SourceRecordID: best.ID, Source: source, Metric: metric, Value: observation.Value,
				ObservedAt: observation.ObservedAt, ObservationID: observation.ID,
			})
		}
		index := math.Log1p(float64(max(bestValue, 0))) / math.Log1p(attentionScale[source])
		index = math.Round(index*1000) / 1000
		if result.Index == nil || index > *result.Index {
			result.Index = &index
		}
	}
	return result
}

type attentionKey struct{ sourceRecordID, metric string }

func latestCumulative(observations []catalog.Observation, asOf time.Time) map[attentionKey]catalog.Observation {
	candidates := make([]catalog.Observation, 0, len(observations))
	for _, observation := range observations {
		if observation.Basis != "CUMULATIVE" {
			continue
		}
		observed, err := time.Parse(time.RFC3339Nano, observation.ObservedAt)
		if err != nil || observed.After(asOf) {
			continue
		}
		candidates = append(candidates, observation)
	}
	// 时间戳为同一格式的 UTC 字符串，按 observedAt、fetchedAt、id 升序后取最后一条。
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].ObservedAt != candidates[j].ObservedAt {
			return candidates[i].ObservedAt < candidates[j].ObservedAt
		}
		if candidates[i].FetchedAt != candidates[j].FetchedAt {
			return candidates[i].FetchedAt < candidates[j].FetchedAt
		}
		return candidates[i].ID < candidates[j].ID
	})
	latest := make(map[attentionKey]catalog.Observation, len(candidates))
	for _, observation := range candidates {
		latest[attentionKey{observation.SourceRecordID, observation.Metric}] = observation
	}
	return latest
}
