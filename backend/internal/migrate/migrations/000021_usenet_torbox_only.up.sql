ALTER TABLE usenet_servers
    DROP COLUMN host,
    DROP COLUMN port,
    DROP COLUMN use_tls,
    DROP COLUMN username,
    DROP COLUMN password,
    DROP COLUMN max_connections,
    DROP COLUMN provider;

ALTER TABLE nzb_downloads
    DROP COLUMN save_path,
    DROP COLUMN provider;
