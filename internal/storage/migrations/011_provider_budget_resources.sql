-- #44：平台配额按资源池分别记录（GitHub search 与 core 是独立额度）。
-- source_budget_states 中旧的 provider_* 列不再使用；旧值无法判断属于哪个池，且一小时内即过期，因此清空。
CREATE TABLE provider_budget_states (
 source TEXT NOT NULL REFERENCES source_configs(source),
 resource TEXT NOT NULL CHECK(length(resource) BETWEEN 1 AND 40),
 remaining_requests INTEGER NOT NULL CHECK(remaining_requests >= 0),
 reset_at TEXT NOT NULL,
 observed_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 PRIMARY KEY(source, resource)
);
UPDATE source_budget_states SET provider_remaining_requests=NULL, provider_reset_at=NULL, provider_observed_at=NULL;
