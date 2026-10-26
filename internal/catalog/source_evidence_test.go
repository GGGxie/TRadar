package catalog

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tradar/internal/storage"
)

func TestDeriveSourceEvidenceFreezesDescriptionsAndLatestMetricsOnce(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 23, 3, 0, 0, 0, time.UTC)
	var project Project
	var github, hn SourceRecord
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		project, _, err = CreateProject(ctx, tx, CreateProjectInput{Name: "acme/ponytail", CanonicalURL: "https://github.com/acme/ponytail"}, now)
		if err != nil {
			return err
		}
		target := "https://github.com/acme/ponytail"
		published := "2026-06-12T00:52:00Z"
		github, _, err = UpsertSource(ctx, tx, UpsertSourceInput{
			ProjectID: project.ID, Source: "GITHUB", ExternalID: "42", RecordType: "PROJECT",
			URL: "https://api.github.com/repositories/42", TargetURL: &target, Title: "acme/ponytail",
			Excerpt: "Makes your AI agent lazy.", PublishedAt: &published, FetchedAt: now,
		})
		if err != nil {
			return err
		}
		hn, _, err = UpsertSource(ctx, tx, UpsertSourceInput{
			ProjectID: project.ID, Source: "HACKER_NEWS", ExternalID: "900", RecordType: "DISCUSSION",
			URL: "https://news.ycombinator.com/item?id=900", TargetURL: &target, Title: "Show HN: Ponytail", FetchedAt: now,
		})
		if err != nil {
			return err
		}
		if _, err = AppendObservations(ctx, tx, "run-old", github.ID, []ObservationInput{
			{Metric: "STARS", Basis: "CUMULATIVE", Value: 100, ObservedAt: now.Add(-time.Hour), FetchedAt: now.Add(-time.Hour)},
		}); err != nil {
			return err
		}
		if _, err = AppendObservations(ctx, tx, "run-new", github.ID, []ObservationInput{
			{Metric: "STARS", Basis: "CUMULATIVE", Value: 173618, ObservedAt: now, FetchedAt: now},
			{Metric: "FORKS", Basis: "CUMULATIVE", Value: 9452, ObservedAt: now, FetchedAt: now},
		}); err != nil {
			return err
		}
		_, err = AppendObservations(ctx, tx, "run-new", hn.ID, []ObservationInput{
			{Metric: "POINTS", Basis: "CUMULATIVE", Value: 412, ObservedAt: now, FetchedAt: now},
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	var first DerivedEvidence
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var deriveErr error
		first, deriveErr = DeriveSourceEvidence(ctx, tx, project.ID, now.Add(time.Minute))
		return deriveErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Created != 4 || len(first.Evidence) != 4 {
		t.Fatalf("first derive created=%d evidence=%+v", first.Created, first.Evidence)
	}
	byKind := map[string][]Evidence{}
	for _, item := range first.Evidence {
		if !item.IsPublic || item.SourceRecordID == nil {
			t.Fatalf("derived evidence must be public and keep its source: %+v", item)
		}
		byKind[item.Kind] = append(byKind[item.Kind], item)
	}
	description := byKind["OFFICIAL_DESCRIPTION"]
	if len(description) != 1 || description[0].Excerpt != "Makes your AI agent lazy." || description[0].URL != "https://github.com/acme/ponytail" || *description[0].SourceRecordID != github.ID {
		t.Fatalf("github description=%+v", description)
	}
	excerpt := byKind["PUBLIC_EXCERPT"]
	if len(excerpt) != 1 || excerpt[0].Excerpt != "Show HN: Ponytail" || excerpt[0].URL != "https://news.ycombinator.com/item?id=900" {
		t.Fatalf("hn title evidence=%+v", excerpt)
	}
	metrics := byKind["METRIC_OBSERVATION"]
	if len(metrics) != 2 {
		t.Fatalf("metric evidence=%+v", metrics)
	}
	for _, item := range metrics {
		if *item.SourceRecordID == github.ID {
			// 只引用最新一次读取，旧的 100 星不进入证据。
			if len(item.ObservationIDs) != 2 || !strings.Contains(item.Excerpt, "Stars 173,618") || !strings.Contains(item.Excerpt, "Forks 9,452") || strings.Contains(item.Excerpt, "100") {
				t.Fatalf("github metric evidence=%+v", item)
			}
		} else if len(item.ObservationIDs) != 1 || !strings.Contains(item.Excerpt, "分数 412") {
			t.Fatalf("hn metric evidence=%+v", item)
		}
	}

	// 同样内容再次派生不新增；返回的是已有证据。
	var second DerivedEvidence
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var deriveErr error
		second, deriveErr = DeriveSourceEvidence(ctx, tx, project.ID, now.Add(2*time.Minute))
		return deriveErr
	})
	if err != nil || second.Created != 0 || len(second.Evidence) != 4 {
		t.Fatalf("second derive=%+v err=%v", second, err)
	}
	var total int
	if err = db.QueryRow(`SELECT count(*) FROM evidence WHERE project_id=?`, project.ID).Scan(&total); err != nil || total != 4 {
		t.Fatalf("evidence rows=%d err=%v", total, err)
	}
}

func TestDeriveSourceEvidenceWithoutSourcesCreatesNothing(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 23, 3, 0, 0, 0, time.UTC)
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		project, _, err := CreateProject(ctx, tx, CreateProjectInput{Name: "manual", CanonicalURL: "https://manual.test"}, now)
		if err != nil {
			return err
		}
		derived, err := DeriveSourceEvidence(ctx, tx, project.ID, now)
		if err == nil && (derived.Created != 0 || len(derived.Evidence) != 0 || derived.Evidence == nil) {
			t.Errorf("derived=%+v", derived)
		}
		_, missingErr := DeriveSourceEvidence(ctx, tx, "missing", now)
		if missingErr == nil {
			t.Error("missing project accepted")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
