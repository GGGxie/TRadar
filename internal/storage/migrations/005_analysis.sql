CREATE TABLE analyses (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id),
 level TEXT NOT NULL CHECK(level IN ('SCREEN','DEEP')),
 version INTEGER NOT NULL CHECK(version > 0),
 origin TEXT NOT NULL CHECK(origin IN ('MANUAL','RULES','MODEL')),
 project_revision INTEGER NOT NULL CHECK(project_revision > 0),
 based_on_analysis_id TEXT REFERENCES analyses(id),
 created_at TEXT NOT NULL,
 cutoff_at TEXT NOT NULL,
 rules_version TEXT,
 model_json TEXT CHECK(model_json IS NULL OR json_valid(model_json)),
 content_json TEXT NOT NULL CHECK(json_valid(content_json)),
 evidence_ids_json TEXT NOT NULL CHECK(json_valid(evidence_ids_json)),
 evidence_snapshots_json TEXT NOT NULL CHECK(json_valid(evidence_snapshots_json)),
 trend_snapshot_json TEXT CHECK(trend_snapshot_json IS NULL OR json_valid(trend_snapshot_json)),
 UNIQUE(project_id,level,version)
);
CREATE INDEX analyses_project_level_created ON analyses(project_id,level,created_at DESC,id);

CREATE TABLE analysis_references (
 analysis_id TEXT NOT NULL REFERENCES analyses(id),
 section_key TEXT NOT NULL,
 claim_index INTEGER NOT NULL CHECK(claim_index >= 0),
 evidence_id TEXT NOT NULL REFERENCES evidence(id),
 PRIMARY KEY(analysis_id,section_key,claim_index,evidence_id)
);

CREATE TABLE analysis_runs (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id),
 level TEXT NOT NULL CHECK(level IN ('SCREEN','DEEP')),
 mode TEXT NOT NULL CHECK(mode IN ('RULES','MODEL')),
 status TEXT NOT NULL CHECK(status IN ('PENDING','RUNNING','SUCCEEDED','FAILED','INTERRUPTED')),
 revision INTEGER NOT NULL CHECK(revision > 0),
 project_revision INTEGER NOT NULL CHECK(project_revision > 0),
 cutoff_at TEXT NOT NULL,
 input_json TEXT NOT NULL CHECK(json_valid(input_json)),
 analysis_id TEXT REFERENCES analyses(id),
 error_code TEXT,
 usage_json TEXT CHECK(usage_json IS NULL OR json_valid(usage_json)),
 created_at TEXT NOT NULL,
 finished_at TEXT
);
CREATE INDEX analysis_runs_project_created ON analysis_runs(project_id,created_at DESC,id);
