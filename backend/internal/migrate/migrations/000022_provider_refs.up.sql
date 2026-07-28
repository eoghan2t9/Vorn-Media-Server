ALTER TABLE debrid_items ADD COLUMN provider_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE nzb_downloads ADD COLUMN provider_ref TEXT NOT NULL DEFAULT '';
