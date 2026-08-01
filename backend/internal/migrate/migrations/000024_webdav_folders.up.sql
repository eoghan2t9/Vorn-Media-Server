-- webdav_folders lets a library list files from a WebDAV server (e.g.
-- TorBox's https://webdav.torbox.app) instead of (or alongside) local
-- filesystem folders. Each row maps one WebDAV root URL to one library;
-- a library can have both local folders and webdav folders, and the
-- scanner discovers both sources in parallel.
--
-- api_key is the password sent via HTTP Basic auth (username "torbox" for
-- TorBox's own WebDAV, but kept generic since any WebDAV server can be
-- configured here). The API key is also used to call GET /user/me for
-- a no-op validity check at creation time (same as the existing
-- TestTorBoxAccount validation for NZB usenet servers).
CREATE TABLE webdav_folders (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    library_id  TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    url         TEXT NOT NULL DEFAULT 'https://webdav.torbox.app',
    api_key     TEXT NOT NULL,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webdav_folders_library ON webdav_folders(library_id);
