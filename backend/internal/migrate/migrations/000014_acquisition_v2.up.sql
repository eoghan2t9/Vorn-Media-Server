-- Fences which resolve attempt is currently authorized to write a
-- media_item's path: retry candidates, quality-upgrade re-checks, and
-- season-pack attempts can all leave a background resolve running past
-- the point its caller gave up on it, and only the most recently
-- authorized attempt should ever be allowed to promote into the item.
ALTER TABLE media_items ADD COLUMN active_debrid_item_id UUID REFERENCES debrid_items(id) ON DELETE SET NULL;

-- Subscribing to a movie/series so Vorn keeps searching for it (still
-- placeholder/error) or re-checks it for a better release (already owned)
-- on a recurring schedule, instead of only ever acting on a play click.
ALTER TABLE media_items ADD COLUMN monitored BOOLEAN NOT NULL DEFAULT false;

-- The release title an owned item's current path actually came from, so
-- the quality-upgrade check has something to compare a new candidate's
-- parsed resolution/codec against.
ALTER TABLE media_items ADD COLUMN current_release_title TEXT NOT NULL DEFAULT '';
