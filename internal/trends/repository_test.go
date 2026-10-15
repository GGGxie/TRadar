package trends

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"tradar/internal/catalog"
	"tradar/internal/ingestion"
	"tradar/internal/storage"
)

func TestListRadarLoadsCatalogDataBeforeFilteringAndPagination(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	asOf := time.Date(2026, 10, 15, 6, 0, 0, 0, time.UTC)
	var project catalog.Project
	if err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var createErr error
		project, _, createErr = catalog.CreateProject(ctx, tx, catalog.CreateProjectInput{Name: "Radar", CanonicalURL: "https://example.com/radar", Category: "DEVTOOLS"}, asOf.Add(-48*time.Hour))
		if createErr != nil {
			return createErr
		}
		firstRun, createErr := ingestion.CreateRun(ctx, tx, ingestion.CreateRunInput{Source: ingestion.GitHub, Trigger: ingestion.TriggerManual}, asOf.Add(-24*time.Hour))
		if createErr != nil {
			return createErr
		}
		source, _, createErr := catalog.UpsertSource(ctx, tx, catalog.UpsertSourceInput{ProjectID: project.ID, Source: "GITHUB", ExternalID: "42", RecordType: "PROJECT", URL: "https://github.com/acme/radar", Title: "acme/radar", FetchedAt: asOf})
		if createErr != nil {
			return createErr
		}
		if _, createErr = catalog.AppendObservations(ctx, tx, firstRun.ID, source.ID, []catalog.ObservationInput{{Metric: "STARS", Basis: "CUMULATIVE", Value: 100, ObservedAt: asOf.Add(-24 * time.Hour), FetchedAt: asOf.Add(-24 * time.Hour)}}); createErr != nil {
			return createErr
		}
		if _, createErr = tx.ExecContext(ctx, `UPDATE ingestion_runs SET status='INTERRUPTED',finished_at=? WHERE id=?`, catalog.Timestamp(asOf.Add(-23*time.Hour)), firstRun.ID); createErr != nil {
			return createErr
		}
		secondRun, createErr := ingestion.CreateRun(ctx, tx, ingestion.CreateRunInput{Source: ingestion.GitHub, Trigger: ingestion.TriggerManual}, asOf)
		if createErr != nil {
			return createErr
		}
		_, createErr = catalog.AppendObservations(ctx, tx, secondRun.ID, source.ID, []catalog.ObservationInput{{Metric: "STARS", Basis: "CUMULATIVE", Value: 160, ObservedAt: asOf, FetchedAt: asOf}})
		return createErr
	}); err != nil {
		t.Fatal(err)
	}
	items, total, err := ListRadar(ctx, db, RadarFilter{Category: "DEVTOOLS", Source: "GITHUB", Window: Window24Hours, AsOf: asOf, Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(items) != 1 || items[0].Trend.Score == nil || items[0].Trend.Growth.Delta == nil || *items[0].Trend.Growth.Delta != 60 {
		t.Fatalf("items=%+v total=%d err=%v", items, total, err)
	}
	filtered, _, err := ListRadar(ctx, db, RadarFilter{Query: "Radar", Window: Window24Hours, AsOf: asOf, Page: 1, PageSize: 20})
	if err != nil || len(filtered) != 1 || *filtered[0].Trend.Score != *items[0].Trend.Score {
		t.Fatalf("filter changed score: %+v err=%v", filtered, err)
	}
}
