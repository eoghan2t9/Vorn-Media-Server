ALTER TABLE torrent_indexers ADD COLUMN supports_imdb_search BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE torrent_indexers ADD COLUMN supports_tvdb_search BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE torrent_indexers ADD COLUMN disabled_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE nzb_indexers ADD COLUMN supports_imdb_search BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE nzb_indexers ADD COLUMN supports_tvdb_search BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE nzb_indexers ADD COLUMN disabled_reason TEXT NOT NULL DEFAULT '';

-- TorBox-provider torrent indexers speak TorBox's own IMDb-ID-driven search
-- API directly (not Torznab), so they always support id search regardless
-- of what a caps fetch would say -- backfill existing rows so the startup
-- capability sweep (added alongside this migration) doesn't disable them.
UPDATE torrent_indexers SET supports_imdb_search = true, supports_tvdb_search = true WHERE provider = 'torbox';
