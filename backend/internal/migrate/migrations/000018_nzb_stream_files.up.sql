CREATE TABLE nzb_files (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    nzb_download_id UUID NOT NULL REFERENCES nzb_downloads(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    stream_url      TEXT NOT NULL
);
CREATE INDEX idx_nzb_files_download_id ON nzb_files(nzb_download_id);
