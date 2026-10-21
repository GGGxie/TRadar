CREATE TABLE projects (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 canonical_url TEXT NOT NULL UNIQUE,
 summary TEXT NOT NULL DEFAULT '',
 category TEXT NOT NULL CHECK(category IN ('AI_AGENT','DEVTOOLS','SAAS','OTHER')),
 first_seen_at TEXT NOT NULL,
 last_seen_at TEXT NOT NULL,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>0)
);
CREATE INDEX projects_first_seen ON projects(first_seen_at DESC,id);
CREATE TABLE idempotency (
 scope TEXT NOT NULL,
 key TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 status INTEGER NOT NULL,
 body BLOB NOT NULL,
 PRIMARY KEY(scope,key)
);
