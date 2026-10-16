CREATE TABLE research_entries (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL UNIQUE REFERENCES projects(id),
 is_starred INTEGER NOT NULL CHECK(is_starred IN (0,1)),
 tags_json TEXT NOT NULL CHECK(json_valid(tags_json) AND json_type(tags_json) = 'array'),
 stage TEXT NOT NULL CHECK(stage IN ('SCREENING','DEEP_RESEARCH','PRACTICE','REVIEWED','ARCHIVED')),
 decision TEXT NOT NULL CHECK(decision IN ('UNDECIDED','OBSERVE','LEARN','USE','VALIDATE','PASS')),
 rationale TEXT NOT NULL,
 notes TEXT NOT NULL,
 next_action TEXT NOT NULL,
 next_action_at TEXT,
 revision INTEGER NOT NULL CHECK(revision > 0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL
);
CREATE INDEX research_entries_updated ON research_entries(updated_at DESC,id);
CREATE INDEX research_entries_starred_updated ON research_entries(is_starred,updated_at DESC,id);
CREATE INDEX research_entries_stage_updated ON research_entries(stage,updated_at DESC,id);
