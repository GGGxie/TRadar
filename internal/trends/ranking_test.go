package trends

import (
	"testing"
	"time"

	"tradar/internal/catalog"
)

func TestComputeProjectCountsPlatformsWithoutAddingDuplicateDiscussions(t *testing.T) {
	asOf := trendBase.Add(49 * time.Hour)
	project := trendProject("project-1", asOf.Add(-7*24*time.Hour))
	records := []catalog.SourceRecord{
		trendSource("github", project.ID, "GITHUB", asOf.Add(-time.Hour)),
		trendSource("hn-a", project.ID, "HACKER_NEWS", asOf.Add(-time.Hour)),
		trendSource("hn-b", project.ID, "HACKER_NEWS", asOf.Add(-2*time.Hour)),
		trendSource("ph", project.ID, "PRODUCT_HUNT", asOf.Add(-time.Hour)),
		trendSource("manual", project.ID, "MANUAL", asOf.Add(-time.Hour)),
	}
	observations := []catalog.Observation{
		trendPoint("g0", "github", "STARS", 100, trendBase),
		trendPoint("g1", "github", "STARS", 160, trendBase.Add(24*time.Hour)),
		trendPoint("g2", "github", "STARS", 260, trendBase.Add(48*time.Hour)),
	}
	result, err := ComputeProject(project, records, observations, asOf, Window24Hours)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.CrossSource.Platforms; len(got) != 3 || got[0] != "GITHUB" || got[1] != "HACKER_NEWS" || got[2] != "PRODUCT_HUNT" {
		t.Fatalf("platforms=%v", got)
	}
	if len(result.CrossSource.SourceRecordIDs) != 4 || result.Components.CrossSource != 30 {
		t.Fatalf("cross source=%+v components=%+v", result.CrossSource, result.Components)
	}
	if result.PrimarySignal == nil || result.PrimarySignal.SourceRecordID != "github" || result.Score == nil {
		t.Fatalf("result=%+v", result)
	}
	if !result.IsPartial || result.Components.Persistence != nil {
		t.Fatalf("missing persistence must remain explicit: %+v", result)
	}
}

func TestSortProjectTrendsIsStableForScoredAndAccumulatingGroups(t *testing.T) {
	newer := trendProject("b", trendBase.Add(time.Hour))
	older := trendProject("a", trendBase)
	tiedA, tiedB := 10.0, 10.0
	items := []ProjectTrend{
		{Project: older, Trend: TrendResult{ProjectID: older.ID}},
		{Project: newer, Trend: TrendResult{ProjectID: newer.ID}},
		{Project: trendProject("d", trendBase), Trend: TrendResult{ProjectID: "d", Score: &tiedA}},
		{Project: trendProject("c", trendBase), Trend: TrendResult{ProjectID: "c", Score: &tiedB}},
	}
	SortProjectTrends(items)
	want := []string{"c", "d", "b", "a"}
	for index, item := range items {
		if item.Project.ID != want[index] {
			t.Fatalf("order[%d]=%s want=%s", index, item.Project.ID, want[index])
		}
	}
}

func TestComputeProjectKeepsLatestAttentionPerPlatformWithoutScoring(t *testing.T) {
	asOf := trendBase.Add(49 * time.Hour)
	project := trendProject("project-1", trendBase)
	records := []catalog.SourceRecord{
		trendSource("github", project.ID, "GITHUB", asOf.Add(-time.Hour)),
		trendSource("hn-a", project.ID, "HACKER_NEWS", asOf.Add(-time.Hour)),
		trendSource("hn-b", project.ID, "HACKER_NEWS", asOf.Add(-2*time.Hour)),
	}
	observations := []catalog.Observation{
		trendPoint("g-old", "github", "STARS", 900, trendBase.Add(40*time.Hour)),
		trendPoint("g-new", "github", "STARS", 1200, trendBase.Add(48*time.Hour)),
		trendPoint("g-future", "github", "STARS", 5000, asOf.Add(time.Hour)),
		trendPoint("g-forks", "github", "FORKS", 30, trendBase.Add(48*time.Hour)),
		trendPoint("hn-a-points", "hn-a", "POINTS", 80, trendBase.Add(48*time.Hour)),
		trendPoint("hn-b-points", "hn-b", "POINTS", 140, trendBase.Add(47*time.Hour)),
		trendPoint("hn-b-comments", "hn-b", "COMMENTS", 12, trendBase.Add(47*time.Hour)),
	}
	result, err := ComputeProject(project, records, observations, asOf, Window24Hours)
	if err != nil {
		t.Fatal(err)
	}
	if result.Score != nil || result.Status != StatusInsufficientHistory {
		t.Fatalf("single-window samples must stay unscored: %+v", result)
	}
	want := []AttentionMetric{
		{SourceRecordID: "github", Source: "GITHUB", Metric: "STARS", Value: 1200, ObservedAt: catalog.Timestamp(trendBase.Add(48 * time.Hour)), ObservationID: "g-new"},
		{SourceRecordID: "github", Source: "GITHUB", Metric: "FORKS", Value: 30, ObservedAt: catalog.Timestamp(trendBase.Add(48 * time.Hour)), ObservationID: "g-forks"},
		{SourceRecordID: "hn-b", Source: "HACKER_NEWS", Metric: "POINTS", Value: 140, ObservedAt: catalog.Timestamp(trendBase.Add(47 * time.Hour)), ObservationID: "hn-b-points"},
		{SourceRecordID: "hn-b", Source: "HACKER_NEWS", Metric: "COMMENTS", Value: 12, ObservedAt: catalog.Timestamp(trendBase.Add(47 * time.Hour)), ObservationID: "hn-b-comments"},
	}
	if len(result.Attention.Metrics) != len(want) {
		t.Fatalf("metrics=%+v", result.Attention.Metrics)
	}
	for index := range want {
		if result.Attention.Metrics[index] != want[index] {
			t.Fatalf("metric[%d]=%+v want=%+v", index, result.Attention.Metrics[index], want[index])
		}
	}
	// 指数取各平台主指标归一化后的最大值，不把 Stars 与 points 相加。
	if result.Attention.Index == nil || *result.Attention.Index <= 0 {
		t.Fatalf("index=%v", result.Attention.Index)
	}
	githubOnly, _ := ComputeProject(project, records[:1], observations, asOf, Window24Hours)
	hnOnly, _ := ComputeProject(project, records[1:], observations, asOf, Window24Hours)
	if *result.Attention.Index != max(*githubOnly.Attention.Index, *hnOnly.Attention.Index) {
		t.Fatalf("index=%v github=%v hn=%v", *result.Attention.Index, *githubOnly.Attention.Index, *hnOnly.Attention.Index)
	}
}

func TestComputeProjectWithoutObservationsHasNoAttention(t *testing.T) {
	asOf := trendBase.Add(time.Hour)
	project := trendProject("project-1", trendBase)
	result, err := ComputeProject(project, []catalog.SourceRecord{trendSource("github", project.ID, "GITHUB", trendBase)}, nil, asOf, Window24Hours)
	if err != nil {
		t.Fatal(err)
	}
	if result.Attention.Index != nil || result.Attention.Metrics == nil || len(result.Attention.Metrics) != 0 {
		t.Fatalf("attention=%+v", result.Attention)
	}
}

func TestSortProjectTrendsOrdersAccumulatingGroupByAttention(t *testing.T) {
	low, high, scored := 0.2, 0.8, 5.0
	items := []ProjectTrend{
		{Project: trendProject("no-attention-newest", trendBase.Add(3*time.Hour)), Trend: TrendResult{}},
		{Project: trendProject("low", trendBase.Add(2*time.Hour)), Trend: TrendResult{Attention: AttentionResult{Index: &low}}},
		{Project: trendProject("high", trendBase), Trend: TrendResult{Attention: AttentionResult{Index: &high}}},
		{Project: trendProject("scored", trendBase), Trend: TrendResult{Score: &scored}},
	}
	SortProjectTrends(items)
	want := []string{"scored", "high", "low", "no-attention-newest"}
	for index, item := range items {
		if item.Project.ID != want[index] {
			t.Fatalf("order[%d]=%s want=%s", index, item.Project.ID, want[index])
		}
	}
}

func TestComputeProjectAlertsOnlyOnFreshThreeSampleAcceleration(t *testing.T) {
	asOf := trendBase.Add(49 * time.Hour)
	project := trendProject("project-1", trendBase)
	record := trendSource("github", project.ID, "GITHUB", asOf.Add(-time.Hour))
	highTotal, err := ComputeProject(project, []catalog.SourceRecord{record}, []catalog.Observation{
		trendPoint("only", record.ID, "STARS", 10000, trendBase.Add(48*time.Hour)),
	}, asOf, Window24Hours)
	if err != nil {
		t.Fatal(err)
	}
	if highTotal.Alert != nil || highTotal.Score != nil {
		t.Fatalf("single high total should accumulate history: %+v", highTotal)
	}

	accelerating, err := ComputeProject(project, []catalog.SourceRecord{record}, []catalog.Observation{
		trendPoint("old", record.ID, "STARS", 100, trendBase),
		trendPoint("middle", record.ID, "STARS", 150, trendBase.Add(24*time.Hour)),
		trendPoint("latest", record.ID, "STARS", 350, trendBase.Add(48*time.Hour)),
	}, asOf, Window24Hours)
	if err != nil {
		t.Fatal(err)
	}
	if accelerating.Alert == nil || accelerating.Alert.Kind != "ACCELERATING_GROWTH" || accelerating.Alert.Threshold != 100 || len(accelerating.Alert.ObservationIDs) != 3 {
		t.Fatalf("alert=%+v", accelerating.Alert)
	}
}

func trendProject(id string, firstSeen time.Time) catalog.Project {
	return catalog.Project{ID: id, Name: id, FirstSeenAt: catalog.Timestamp(firstSeen)}
}

func trendSource(id, projectID, source string, fetched time.Time) catalog.SourceRecord {
	return catalog.SourceRecord{ID: id, ProjectID: projectID, Source: source, FetchedAt: catalog.Timestamp(fetched)}
}

func trendPoint(id, sourceRecordID, metric string, value int64, observed time.Time) catalog.Observation {
	return catalog.Observation{ID: id, SourceRecordID: sourceRecordID, Metric: metric, Basis: "CUMULATIVE", Value: value, ObservedAt: catalog.Timestamp(observed), FetchedAt: catalog.Timestamp(observed)}
}
