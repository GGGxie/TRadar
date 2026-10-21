package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenMigratesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "radar.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO projects(id,name,canonical_url,summary,category,first_seen_at,last_seen_at,created_at,updated_at,revision) VALUES('p','Radar','https://example.com/','','OTHER','2026-10-14T00:00:00Z','2026-10-14T00:00:00Z','2026-10-14T00:00:00Z','2026-10-14T00:00:00Z',1)`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var name string
	if err = db.QueryRow(`SELECT name FROM projects WHERE id='p'`).Scan(&name); err != nil || name != "Radar" {
		t.Fatalf("name=%q err=%v", name, err)
	}
}

func TestTransactionRollsBack(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "radar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	want := sql.ErrNoRows
	err = WithTx(context.Background(), db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO idempotency(scope,key,request_hash,status,body) VALUES('test','k','h',201,'{}')`); err != nil {
			return err
		}
		return want
	})
	if err != want {
		t.Fatal(err)
	}
	var count int
	db.QueryRow(`SELECT count(*) FROM idempotency`).Scan(&count)
	if count != 0 {
		t.Fatalf("partial write: %d", count)
	}
}

func TestNewerSchemaIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "radar.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`PRAGMA user_version=999`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err == nil {
		db.Close()
		t.Fatal("opened unsupported schema")
	}
}
