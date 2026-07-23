CREATE TABLE torrent_indexers (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    base_url   TEXT NOT NULL,
    api_key    TEXT NOT NULL DEFAULT '',
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE torrents (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id   UUID REFERENCES libraries(id) ON DELETE SET NULL,
    info_hash    TEXT NOT NULL UNIQUE,
    name         TEXT NOT NULL,
    magnet_uri   TEXT NOT NULL DEFAULT '',
    save_path    TEXT NOT NULL,
    sequential   BOOLEAN NOT NULL DEFAULT false,
    status       TEXT NOT NULL CHECK (status IN ('downloading', 'seeding', 'completed', 'error', 'removed')) DEFAULT 'downloading',
    bytes_total  BIGINT NOT NULL DEFAULT 0,
    bytes_done   BIGINT NOT NULL DEFAULT 0,
    error        TEXT NOT NULL DEFAULT '',
    promoted     BOOLEAN NOT NULL DEFAULT false,
    added_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX idx_torrents_library_id ON torrents(library_id);
