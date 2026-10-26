-- #59：记录符合变化条件但因 maxItems 未收录的项目数；旧版本未记录，按 0 处理。
ALTER TABLE daily_briefs ADD COLUMN omitted_item_count INTEGER NOT NULL DEFAULT 0 CHECK(omitted_item_count >= 0);
