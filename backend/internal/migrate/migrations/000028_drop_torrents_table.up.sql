-- Torrents are no longer downloaded locally -- every release is resolved
-- through a debrid provider (debrid_items), which never touches this
-- server's disk. This table tracked local BitTorrent client downloads and
-- has no remaining reader.
DROP TABLE torrents;
