CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE server_settings (
    key         TEXT PRIMARY KEY,
    value       JSONB NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    is_admin      BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);

CREATE TABLE libraries (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    type       TEXT NOT NULL CHECK (type IN ('movie', 'series')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE library_folders (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    path       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (library_id, path)
);

CREATE TABLE library_permissions (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, library_id)
);

-- Movies, series, seasons, and episodes all live in media_items, related via parent_id:
-- movie: parent_id NULL. series: parent_id NULL. season: parent_id = series.id.
-- episode: parent_id = season.id.
CREATE TABLE media_items (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id     UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    parent_id      UUID REFERENCES media_items(id) ON DELETE CASCADE,
    kind           TEXT NOT NULL CHECK (kind IN ('movie', 'series', 'season', 'episode')),
    title          TEXT NOT NULL,
    sort_title     TEXT NOT NULL,
    overview       TEXT NOT NULL DEFAULT '',
    season_number  INTEGER,
    episode_number INTEGER,
    release_date   DATE,
    path           TEXT,
    tmdb_id        INTEGER,
    metadata       JSONB NOT NULL DEFAULT '{}'::jsonb,
    added_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_media_items_library_id ON media_items(library_id);
CREATE INDEX idx_media_items_parent_id ON media_items(parent_id);
CREATE INDEX idx_media_items_kind ON media_items(kind);

CREATE TABLE playback_state (
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_item_id    UUID NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    position_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
    duration_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, media_item_id)
);
