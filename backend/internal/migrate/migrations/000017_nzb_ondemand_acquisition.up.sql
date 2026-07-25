-- Lets NZB acquisition fulfil a specific existing placeholder media_item,
-- mirroring debrid_items.media_item_id / media_items.active_debrid_item_id
-- (000013_ondemand_acquisition) -- the on-demand acquisition engine can now
-- fall back to a configured Usenet server when no torrent release is found.
ALTER TABLE nzb_downloads ADD COLUMN media_item_id UUID REFERENCES media_items(id) ON DELETE CASCADE;
ALTER TABLE media_items ADD COLUMN active_nzb_download_id UUID REFERENCES nzb_downloads(id) ON DELETE SET NULL;
