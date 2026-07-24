-- Lets an admin mark one library per (type, is_4k) combo as the default
-- target a content request auto-fulfills into -- e.g. a default standard
-- "Movies" library and a default "Movies 4K" library, so a request can fan
-- out into both without the requester having to pick libraries themselves.
ALTER TABLE libraries ADD COLUMN default_request_target BOOLEAN NOT NULL DEFAULT false;
CREATE UNIQUE INDEX idx_libraries_one_default_target ON libraries(type, is_4k) WHERE default_request_target;

-- Tracks which media_item(s) a content request spawned, one row per target
-- library it was fanned out to (typically one standard + one 4K), so the
-- requester can see independent acquisition progress per quality instead of
-- a single blended status.
CREATE TABLE content_request_fulfillments (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    content_request_id UUID NOT NULL REFERENCES content_requests(id) ON DELETE CASCADE,
    library_id         UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
    media_item_id      UUID NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (content_request_id, library_id)
);
