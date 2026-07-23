CREATE TABLE metadata_sync_jobs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    library_id    UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    status        TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed')) DEFAULT 'running',
    items_found   BIGINT NOT NULL DEFAULT 0,
    items_matched BIGINT NOT NULL DEFAULT 0,
    error         TEXT,
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ
);
CREATE INDEX idx_metadata_sync_jobs_library_id ON metadata_sync_jobs(library_id);

-- Set once an item has been manually corrected by an admin, so a later
-- metadata sync doesn't silently overwrite the fix with a fresh provider match.
ALTER TABLE media_items ADD COLUMN metadata_locked BOOLEAN NOT NULL DEFAULT false;
