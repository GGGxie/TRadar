CREATE TABLE experiments (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id),
 title TEXT NOT NULL,
 hypothesis TEXT NOT NULL,
 method TEXT NOT NULL,
 status TEXT NOT NULL CHECK(status IN ('PLANNED','RUNNING','COMPLETED','CANCELLED')),
 time_budget_minutes INTEGER NOT NULL CHECK(time_budget_minutes BETWEEN 1 AND 10080),
 actual_minutes INTEGER CHECK(actual_minutes IS NULL OR actual_minutes BETWEEN 0 AND 1000000),
 result TEXT NOT NULL,
 outcome TEXT CHECK(outcome IS NULL OR outcome IN ('SUPPORTED','REFUTED','INCONCLUSIVE')),
 evidence_ids_json TEXT NOT NULL CHECK(json_valid(evidence_ids_json) AND json_type(evidence_ids_json) = 'array'),
 artifact_urls_json TEXT NOT NULL CHECK(json_valid(artifact_urls_json) AND json_type(artifact_urls_json) = 'array'),
 next_action TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision > 0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE INDEX experiments_project_updated ON experiments(project_id,updated_at DESC,id);
CREATE INDEX experiments_project_status_updated ON experiments(project_id,status,updated_at DESC,id);
