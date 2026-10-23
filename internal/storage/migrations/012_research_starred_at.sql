-- #48：记录变为收藏的时刻，供「今日收藏」节奏统计。既有收藏时间未知，保持 NULL，不计入统计。
ALTER TABLE research_entries ADD COLUMN starred_at TEXT;
CREATE INDEX research_entries_starred_at ON research_entries(starred_at);
