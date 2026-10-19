CREATE TABLE analysis_model_reservations (
 run_id TEXT PRIMARY KEY REFERENCES analysis_runs(id),
 budget_date TEXT NOT NULL,
 provider TEXT NOT NULL,
 model TEXT NOT NULL,
 reserved_tokens INTEGER NOT NULL CHECK(reserved_tokens > 0),
 charged_tokens INTEGER CHECK(charged_tokens IS NULL OR charged_tokens >= 0),
 state TEXT NOT NULL CHECK(state IN ('RESERVED','SETTLED')),
 created_at TEXT NOT NULL,
 settled_at TEXT
);
CREATE INDEX analysis_model_reservations_budget
ON analysis_model_reservations(budget_date,provider,model,state);
