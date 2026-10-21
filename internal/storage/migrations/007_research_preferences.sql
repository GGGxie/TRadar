CREATE TABLE research_preferences (
 singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
 revision INTEGER NOT NULL CHECK(revision > 0),
 focus_topics_json TEXT NOT NULL CHECK(json_valid(focus_topics_json) AND json_type(focus_topics_json) = 'array'),
 daily_save_target INTEGER NOT NULL CHECK(daily_save_target BETWEEN 0 AND 100),
 weekly_deep_target INTEGER NOT NULL CHECK(weekly_deep_target BETWEEN 0 AND 100),
 monthly_practice_target INTEGER NOT NULL CHECK(monthly_practice_target BETWEEN 0 AND 100)
);
INSERT INTO research_preferences(singleton,revision,focus_topics_json,daily_save_target,weekly_deep_target,monthly_practice_target)
VALUES(1,1,'["AI Agent","开发者工具","SaaS"]',3,2,1);
