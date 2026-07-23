DROP INDEX IF EXISTS idx_media_items_lookup;
ALTER TABLE scan_files DROP COLUMN IF EXISTS media_item_id;
