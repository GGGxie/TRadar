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

// #60：在搜索框粘贴项目链接（可省略协议、带 /tree/main 或跟踪参数）时按规范化链接匹配。
func TestListProjectsMatchesPastedLinks(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	err = storage.WithTx(ctx, db, func(tx *sql.Tx) error {
		if _, _, err := CreateProject(ctx, tx, CreateProjectInput{Name: "ponytail", CanonicalURL: "https://github.com/DietrichGebert/ponytail"}, now); err != nil {
			return err
		}
		_, _, err := CreateProject(ctx, tx, CreateProjectInput{Name: "other", CanonicalURL: "https://example.com/tool"}, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string]string{
		"github.com/DietrichGebert/ponytail":                   "ponytail",
		"https://github.com/dietrichgebert/ponytail/tree/main": "ponytail",
		"example.com/tool?utm_source=x":                        "other",
		"ponytail":                                             "ponytail",
	} {
		list, total, err := ListProjects(ctx, db, ProjectFilter{Query: query, Page: 1, PageSize: 10})
		if err != nil || total != 1 || len(list) != 1 || list[0].Name != want {
			t.Fatalf("query %q: total=%d list=%+v err=%v", query, total, list, err)
		}
	}
	if _, total, _ := ListProjects(ctx, db, ProjectFilter{Query: "github.com/nobody/none", Page: 1, PageSize: 10}); total != 0 {
		t.Fatalf("unknown link matched %d projects", total)
	}
}
