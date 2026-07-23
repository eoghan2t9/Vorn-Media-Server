CREATE TABLE scan_jobs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id   UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('real', 'synthetic')),
    status       TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed')) DEFAULT 'running',
    files_found  BIGINT NOT NULL DEFAULT 0,
    files_synced BIGINT NOT NULL DEFAULT 0,
    error        TEXT,
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at  TIMESTAMPTZ
);
CREATE INDEX idx_scan_jobs_library_id ON scan_jobs(library_id);

-- Raw candidates discovered by a scan, staged here before Phase 4's metadata
-- matching turns them into real media_items. Kept separate from media_items
-- so a scan never has to guess at final catalog identity.
CREATE TABLE scan_files (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id     UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    scan_job_id    UUID NOT NULL REFERENCES scan_jobs(id) ON DELETE CASCADE,
    path           TEXT NOT NULL,
    size_bytes     BIGINT NOT NULL DEFAULT 0,
    modified_at    TIMESTAMPTZ,
    guessed_kind   TEXT NOT NULL CHECK (guessed_kind IN ('movie', 'episode')),
    guessed_title  TEXT NOT NULL,
    guessed_year   INTEGER,
    season_number  INTEGER,
    episode_number INTEGER,
    matched        BOOLEAN NOT NULL DEFAULT false,
    discovered_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (library_id, path)
);
CREATE INDEX idx_scan_files_library_id ON scan_files(library_id);
CREATE INDEX idx_scan_files_matched ON scan_files(matched);
