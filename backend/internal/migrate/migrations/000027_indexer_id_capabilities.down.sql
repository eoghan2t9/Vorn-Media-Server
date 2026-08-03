ALTER TABLE torrent_indexers DROP COLUMN supports_imdb_search;
ALTER TABLE torrent_indexers DROP COLUMN supports_tvdb_search;
ALTER TABLE torrent_indexers DROP COLUMN disabled_reason;

ALTER TABLE nzb_indexers DROP COLUMN supports_imdb_search;
ALTER TABLE nzb_indexers DROP COLUMN supports_tvdb_search;
ALTER TABLE nzb_indexers DROP COLUMN disabled_reason;
