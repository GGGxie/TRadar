// Package trends calculates deterministic growth signals from catalog observations.
package trends

import (
	"fmt"
	"sort"
	"time"

	"tradar/internal/catalog"
)

type Window string

const (
	Window24Hours Window = "24h"
	Window7Days   Window = "7d"
)

type Status string

const (
	StatusReady               Status = "READY"
	StatusInsufficientHistory Status = "INSUFFICIENT_HISTORY"
	StatusStale               Status = "STALE"
	StatusUnsupported         Status = "UNSUPPORTED"
)

type SignalInput struct {
	SourceRecordID string
	Source         string
	Metric         string
	Observations   []catalog.Observation
}

type GrowthResult struct {
	Status         Status   `json:"status"`
	From           *string  `json:"from"`
	To             *string  `json:"to"`
	DurationHours  *float64 `json:"durationHours"`
	Delta          *int64   `json:"delta"`
	RatePerDay     *float64 `json:"ratePerDay"`
	GrowthRatio    *float64 `json:"growthRatio"`
	ObservationIDs []string `json:"observationIds"`
}

type AccelerationResult struct {
	Status         Status   `json:"status"`
	Value          *float64 `json:"value"`
	ObservationIDs []string `json:"observationIds"`
}

type PersistenceResult struct {
	Status          Status `json:"status"`
	PositiveWindows *int   `json:"positiveWindows"`
	ValidWindows    int    `json:"validWindows"`
	TotalWindows    int    `json:"totalWindows"`
}

type SignalTrend struct {
	Status              Status             `json:"status"`
	LatestValue         *int64             `json:"latestValue"`
	LatestObservedAt    *string            `json:"latestObservedAt"`
	LatestObservationID *string            `json:"latestObservationId"`
	Growth              GrowthResult       `json:"growth"`
	Acceleration        AccelerationResult `json:"acceleration"`
	Persistence         PersistenceResult  `json:"persistence"`
}

type windowDefinition struct {
	duration  time.Duration
	tolerance time.Duration
}

type sample struct {
	id       string
	value    int64
	observed time.Time
	fetched  time.Time
}

// Calculate selects cumulative samples for one source record and metric. It is
// pure: callers provide the cutoff time, and the result depends only on input.
func Calculate(input SignalInput, asOf time.Time, window Window) (SignalTrend, error) {
	definition, err := defineWindow(window)
	if err != nil {
		return SignalTrend{}, err
	}
	result := emptyTrend(StatusInsufficientHistory)
	if asOf.IsZero() {
		return SignalTrend{}, fmt.Errorf("asOf is required")
	}
	if !supportedSignal(input.Source, input.Metric) {
		return emptyTrend(StatusUnsupported), nil
	}
	samples, err := normalizeSamples(input, asOf.UTC())
	if err != nil {
		return SignalTrend{}, err
	}
	if len(samples) == 0 {
		return result, nil
	}
	latest := samples[len(samples)-1]
	latestTime := catalog.Timestamp(latest.observed)
	latestID := latest.id
	latestValue := latest.value
	result.LatestValue = &latestValue
	result.LatestObservedAt = &latestTime
	result.LatestObservationID = &latestID

	previous := selectBoundary(samples, latest.observed.Add(-definition.duration), definition.tolerance)
	if previous != nil {
		result.Growth = calculateGrowth(*previous, latest, StatusReady)
		result.Status = StatusReady
		result.Acceleration = calculateAcceleration(samples, *previous, latest, definition)
	}
	result.Persistence = calculatePersistence(samples, latest)
	if asOf.UTC().Sub(latest.observed) > 3*time.Hour {
		result.Status = StatusStale
		if result.Growth.Delta != nil {
			result.Growth.Status = StatusStale
		}
		if result.Acceleration.Value != nil {
			result.Acceleration.Status = StatusStale
		}
		if result.Persistence.PositiveWindows != nil {
			result.Persistence.Status = StatusStale
		}
	}
	return result, nil
}

func emptyTrend(status Status) SignalTrend {
	return SignalTrend{
		Status:       status,
		Growth:       GrowthResult{Status: status, ObservationIDs: []string{}},
		Acceleration: AccelerationResult{Status: status, ObservationIDs: []string{}},
		Persistence:  PersistenceResult{Status: status, TotalWindows: 7},
	}
}

func defineWindow(window Window) (windowDefinition, error) {
	switch window {
	case Window24Hours:
		return windowDefinition{duration: 24 * time.Hour, tolerance: 2 * time.Hour}, nil
	case Window7Days:
		return windowDefinition{duration: 7 * 24 * time.Hour, tolerance: 6 * time.Hour}, nil
	default:
		return windowDefinition{}, fmt.Errorf("unsupported trend window %q", window)
	}
}

func supportedSignal(source, metric string) bool {
	return source == "GITHUB" && metric == "STARS" ||
		source == "HACKER_NEWS" && metric == "POINTS" ||
		source == "PRODUCT_HUNT" && metric == "VOTES"
}

func normalizeSamples(input SignalInput, asOf time.Time) ([]sample, error) {
	items := make([]sample, 0, len(input.Observations))
	for _, observation := range input.Observations {
		if observation.SourceRecordID != input.SourceRecordID || observation.Metric != input.Metric || observation.Basis != "CUMULATIVE" {
			continue
		}
		observed, err := time.Parse(time.RFC3339Nano, observation.ObservedAt)
		if err != nil {
			return nil, fmt.Errorf("observation %q observedAt: %w", observation.ID, err)
		}
		fetched, err := time.Parse(time.RFC3339Nano, observation.FetchedAt)
		if err != nil {
			return nil, fmt.Errorf("observation %q fetchedAt: %w", observation.ID, err)
		}
		observed = observed.UTC()
		if observed.After(asOf) {
			continue
		}
		items = append(items, sample{id: observation.ID, value: observation.Value, observed: observed, fetched: fetched.UTC()})
	}
	sort.Slice(items, func(i, j int) bool {
		if !items[i].observed.Equal(items[j].observed) {
			return items[i].observed.Before(items[j].observed)
		}
		if !items[i].fetched.Equal(items[j].fetched) {
			return items[i].fetched.Before(items[j].fetched)
		}
		return items[i].id < items[j].id
	})
	deduplicated := make([]sample, 0, len(items))
	for _, item := range items {
		if len(deduplicated) > 0 && deduplicated[len(deduplicated)-1].observed.Equal(item.observed) {
			deduplicated[len(deduplicated)-1] = item
			continue
		}
		deduplicated = append(deduplicated, item)
	}
	return deduplicated, nil
}

func selectBoundary(samples []sample, target time.Time, tolerance time.Duration) *sample {
	index := sort.Search(len(samples), func(index int) bool {
		return samples[index].observed.After(target)
	}) - 1
	if index < 0 || target.Sub(samples[index].observed) > tolerance {
		return nil
	}
	selected := samples[index]
	return &selected
}

func calculateGrowth(from, to sample, status Status) GrowthResult {
	durationHours := to.observed.Sub(from.observed).Hours()
	delta := to.value - from.value
	rate := float64(delta) / (durationHours / 24)
	fromTime, toTime := catalog.Timestamp(from.observed), catalog.Timestamp(to.observed)
	result := GrowthResult{
		Status: status, From: &fromTime, To: &toTime, DurationHours: &durationHours,
		Delta: &delta, RatePerDay: &rate, ObservationIDs: []string{from.id, to.id},
	}
	if from.value != 0 {
		ratio := float64(delta) / float64(from.value)
		result.GrowthRatio = &ratio
	}
	return result
}

func calculateAcceleration(samples []sample, middle, latest sample, definition windowDefinition) AccelerationResult {
	result := AccelerationResult{Status: StatusInsufficientHistory, ObservationIDs: []string{}}
	oldest := selectBoundary(samples, middle.observed.Add(-definition.duration), definition.tolerance)
	if oldest == nil {
		return result
	}
	oldDurationDays := middle.observed.Sub(oldest.observed).Hours() / 24
	newDurationDays := latest.observed.Sub(middle.observed).Hours() / 24
	midpointDistanceDays := latest.observed.Sub(oldest.observed).Hours() / 48
	if oldDurationDays <= 0 || newDurationDays <= 0 || midpointDistanceDays <= 0 {
		return result
	}
	oldRate := float64(middle.value-oldest.value) / oldDurationDays
	newRate := float64(latest.value-middle.value) / newDurationDays
	value := (newRate - oldRate) / midpointDistanceDays
	return AccelerationResult{Status: StatusReady, Value: &value, ObservationIDs: []string{oldest.id, middle.id, latest.id}}
}

func calculatePersistence(samples []sample, latest sample) PersistenceResult {
	result := PersistenceResult{Status: StatusInsufficientHistory, TotalWindows: 7}
	boundaries := make([]*sample, 8)
	latestCopy := latest
	boundaries[0] = &latestCopy
	for index := 1; index < len(boundaries); index++ {
		boundaries[index] = selectBoundary(samples, latest.observed.Add(-time.Duration(index)*24*time.Hour), 2*time.Hour)
	}
	positive := 0
	for index := 0; index < 7; index++ {
		newer, older := boundaries[index], boundaries[index+1]
		if newer == nil || older == nil {
			continue
		}
		duration := newer.observed.Sub(older.observed)
		if duration < 24*time.Hour || duration > 26*time.Hour {
			continue
		}
		result.ValidWindows++
		if newer.value > older.value {
			positive++
		}
	}
	if result.ValidWindows >= 4 {
		result.Status = StatusReady
		result.PositiveWindows = &positive
	}
	return result
}
