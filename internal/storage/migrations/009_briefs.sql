CREATE TABLE brief_settings (
 singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
 revision INTEGER NOT NULL CHECK(revision > 0),
 is_enabled INTEGER NOT NULL CHECK(is_enabled IN (0,1)),
 time_zone TEXT NOT NULL,
 local_time TEXT NOT NULL,
 max_items INTEGER NOT NULL CHECK(max_items BETWEEN 1 AND 20)
);
INSERT INTO brief_settings(singleton,revision,is_enabled,time_zone,local_time,max_items)
VALUES(1,1,1,'Asia/Shanghai','08:30',5);

CREATE TABLE daily_briefs (
 id TEXT PRIMARY KEY,
 date TEXT NOT NULL,
 time_zone TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version > 0),
 cutoff_at TEXT NOT NULL,
 generated_at TEXT NOT NULL,
 previous_brief_id TEXT REFERENCES daily_briefs(id),
 selection_version TEXT NOT NULL,
 ranking_version TEXT NOT NULL,
 is_demo INTEGER NOT NULL CHECK(is_demo IN (0,1)),
 coverage_json TEXT NOT NULL CHECK(json_valid(coverage_json) AND json_type(coverage_json) = 'array'),
 empty_reason TEXT CHECK(empty_reason IS NULL OR empty_reason IN ('NO_DATA','NO_NEW_CHANGES')),
 UNIQUE(date,time_zone,version)
);
CREATE INDEX daily_briefs_list ON daily_briefs(date DESC,version DESC,id);
CREATE INDEX daily_briefs_cutoff ON daily_briefs(time_zone,cutoff_at DESC,id);

CREATE TABLE brief_items (
 brief_id TEXT NOT NULL REFERENCES daily_briefs(id),
 project_id TEXT NOT NULL REFERENCES projects(id),
 ordinal INTEGER NOT NULL CHECK(ordinal >= 0),
 item_json TEXT NOT NULL CHECK(json_valid(item_json) AND json_type(item_json) = 'object'),
 PRIMARY KEY(brief_id,project_id),
 UNIQUE(brief_id,ordinal)
);

CREATE TABLE brief_runs (
 id TEXT PRIMARY KEY,
 status TEXT NOT NULL CHECK(status IN ('PENDING','RUNNING','SUCCEEDED','FAILED','INTERRUPTED')),
 revision INTEGER NOT NULL CHECK(revision > 0),
 date TEXT NOT NULL,
 time_zone TEXT NOT NULL,
 cutoff_at TEXT NOT NULL,
 brief_id TEXT REFERENCES daily_briefs(id),
 error_code TEXT,
 input_json TEXT NOT NULL CHECK(json_valid(input_json) AND json_type(input_json) = 'object'),
 created_at TEXT NOT NULL,
 finished_at TEXT
);
CREATE UNIQUE INDEX brief_runs_active ON brief_runs(date,time_zone) WHERE status IN ('PENDING','RUNNING');
CREATE INDEX brief_runs_date ON brief_runs(date DESC,created_at DESC,id);
