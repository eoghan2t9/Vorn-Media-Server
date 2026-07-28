ALTER TABLE usenet_servers
    ADD COLUMN host TEXT NOT NULL DEFAULT '',
    ADD COLUMN port INT NOT NULL DEFAULT 563,
    ADD COLUMN use_tls BOOLEAN NOT NULL DEFAULT true,
    ADD COLUMN username TEXT NOT NULL DEFAULT '',
    ADD COLUMN password TEXT NOT NULL DEFAULT '',
    ADD COLUMN max_connections INT NOT NULL DEFAULT 4,
    ADD COLUMN provider TEXT NOT NULL DEFAULT 'torbox' CHECK (provider IN ('nntp', 'torbox'));

ALTER TABLE nzb_downloads
    ADD COLUMN save_path TEXT NOT NULL DEFAULT '',
    ADD COLUMN provider TEXT NOT NULL DEFAULT 'torbox';
