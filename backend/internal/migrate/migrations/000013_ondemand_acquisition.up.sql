-- On-demand acquisition: a media_item can now exist as a placeholder (no
-- file yet, just TMDb metadata) that transitions through search/acquire on
-- first play instead of only ever being created after a file already
-- exists. Every pre-existing row already has a file, so 'owned' is the
-- correct default/backfill for all of them.
ALTER TABLE media_items ADD COLUMN acquisition_status TEXT NOT NULL DEFAULT 'owned'
    CHECK (acquisition_status IN ('owned', 'placeholder', 'searching', 'acquiring', 'error'));
ALTER TABLE media_items ADD COLUMN acquisition_error TEXT NOT NULL DEFAULT '';

-- Lets a debrid resolve job point back at the exact placeholder media_item
-- it's acquiring for, so completion can update that row directly instead of
-- filename-guessing a new one (debrid.PromoteCompleted's existing path,
-- still used for manually-added magnets with no media_item_id).
ALTER TABLE debrid_items ADD COLUMN media_item_id UUID REFERENCES media_items(id) ON DELETE SET NULL;
CREATE INDEX idx_debrid_items_media_item_id ON debrid_items(media_item_id);

-- Per-library release-selection rules for automatic acquisition. One row
-- per library; a library with no row falls back to hardcoded defaults in
-- Go rather than needing a row inserted at library-creation time.
CREATE TABLE quality_profiles (
    library_id      UUID PRIMARY KEY REFERENCES libraries(id) ON DELETE CASCADE,
    min_resolution  TEXT NOT NULL DEFAULT '720p' CHECK (min_resolution IN ('480p', '720p', '1080p', '2160p')),
    max_resolution  TEXT NOT NULL DEFAULT '2160p' CHECK (max_resolution IN ('480p', '720p', '1080p', '2160p')),
    preferred_codec TEXT NOT NULL DEFAULT '' CHECK (preferred_codec IN ('', 'x264', 'x265', 'av1')),
    min_seeders     INT NOT NULL DEFAULT 1,
    prefer_remux    BOOLEAN NOT NULL DEFAULT false,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
