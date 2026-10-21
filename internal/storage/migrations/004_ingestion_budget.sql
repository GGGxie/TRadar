CREATE TABLE request_budget_claims (
 id TEXT PRIMARY KEY,
 source TEXT NOT NULL REFERENCES source_configs(source),
 run_id TEXT NOT NULL REFERENCES ingestion_runs(id),
 request_class TEXT NOT NULL CHECK(request_class IN ('GENERAL','SEARCH')),
 claimed_at TEXT NOT NULL
);
CREATE INDEX request_budget_claims_source_time ON request_budget_claims(source,claimed_at);
CREATE INDEX request_budget_claims_search_time ON request_budget_claims(source,request_class,claimed_at);

CREATE TABLE source_budget_states (
 source TEXT PRIMARY KEY REFERENCES source_configs(source),
 local_remaining_requests INTEGER NOT NULL CHECK(local_remaining_requests >= 0),
 local_reset_at TEXT NOT NULL,
 provider_remaining_requests INTEGER CHECK(provider_remaining_requests >= 0),
 provider_reset_at TEXT,
 provider_observed_at TEXT,
 updated_at TEXT NOT NULL,
 CHECK((provider_remaining_requests IS NULL AND provider_reset_at IS NULL AND provider_observed_at IS NULL)
    OR (provider_remaining_requests IS NOT NULL AND provider_reset_at IS NOT NULL AND provider_observed_at IS NOT NULL))
);
