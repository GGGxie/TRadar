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

func TestEvidenceVersionsStayImmutableAndRespectHistory(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 4, 0, 0, 0, time.UTC)
	var project Project
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		project, _, err = CreateProject(ctx, tx, CreateProjectInput{Name: "Signal", CanonicalURL: "https://signal.test"}, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	var original, revision Evidence
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		original, err = CreateEvidence(ctx, tx, project.ID, CreateEvidenceInput{
			URL: "https://signal.test/launch?utm_source=manual", Title: "创始人发布",
			Excerpt: "创始人称产品已有付费用户。", Kind: "FOUNDER_CLAIM",
		}, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if original.IsPublic || original.Kind != "FOUNDER_CLAIM" || original.URL != "https://signal.test/launch" {
		t.Fatalf("unexpected original evidence: %+v", original)
	}
	public := true
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		revision, err = CreateEvidence(ctx, tx, project.ID, CreateEvidenceInput{
			URL: "https://signal.test/launch", Title: "创始人发布（补充期间）",
			Excerpt: "创始人称本月已有付费用户。", Kind: "FOUNDER_CLAIM",
			IsPublic: &public, SupersedesID: &original.ID,
		}, now.Add(time.Hour))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision.SupersedesID == nil || *revision.SupersedesID != original.ID {
		t.Fatalf("revision chain missing: %+v", revision)
	}

	asOf := now.Add(30 * time.Minute)
	historical, total, err := ListEvidence(ctx, db, project.ID, EvidenceFilter{AsOf: &asOf, Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(historical) != 1 || historical[0].ID != original.ID {
		t.Fatalf("historical evidence: total=%d items=%+v err=%v", total, historical, err)
	}
	current, total, err := ListEvidence(ctx, db, project.ID, EvidenceFilter{Page: 1, PageSize: 20})
	if err != nil || total != 2 || len(current) != 2 || current[0].ID != revision.ID || current[1].Title != original.Title {
		t.Fatalf("immutable versions: total=%d items=%+v err=%v", total, current, err)
	}
	publicOnly, total, err := ListEvidence(ctx, db, project.ID, EvidenceFilter{PublicOnly: true, Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(publicOnly) != 1 || publicOnly[0].ID != revision.ID {
		t.Fatalf("public filter: total=%d items=%+v err=%v", total, publicOnly, err)
	}
}

func TestCurrentEvidenceExcludesSupersededVersionsAsOfCutoff(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 23, 4, 0, 0, 0, time.UTC)
	var project Project
	var original, revised Evidence
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		if project, _, err = CreateProject(ctx, tx, CreateProjectInput{Name: "Versions", CanonicalURL: "https://versions.test"}, now); err != nil {
			return err
		}
		if original, err = CreateEvidence(ctx, tx, project.ID, CreateEvidenceInput{URL: "https://versions.test/a", Title: "原文", Excerpt: "第一版", Kind: "PUBLIC_EXCERPT"}, now); err != nil {
			return err
		}
		revised, err = CreateEvidence(ctx, tx, project.ID, CreateEvidenceInput{URL: "https://versions.test/a", Title: "原文", Excerpt: "第二版", Kind: "PUBLIC_EXCERPT", SupersedesID: &original.ID}, now.Add(time.Hour))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	current, total, err := ListEvidence(ctx, db, project.ID, EvidenceFilter{CurrentOnly: true, Page: 1, PageSize: 20})
	if err != nil || total != 1 || len(current) != 1 || current[0].ID != revised.ID {
		t.Fatalf("current=%+v total=%d err=%v", current, total, err)
	}
	// 修订发生之前的截止时间：旧版仍是当时的当前版本。
	before := now.Add(30 * time.Minute)
	historical, _, err := ListEvidence(ctx, db, project.ID, EvidenceFilter{CurrentOnly: true, AsOf: &before, Page: 1, PageSize: 20})
	if err != nil || len(historical) != 1 || historical[0].ID != original.ID {
		t.Fatalf("historical current=%+v err=%v", historical, err)
	}
	all, total, err := ListEvidence(ctx, db, project.ID, EvidenceFilter{Page: 1, PageSize: 20})
	if err != nil || total != 2 || len(all) != 2 {
		t.Fatalf("all versions=%d err=%v", total, err)
	}
	// 已有新版本的证据不能再分叉修订。
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, err := CreateEvidence(ctx, tx, project.ID, CreateEvidenceInput{URL: "https://versions.test/a", Title: "原文", Excerpt: "分叉", Kind: "PUBLIC_EXCERPT", SupersedesID: &original.ID}, now.Add(2*time.Hour))
		return err
	})
	var problem *fault.Error
	if !errors.As(err, &problem) || problem.Fields["supersedesId"] == "" {
		t.Fatalf("branching revision accepted: %v", err)
	}
}

func TestEvidenceRejectsCrossProjectAndSyntheticMetricClaims(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 5, 0, 0, 0, time.UTC)
	var first, second Project
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		first, _, err = CreateProject(ctx, tx, CreateProjectInput{Name: "One", CanonicalURL: "https://one.test"}, now)
		if err != nil {
			return err
		}
		second, _, err = CreateProject(ctx, tx, CreateProjectInput{Name: "Two", CanonicalURL: "https://two.test"}, now)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO source_records(id,project_id,source,external_id,record_type,url,title,fetched_at,revision) VALUES(?,?,?,?,?,?,?,?,?)`,
			"source-one", first.ID, "MANUAL", "manual:one", "PROJECT", "https://one.test", "One", Timestamp(now), 1)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	sourceID := "source-one"
	var original Evidence
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		original, err = CreateEvidence(ctx, tx, first.ID, CreateEvidenceInput{URL: "https://one.test/about", Title: "About", Excerpt: "Official description", Kind: "OFFICIAL_DESCRIPTION"}, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	for name, tc := range map[string]struct {
		projectID string
		input     CreateEvidenceInput
		manual    bool
	}{
		"cross project revision": {
			projectID: second.ID,
			input:     CreateEvidenceInput{URL: "https://two.test/about", Title: "Wrong", Excerpt: "Wrong project", Kind: "PUBLIC_EXCERPT", SupersedesID: &original.ID},
		},
		"manual metric evidence": {
			projectID: first.ID,
			input:     CreateEvidenceInput{URL: "https://one.test/stars", Title: "Stars", Excerpt: "999", Kind: "METRIC_OBSERVATION"},
			manual:    true,
		},
		"cross project source": {
			projectID: second.ID,
			input:     CreateEvidenceInput{URL: "https://one.test/about", Title: "Wrong source", Excerpt: "Wrong project source", Kind: "PUBLIC_EXCERPT", SourceRecordID: &sourceID},
		},
	} {
		err := storage.WithTx(ctx, db, func(tx *sql.Tx) error {
			var err error
			if tc.manual {
				_, err = CreateManualEvidence(ctx, tx, tc.projectID, tc.input, now.Add(time.Hour))
			} else {
				_, err = CreateEvidence(ctx, tx, tc.projectID, tc.input, now.Add(time.Hour))
			}
			return err
		})
		var problem *fault.Error
		if !errors.As(err, &problem) || problem.Code != "VALIDATION_ERROR" {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
}
