-- Usenet (NZB) acquisition
CREATE TABLE usenet_servers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    host            TEXT NOT NULL,
    port            INT NOT NULL DEFAULT 563,
    use_tls         BOOLEAN NOT NULL DEFAULT true,
    username        TEXT NOT NULL DEFAULT '',
    password        TEXT NOT NULL DEFAULT '',
    max_connections INT NOT NULL DEFAULT 4,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE nzb_downloads (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id   UUID REFERENCES libraries(id) ON DELETE SET NULL,
    name         TEXT NOT NULL,
    save_path    TEXT NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('downloading', 'repairing', 'completed', 'error', 'removed')) DEFAULT 'downloading',
    bytes_total  BIGINT NOT NULL DEFAULT 0,
    bytes_done   BIGINT NOT NULL DEFAULT 0,
    error        TEXT NOT NULL DEFAULT '',
    promoted     BOOLEAN NOT NULL DEFAULT false,
    added_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
CREATE INDEX idx_nzb_downloads_library_id ON nzb_downloads(library_id);

-- Debrid (Real-Debrid / TorBox) acquisition
CREATE TABLE debrid_accounts (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider   TEXT NOT NULL CHECK (provider IN ('realdebrid', 'torbox')),
    api_key    TEXT NOT NULL,
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE debrid_items (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id  UUID REFERENCES libraries(id) ON DELETE SET NULL,
    account_id  UUID NOT NULL REFERENCES debrid_accounts(id) ON DELETE CASCADE,
    source_ref  TEXT NOT NULL,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('resolving', 'ready', 'error', 'removed')) DEFAULT 'resolving',
    error       TEXT NOT NULL DEFAULT '',
    promoted    BOOLEAN NOT NULL DEFAULT false,
    added_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_debrid_items_library_id ON debrid_items(library_id);

CREATE TABLE debrid_files (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    debrid_item_id UUID NOT NULL REFERENCES debrid_items(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    size_bytes     BIGINT NOT NULL DEFAULT 0,
    stream_url     TEXT NOT NULL
);
CREATE INDEX idx_debrid_files_item_id ON debrid_files(debrid_item_id);
