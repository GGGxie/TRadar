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
