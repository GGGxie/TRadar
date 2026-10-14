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

func TestProjectIdentityAndConflictingEdits(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	var first, again, other Project
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		var err error
		var created bool
		first, created, err = CreateProject(ctx, tx, CreateProjectInput{Name: "雷达", CanonicalURL: "https://example.com?utm_source=a"}, now)
		if err != nil || !created {
			t.Fatalf("create: %v %v", created, err)
		}
		again, created, err = CreateProject(ctx, tx, CreateProjectInput{Name: "renamed", CanonicalURL: "https://example.com/"}, now)
		if err != nil || created {
			t.Fatalf("duplicate: %v %v", created, err)
		}
		other, _, err = CreateProject(ctx, tx, CreateProjectInput{Name: "雷达", CanonicalURL: "https://example.org"}, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != again.ID || first.ID == other.ID || again.Name != "雷达" {
		t.Fatalf("identity lost: %+v %+v %+v", first, again, other)
	}
	title := "我的雷达"
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, err := UpdateProject(ctx, tx, first.ID, 1, ProjectPatch{Name: &title}, now.Add(time.Hour))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		_, err := UpdateProject(ctx, tx, first.ID, 1, ProjectPatch{Name: &title}, now)
		return err
	})
	var problem *fault.Error
	if !errors.As(err, &problem) || problem.Code != "VERSION_CONFLICT" {
		t.Fatalf("stale update: %v", err)
	}
	got, err := GetProject(ctx, db, first.ID)
	if err != nil || got.Name != title || got.Revision != 2 {
		t.Fatalf("get: %+v %v", got, err)
	}
	list, total, err := ListProjects(ctx, db, ProjectFilter{Query: "雷达", Page: 1, PageSize: 1})
	if err != nil || len(list) != 1 || total != 2 {
		t.Fatalf("list len=%d total=%d err=%v", len(list), total, err)
	}
}
func TestProjectValidation(t *testing.T) {
	for _, input := range []CreateProjectInput{{Name: "", CanonicalURL: "https://example.com"}, {Name: "ok", CanonicalURL: "javascript:x"}, {Name: "ok", CanonicalURL: "https://example.com", Category: "FAKE"}} {
		_, _, err := CreateProject(context.Background(), nil, input, time.Now())
		if err == nil {
			t.Fatal("invalid input accepted")
		}
	}
}
