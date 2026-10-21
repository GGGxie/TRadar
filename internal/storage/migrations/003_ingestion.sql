CREATE TABLE source_configs (
 source TEXT PRIMARY KEY CHECK(source IN ('GITHUB','HACKER_NEWS','PRODUCT_HUNT')),
 config_json TEXT NOT NULL CHECK(json_valid(config_json)),
 revision INTEGER NOT NULL CHECK(revision > 0),
 next_scheduled_at TEXT,
 next_retry_at TEXT,
 updated_at TEXT NOT NULL
);

INSERT INTO source_configs(source,config_json,revision,updated_at) VALUES
 ('GITHUB','{"source":"GITHUB","isEnabled":true,"isScheduleEnabled":false,"intervalMinutes":60,"keywords":["agent","developer tools","saas"],"discoveryDays":180,"candidatesPerKeyword":10,"trackedSourceRecordIds":[],"requestBudgetPerHour":60}',1,'1970-01-01T00:00:00.000000000Z'),
 ('HACKER_NEWS','{"source":"HACKER_NEWS","isEnabled":true,"isScheduleEnabled":false,"intervalMinutes":60,"feeds":["TOP","NEW","SHOW"],"itemsPerFeed":20,"trackedSourceRecordIds":[]}',1,'1970-01-01T00:00:00.000000000Z'),
 ('PRODUCT_HUNT','{"source":"PRODUCT_HUNT","isEnabled":false,"isScheduleEnabled":false,"intervalMinutes":60,"discoveryDays":7,"maxPages":3}',1,'1970-01-01T00:00:00.000000000Z');

CREATE TABLE ingestion_runs (
 id TEXT PRIMARY KEY,
 source TEXT NOT NULL REFERENCES source_configs(source),
 trigger TEXT NOT NULL CHECK(trigger IN ('MANUAL','SCHEDULED')),
 status TEXT NOT NULL CHECK(status IN ('QUEUED','RUNNING','RETRY_WAIT','SUCCEEDED','SUCCEEDED_EMPTY','PARTIAL','FAILED','INTERRUPTED')),
 config_revision INTEGER NOT NULL CHECK(config_revision > 0),
 config_json TEXT NOT NULL CHECK(json_valid(config_json)),
 created_at TEXT NOT NULL,
 started_at TEXT,
 finished_at TEXT,
 window_start TEXT,
 window_end TEXT NOT NULL,
 fetched_items INTEGER NOT NULL DEFAULT 0 CHECK(fetched_items >= 0),
 accepted_items INTEGER NOT NULL DEFAULT 0 CHECK(accepted_items >= 0),
 skipped_items INTEGER NOT NULL DEFAULT 0 CHECK(skipped_items >= 0),
 failed_items INTEGER NOT NULL DEFAULT 0 CHECK(failed_items >= 0),
 observations_written INTEGER NOT NULL DEFAULT 0 CHECK(observations_written >= 0),
 requests_made INTEGER NOT NULL DEFAULT 0 CHECK(requests_made >= 0),
 is_truncated INTEGER NOT NULL DEFAULT 0 CHECK(is_truncated IN (0,1)),
 next_retry_at TEXT,
 errors_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(errors_json))
);
CREATE INDEX ingestion_runs_created ON ingestion_runs(created_at DESC,id DESC);
CREATE UNIQUE INDEX ingestion_runs_one_active_source ON ingestion_runs(source)
 WHERE status IN ('QUEUED','RUNNING','RETRY_WAIT');

CREATE TABLE ingestion_checkpoints (
 run_id TEXT PRIMARY KEY REFERENCES ingestion_runs(id),
 batch_key TEXT NOT NULL,
 checkpoint_json TEXT NOT NULL CHECK(json_valid(checkpoint_json)),
 updated_at TEXT NOT NULL
);

CREATE TABLE ingestion_batches (
 run_id TEXT NOT NULL REFERENCES ingestion_runs(id),
 batch_key TEXT NOT NULL,
 checkpoint_json TEXT NOT NULL CHECK(json_valid(checkpoint_json)),
 fetched_items INTEGER NOT NULL CHECK(fetched_items >= 0),
 accepted_items INTEGER NOT NULL CHECK(accepted_items >= 0),
 skipped_items INTEGER NOT NULL CHECK(skipped_items >= 0),
 failed_items INTEGER NOT NULL CHECK(failed_items >= 0),
 observations_written INTEGER NOT NULL CHECK(observations_written >= 0),
 committed_at TEXT NOT NULL,
 PRIMARY KEY(run_id,batch_key)
);
