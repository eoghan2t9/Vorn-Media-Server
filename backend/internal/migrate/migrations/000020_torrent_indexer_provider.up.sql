ALTER TABLE torrent_indexers ADD COLUMN provider TEXT NOT NULL DEFAULT 'torznab' CHECK (provider IN ('torznab', 'torbox'));
