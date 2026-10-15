package trends

import (
	"math"
	"testing"
	"time"

	"tradar/internal/catalog"
)

var trendBase = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func TestCalculateGrowthWindows(t *testing.T) {
	tests := []struct {
		name     string
		window   Window
		points   []catalog.Observation
		asOf     time.Time
		status   Status
		delta    *int64
		rate     *float64
		ratio    *float64
		duration *float64
		latest   *int64
	}{
		{
			name: "single snapshot keeps latest but has no invented growth", window: Window24Hours,
			points: []catalog.Observation{point("latest", 150, trendBase.Add(24*time.Hour), "CUMULATIVE")},
			asOf:   trendBase.Add(25 * time.Hour), status: StatusInsufficientHistory, latest: int64ptr(150),
		},
		{
			name: "25 hour span reports its real duration", window: Window24Hours,
			points: []catalog.Observation{
				point("from", 100, trendBase, "CUMULATIVE"),
				point("to", 150, trendBase.Add(25*time.Hour), "CUMULATIVE"),
			},
			asOf: trendBase.Add(26 * time.Hour), status: StatusReady, latest: int64ptr(150),
			delta: int64ptr(50), rate: float64ptr(48), ratio: float64ptr(.5), duration: float64ptr(25),
		},
		{
			name: "negative changes remain negative", window: Window24Hours,
			points: []catalog.Observation{
				point("from", 100, trendBase, "CUMULATIVE"),
				point("to", 90, trendBase.Add(24*time.Hour), "CUMULATIVE"),
			},
			asOf: trendBase.Add(25 * time.Hour), status: StatusReady, latest: int64ptr(90),
			delta: int64ptr(-10), rate: float64ptr(-10), ratio: float64ptr(-.1), duration: float64ptr(24),
		},
		{
			name: "zero baseline has no ratio", window: Window24Hours,
			points: []catalog.Observation{
				point("from", 0, trendBase, "CUMULATIVE"),
				point("to", 10, trendBase.Add(24*time.Hour), "CUMULATIVE"),
			},
			asOf: trendBase.Add(25 * time.Hour), status: StatusReady, latest: int64ptr(10),
			delta: int64ptr(10), rate: float64ptr(10), ratio: nil, duration: float64ptr(24),
		},
		{
			name: "24 hour sample outside tolerance is rejected", window: Window24Hours,
			points: []catalog.Observation{
				point("too-old", 100, trendBase, "CUMULATIVE"),
				point("to", 150, trendBase.Add(27*time.Hour), "CUMULATIVE"),
			},
			asOf: trendBase.Add(28 * time.Hour), status: StatusInsufficientHistory, latest: int64ptr(150),
		},
		{
			name: "seven day window accepts six hour tolerance", window: Window7Days,
			points: []catalog.Observation{
				point("from", 100, trendBase, "CUMULATIVE"),
				point("to", 269, trendBase.Add(169*time.Hour), "CUMULATIVE"),
			},
			asOf: trendBase.Add(170 * time.Hour), status: StatusReady, latest: int64ptr(269),
			delta: int64ptr(169), rate: float64ptr(24), ratio: float64ptr(1.69), duration: float64ptr(169),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := Calculate(SignalInput{SourceRecordID: "source-1", Source: "GITHUB", Metric: "STARS", Observations: tt.points}, tt.asOf, tt.window)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != tt.status || !sameInt(result.LatestValue, tt.latest) || !sameInt(result.Growth.Delta, tt.delta) || !closeFloat(result.Growth.RatePerDay, tt.rate) || !closeFloat(result.Growth.GrowthRatio, tt.ratio) || !closeFloat(result.Growth.DurationHours, tt.duration) {
				t.Fatalf("result=%+v growth=%+v", result, result.Growth)
			}
		})
	}
}

func TestCalculateMarksOldLatestStaleWhileKeepingMeasuredValues(t *testing.T) {
	result, err := Calculate(SignalInput{SourceRecordID: "source-1", Source: "GITHUB", Metric: "STARS", Observations: []catalog.Observation{
		point("from", 100, trendBase, "CUMULATIVE"),
		point("to", 124, trendBase.Add(24*time.Hour), "CUMULATIVE"),
	}}, trendBase.Add(28*time.Hour), Window24Hours)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusStale || result.Growth.Status != StatusStale || result.Growth.Delta == nil || *result.Growth.Delta != 24 {
		t.Fatalf("stale result=%+v", result)
	}
}

func TestCalculateAccelerationUsesComparableCumulativeSamples(t *testing.T) {
	points := []catalog.Observation{
		point("latest", 172, trendBase.Add(48*time.Hour), "CUMULATIVE"),
		point("middle-a", 124, trendBase.Add(24*time.Hour), "CUMULATIVE"),
		point("old", 100, trendBase, "CUMULATIVE"),
		pointFetched("middle-b", 124, trendBase.Add(24*time.Hour), trendBase.Add(24*time.Hour+time.Minute), "CUMULATIVE"),
	}
	result, err := Calculate(SignalInput{SourceRecordID: "source-1", Source: "GITHUB", Metric: "STARS", Observations: points}, trendBase.Add(49*time.Hour), Window24Hours)
	if err != nil {
		t.Fatal(err)
	}
	if result.Acceleration.Status != StatusReady || result.Acceleration.Value == nil || math.Abs(*result.Acceleration.Value-24) > 1e-9 {
		t.Fatalf("acceleration=%+v", result.Acceleration)
	}
	if got := result.Acceleration.ObservationIDs; len(got) != 3 || got[0] != "old" || got[1] != "middle-b" || got[2] != "latest" {
		t.Fatalf("observation ids=%v", got)
	}
}

func TestCalculateIgnoresDailyOtherMetricAndFutureObservations(t *testing.T) {
	points := []catalog.Observation{
		point("from", 100, trendBase, "CUMULATIVE"),
		point("daily", 9000, trendBase, "SOURCE_DAILY"),
		point("daily-refetch", 12000, trendBase, "SOURCE_DAILY"),
		pointMetric("forks", 8000, trendBase.Add(24*time.Hour), "CUMULATIVE", "FORKS"),
		point("latest", 125, trendBase.Add(24*time.Hour), "CUMULATIVE"),
		point("future", 9999, trendBase.Add(27*time.Hour), "CUMULATIVE"),
	}
	result, err := Calculate(SignalInput{SourceRecordID: "source-1", Source: "GITHUB", Metric: "STARS", Observations: points}, trendBase.Add(25*time.Hour), Window24Hours)
	if err != nil {
		t.Fatal(err)
	}
	if result.Growth.Delta == nil || *result.Growth.Delta != 25 || result.LatestValue == nil || *result.LatestValue != 125 {
		t.Fatalf("filtered result=%+v", result)
	}
}

func TestCalculatePersistenceRequiresFourCompleteNonOverlappingWindows(t *testing.T) {
	values := []int64{100, 110, 105, 120, 130, 130, 150, 140}
	points := make([]catalog.Observation, 0, len(values))
	for index, value := range values {
		points = append(points, point(time.Duration(index).String(), value, trendBase.Add(time.Duration(index)*24*time.Hour), "CUMULATIVE"))
	}
	result, err := Calculate(SignalInput{SourceRecordID: "source-1", Source: "GITHUB", Metric: "STARS", Observations: points}, trendBase.Add(7*24*time.Hour+time.Hour), Window24Hours)
	if err != nil {
		t.Fatal(err)
	}
	if result.Persistence.Status != StatusReady || result.Persistence.ValidWindows != 7 || result.Persistence.PositiveWindows == nil || *result.Persistence.PositiveWindows != 4 {
		t.Fatalf("persistence=%+v", result.Persistence)
	}

	short, err := Calculate(SignalInput{SourceRecordID: "source-1", Source: "GITHUB", Metric: "STARS", Observations: points[:4]}, trendBase.Add(3*24*time.Hour+time.Hour), Window24Hours)
	if err != nil {
		t.Fatal(err)
	}
	if short.Persistence.Status != StatusInsufficientHistory || short.Persistence.PositiveWindows != nil || short.Persistence.ValidWindows != 3 {
		t.Fatalf("short persistence=%+v", short.Persistence)
	}

	withGap := append([]catalog.Observation(nil), points[:3]...)
	withGap = append(withGap, points[4:]...)
	gapped, err := Calculate(SignalInput{SourceRecordID: "source-1", Source: "GITHUB", Metric: "STARS", Observations: withGap}, trendBase.Add(7*24*time.Hour+time.Hour), Window24Hours)
	if err != nil {
		t.Fatal(err)
	}
	if gapped.Persistence.Status != StatusReady || gapped.Persistence.ValidWindows != 5 {
		t.Fatalf("gapped persistence=%+v", gapped.Persistence)
	}
}

func TestCalculateReturnsUnsupportedWithoutInventingValues(t *testing.T) {
	result, err := Calculate(SignalInput{SourceRecordID: "source-1", Source: "GITHUB", Metric: "COMMENTS", Observations: []catalog.Observation{
		pointMetric("comment", 20, trendBase, "CUMULATIVE", "COMMENTS"),
	}}, trendBase.Add(time.Hour), Window24Hours)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusUnsupported || result.LatestValue != nil || result.Growth.Delta != nil {
		t.Fatalf("unsupported=%+v", result)
	}
}

func point(id string, value int64, observed time.Time, basis string) catalog.Observation {
	return pointFetched(id, value, observed, observed, basis)
}

func pointFetched(id string, value int64, observed, fetched time.Time, basis string) catalog.Observation {
	return catalog.Observation{ID: id, SourceRecordID: "source-1", Metric: "STARS", Basis: basis, Value: value, ObservedAt: observed.Format(time.RFC3339Nano), FetchedAt: fetched.Format(time.RFC3339Nano)}
}

func pointMetric(id string, value int64, observed time.Time, basis, metric string) catalog.Observation {
	item := point(id, value, observed, basis)
	item.Metric = metric
	return item
}

func int64ptr(value int64) *int64       { return &value }
func float64ptr(value float64) *float64 { return &value }

func sameInt(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func closeFloat(left, right *float64) bool {
	return left == nil && right == nil || left != nil && right != nil && math.Abs(*left-*right) < 1e-9
}
