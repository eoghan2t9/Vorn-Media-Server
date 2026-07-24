-- Lets any logged-in user ask for a movie/series they'd like added, and
-- admins review a queue of them. tmdb_id identifies the title (via TMDb's
-- own search, independent of whether it's in the library yet) rather than
-- a media_items row, since the whole point is requesting things Vorn
-- doesn't have yet.
CREATE TABLE content_requests (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requested_by  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    media_type    TEXT NOT NULL CHECK (media_type IN ('movie', 'series')),
    tmdb_id       INT NOT NULL,
    title         TEXT NOT NULL,
    overview      TEXT NOT NULL DEFAULT '',
    release_date  TEXT NOT NULL DEFAULT '',
    poster_url    TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL CHECK (status IN ('pending', 'approved', 'declined')) DEFAULT 'pending',
    decided_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    decided_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_content_requests_requested_by ON content_requests(requested_by);
CREATE INDEX idx_content_requests_status ON content_requests(status);

-- One pending request per title at a time -- a second user asking for
-- something already pending should see "already requested", not create a
-- duplicate row an admin would have to notice and dedupe manually.
CREATE UNIQUE INDEX idx_content_requests_unique_pending ON content_requests(media_type, tmdb_id) WHERE status = 'pending';
