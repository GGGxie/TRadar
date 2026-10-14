CREATE TABLE project_aliases (
 normalized_url TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id),
 created_at TEXT NOT NULL
);

CREATE TABLE source_records (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id),
 source TEXT NOT NULL CHECK(source IN ('GITHUB','HACKER_NEWS','PRODUCT_HUNT','MANUAL')),
 external_id TEXT NOT NULL,
 record_type TEXT NOT NULL CHECK(record_type IN ('PROJECT','DISCUSSION','LAUNCH','STORY')),
 url TEXT NOT NULL,
 target_url TEXT,
 title TEXT NOT NULL,
 excerpt TEXT NOT NULL DEFAULT '',
 published_at TEXT,
 fetched_at TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision > 0),
 UNIQUE(source, external_id)
);
CREATE INDEX source_records_project ON source_records(project_id, source, id);

CREATE TABLE observations (
 id TEXT PRIMARY KEY,
 source_record_id TEXT NOT NULL REFERENCES source_records(id),
 run_id TEXT NOT NULL,
 metric TEXT NOT NULL CHECK(metric IN ('STARS','FORKS','POINTS','COMMENTS','VOTES')),
 basis TEXT NOT NULL CHECK(basis IN ('CUMULATIVE','SOURCE_DAILY')),
 period_key TEXT NOT NULL DEFAULT '',
 value INTEGER NOT NULL CHECK(value >= 0),
 observed_at TEXT NOT NULL,
 fetched_at TEXT NOT NULL,
 provider_period TEXT,
 UNIQUE(run_id, source_record_id, metric, basis, period_key),
 CHECK(
   (basis = 'CUMULATIVE' AND period_key = '' AND provider_period IS NULL) OR
   (basis = 'SOURCE_DAILY' AND period_key <> '' AND provider_period IS NOT NULL)
 )
);
CREATE INDEX observations_source_time ON observations(source_record_id, observed_at, id);

CREATE TABLE evidence (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id),
 source_record_id TEXT REFERENCES source_records(id),
 url TEXT NOT NULL,
 title TEXT NOT NULL,
 excerpt TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('METRIC_OBSERVATION','OFFICIAL_DESCRIPTION','OFFICIAL_PRICING','FOUNDER_CLAIM','THIRD_PARTY_ESTIMATE','PUBLIC_EXCERPT')),
 publisher TEXT,
 published_at TEXT,
 collected_at TEXT NOT NULL,
 is_public INTEGER NOT NULL DEFAULT 0 CHECK(is_public IN (0, 1)),
 supersedes_id TEXT UNIQUE REFERENCES evidence(id)
);
CREATE INDEX evidence_project_time ON evidence(project_id, collected_at DESC, id);

CREATE TABLE evidence_observations (
 evidence_id TEXT NOT NULL REFERENCES evidence(id),
 observation_id TEXT NOT NULL REFERENCES observations(id),
 PRIMARY KEY(evidence_id, observation_id)
);

CREATE TABLE association_events (
 id TEXT PRIMARY KEY,
 source_record_id TEXT NOT NULL REFERENCES source_records(id),
 old_project_id TEXT NOT NULL REFERENCES projects(id),
 new_project_id TEXT NOT NULL REFERENCES projects(id),
 reason TEXT NOT NULL,
 changed_at TEXT NOT NULL
);
CREATE INDEX association_events_source_time ON association_events(source_record_id, changed_at, id);
