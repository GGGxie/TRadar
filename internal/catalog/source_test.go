package catalog

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"tradar/internal/fault"
	"tradar/internal/storage"
)

func TestStableSourceIdentitySurvivesRepositoryRename(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 8, 0, 0, 0, time.UTC)
	first, second := createSourceProjects(t, db, now)

	var initial, renamed SourceRecord
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		initial, _, err = UpsertSource(ctx, tx, UpsertSourceInput{
			ProjectID: first.ID, Source: "GITHUB", ExternalID: "repo:4242", RecordType: "PROJECT",
			URL: "https://api.github.com/repositories/4242", TargetURL: stringPointer("https://github.com/acme/old-name"),
			Title: "acme/old-name", Excerpt: "First description", FetchedAt: now,
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		renamed, _, err = UpsertSource(ctx, tx, UpsertSourceInput{
			ProjectID: second.ID, Source: "GITHUB", ExternalID: "repo:4242", RecordType: "PROJECT",
			URL: "https://api.github.com/repositories/4242", TargetURL: stringPointer("https://github.com/acme/new-name"),
			Title: "acme/new-name", Excerpt: "Renamed repository", FetchedAt: now.Add(time.Hour),
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if initial.ID != renamed.ID || renamed.ProjectID != first.ID || renamed.Revision != 2 || renamed.Title != "acme/new-name" {
		t.Fatalf("stable source identity lost: initial=%+v renamed=%+v", initial, renamed)
	}
	updatedProject, err := GetProject(ctx, db, first.ID)
	if err != nil || updatedProject.LastSeenAt != Timestamp(now.Add(time.Hour)) {
		t.Fatalf("project last seen=%q err=%v", updatedProject.LastSeenAt, err)
	}
	var aliasOwner string
	if err = db.QueryRow(`SELECT project_id FROM project_aliases WHERE normalized_url=?`, "https://github.com/acme/new-name").Scan(&aliasOwner); err != nil || aliasOwner != first.ID {
		t.Fatalf("renamed alias owner=%q err=%v", aliasOwner, err)
	}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, _, err := UpsertSource(ctx, tx, UpsertSourceInput{
			ProjectID: second.ID, Source: "GITHUB", ExternalID: "repo:9999", RecordType: "PROJECT",
			URL: "https://api.github.com/repositories/9999", TargetURL: stringPointer("https://github.com/acme/new-name"),
			Title: "conflicting/repository", FetchedAt: now.Add(2 * time.Hour),
		})
		return err
	})
	var problem *fault.Error
	if !errors.As(err, &problem) || problem.Code != "VALIDATION_ERROR" {
		t.Fatalf("alias conflict accepted: %v", err)
	}
	var conflicting int
	if err = db.QueryRow(`SELECT count(*) FROM source_records WHERE external_id='repo:9999'`).Scan(&conflicting); err != nil || conflicting != 0 {
		t.Fatalf("conflicting source partially committed: count=%d err=%v", conflicting, err)
	}
}

func TestObservationRetriesAndDailyBucketsRemainDistinct(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	project, _ := createSourceProjects(t, db, now)
	var source SourceRecord
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		source, _, err = UpsertSource(ctx, tx, UpsertSourceInput{
			ProjectID: project.ID, Source: "GITHUB", ExternalID: "repo:7", RecordType: "PROJECT",
			URL: "https://api.github.com/repositories/7", TargetURL: stringPointer("https://github.com/acme/signal"),
			Title: "acme/signal", FetchedAt: now,
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	cumulative := ObservationInput{Metric: "STARS", Basis: "CUMULATIVE", Value: 100, ObservedAt: now, FetchedAt: now}
	var firstWrite, retry, nextRun []Observation
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		firstWrite, err = AppendObservations(ctx, tx, "run-1", source.ID, []ObservationInput{cumulative})
		if err != nil {
			return err
		}
		retry, err = AppendObservations(ctx, tx, "run-1", source.ID, []ObservationInput{cumulative})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		nextRun, err = AppendObservations(ctx, tx, "run-2", source.ID, []ObservationInput{cumulative})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstWrite) != 1 || len(retry) != 1 || len(nextRun) != 1 || firstWrite[0].ID != retry[0].ID || firstWrite[0].ID == nextRun[0].ID {
		t.Fatalf("retry identity: first=%+v retry=%+v next=%+v", firstWrite, retry, nextRun)
	}
	period := &ProviderPeriod{WeekUnix: 1791763200, DayIndex: 2, TimeZone: "UNSPECIFIED"}
	for index, value := range []int64{5, 7} {
		err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
			_, err := AppendObservations(ctx, tx, "daily-"+string(rune('a'+index)), source.ID, []ObservationInput{{
				Metric: "STARS", Basis: "SOURCE_DAILY", PeriodKey: "1791763200:2", Value: value,
				ObservedAt: now.Add(time.Duration(index+1) * time.Hour), FetchedAt: now.Add(time.Duration(index+1) * time.Hour), ProviderPeriod: period,
			}})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	from, to := now.Add(-time.Hour), now.Add(24*time.Hour)
	items, total, err := ListObservations(ctx, db, project.ID, ObservationFilter{Basis: "SOURCE_DAILY", From: from, To: to, Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(items) != 1 || items[0].Value != 7 {
		t.Fatalf("daily latest: total=%d items=%+v err=%v", total, items, err)
	}
	mismatch := cumulative
	mismatch.Value = 101
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, err := AppendObservations(ctx, tx, "run-1", source.ID, []ObservationInput{mismatch})
		return err
	})
	var problem *fault.Error
	if !errors.As(err, &problem) || problem.Code != "IDEMPOTENCY_MISMATCH" {
		t.Fatalf("mismatched retry accepted: %v", err)
	}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		empty, err := AppendObservations(ctx, tx, "run-empty", source.ID, nil)
		if err != nil || len(empty) != 0 {
			t.Fatalf("empty observation set: %+v %v", empty, err)
		}
		_, err = AppendObservations(ctx, tx, "run-partial", source.ID, []ObservationInput{
			{Metric: "FORKS", Basis: "CUMULATIVE", Value: 1, ObservedAt: now, FetchedAt: now},
			{Metric: "COMMENTS", Basis: "CUMULATIVE", Value: -1, ObservedAt: now, FetchedAt: now},
		})
		return err
	})
	if err == nil {
		t.Fatal("negative observation accepted")
	}
	var partial int
	if err = db.QueryRow(`SELECT count(*) FROM observations WHERE run_id='run-partial'`).Scan(&partial); err != nil || partial != 0 {
		t.Fatalf("partial observations committed: count=%d err=%v", partial, err)
	}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		transient, _, err := UpsertSource(ctx, tx, UpsertSourceInput{
			ProjectID: project.ID, Source: "GITHUB", ExternalID: "repo:rollback", RecordType: "PROJECT",
			URL: "https://api.github.com/repositories/88", TargetURL: stringPointer("https://github.com/acme/rollback"),
			Title: "acme/rollback", FetchedAt: now,
		})
		if err != nil {
			return err
		}
		_, err = AppendObservations(ctx, tx, "run-rollback", transient.ID, []ObservationInput{{
			Metric: "STARS", Basis: "CUMULATIVE", Value: -1, ObservedAt: now, FetchedAt: now,
		}})
		return err
	})
	if err == nil {
		t.Fatal("source and invalid observation transaction succeeded")
	}
	var transient int
	if err = db.QueryRow(`SELECT count(*) FROM source_records WHERE external_id='repo:rollback'`).Scan(&transient); err != nil || transient != 0 {
		t.Fatalf("source survived failed observation: count=%d err=%v", transient, err)
	}
}

func TestReassociationAuditsWithoutMovingEvidenceOrObservations(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	first, second := createSourceProjects(t, db, now)
	var source SourceRecord
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		source, _, err = UpsertSource(ctx, tx, UpsertSourceInput{
			ProjectID: first.ID, Source: "HACKER_NEWS", ExternalID: "item:99", RecordType: "DISCUSSION",
			URL: "https://news.ycombinator.com/item?id=99", Title: "Show HN: Signal", FetchedAt: now,
		})
		if err != nil {
			return err
		}
		_, err = AppendObservations(ctx, tx, "run-hn", source.ID, []ObservationInput{{Metric: "POINTS", Basis: "CUMULATIVE", Value: 12, ObservedAt: now, FetchedAt: now}})
		if err != nil {
			return err
		}
		_, err = CreateEvidence(ctx, tx, first.ID, CreateEvidenceInput{
			URL: source.URL, Title: "Discussion", Excerpt: "Early comments", Kind: "PUBLIC_EXCERPT", SourceRecordID: &source.ID,
		}, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		source, err = ReassociateSource(ctx, tx, source.ID, 1, second.ID, "链接实际属于第二个项目", now.Add(time.Hour))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if source.ProjectID != second.ID || source.Revision != 2 {
		t.Fatalf("source not reassociated: %+v", source)
	}
	var auditCount int
	if err = db.QueryRow(`SELECT count(*) FROM association_events WHERE source_record_id=? AND old_project_id=? AND new_project_id=?`, source.ID, first.ID, second.ID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("audit count=%d err=%v", auditCount, err)
	}
	evidence, total, err := ListEvidence(ctx, db, first.ID, EvidenceFilter{Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(evidence) != 1 || evidence[0].ProjectID != first.ID {
		t.Fatalf("evidence moved: total=%d evidence=%+v err=%v", total, evidence, err)
	}
	from, to := now.Add(-time.Hour), now.Add(time.Hour)
	observations, total, err := ListObservations(ctx, db, second.ID, ObservationFilter{From: from, To: to, Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(observations) != 1 || observations[0].SourceRecordID != source.ID {
		t.Fatalf("observations lost: total=%d observations=%+v err=%v", total, observations, err)
	}
}

func TestTrackedSourcesPreferNeverAndLeastRecentlyObserved(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 15, 1, 0, 0, 0, time.UTC)
	project, _ := createSourceProjects(t, db, now)
	sources := make([]SourceRecord, 3)
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		for index := range sources {
			target := "https://github.com/acme/tracked-" + string(rune('a'+index))
			var writeErr error
			sources[index], _, writeErr = UpsertSource(ctx, tx, UpsertSourceInput{
				ProjectID: project.ID, Source: "GITHUB", ExternalID: string(rune('1' + index)), RecordType: "PROJECT",
				URL: "https://api.github.com/repositories/" + string(rune('1'+index)), TargetURL: &target,
				Title: "acme/tracked-" + string(rune('a'+index)), FetchedAt: now,
			})
			if writeErr != nil {
				return writeErr
			}
		}
		for index, observedAt := range []time.Time{now.Add(-time.Hour), now} {
			if _, writeErr := AppendObservations(ctx, tx, "age-"+string(rune('a'+index)), sources[index].ID, []ObservationInput{{Metric: "STARS", Basis: "CUMULATIVE", Value: int64(index + 1), ObservedAt: observedAt, FetchedAt: observedAt}}); writeErr != nil {
				return writeErr
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ordered, err := ListSourcesByObservationAge(ctx, db, "GITHUB", []string{sources[1].ID, sources[2].ID, sources[0].ID}, "CUMULATIVE", 2)
	if err != nil || len(ordered) != 2 || ordered[0].ID != sources[2].ID || ordered[1].ID != sources[0].ID {
		t.Fatalf("tracked order: %+v err=%v", ordered, err)
	}
}

func createSourceProjects(t *testing.T, db *sql.DB, now time.Time) (Project, Project) {
	t.Helper()
	ctx := context.Background()
	var first, second Project
	err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		first, _, err = CreateProject(ctx, tx, CreateProjectInput{Name: "First", CanonicalURL: "https://first.test"}, now)
		if err != nil {
			return err
		}
		second, _, err = CreateProject(ctx, tx, CreateProjectInput{Name: "Second", CanonicalURL: "https://second.test"}, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return first, second
}

func stringPointer(value string) *string { return &value }
