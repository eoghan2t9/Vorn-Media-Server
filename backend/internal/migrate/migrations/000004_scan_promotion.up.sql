ALTER TABLE scan_files ADD COLUMN media_item_id UUID REFERENCES media_items(id) ON DELETE SET NULL;

-- Speeds up the find-or-create lookups promotion does for series/season
-- parents and movie/episode identity checks.
CREATE INDEX idx_media_items_lookup ON media_items (library_id, kind, parent_id, title);
