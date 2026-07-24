DROP TABLE quality_profiles;
DROP INDEX idx_debrid_items_media_item_id;
ALTER TABLE debrid_items DROP COLUMN media_item_id;
ALTER TABLE media_items DROP COLUMN acquisition_error;
ALTER TABLE media_items DROP COLUMN acquisition_status;
