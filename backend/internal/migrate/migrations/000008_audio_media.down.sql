ALTER TABLE scan_files DROP COLUMN guessed_album;
ALTER TABLE scan_files DROP COLUMN guessed_artist;

ALTER TABLE scan_files DROP CONSTRAINT scan_files_guessed_kind_check;
ALTER TABLE scan_files ADD CONSTRAINT scan_files_guessed_kind_check
    CHECK (guessed_kind IN ('movie', 'episode'));

ALTER TABLE media_items DROP CONSTRAINT media_items_kind_check;
ALTER TABLE media_items ADD CONSTRAINT media_items_kind_check
    CHECK (kind IN ('movie', 'series', 'season', 'episode'));

ALTER TABLE libraries DROP CONSTRAINT libraries_type_check;
ALTER TABLE libraries ADD CONSTRAINT libraries_type_check
    CHECK (type IN ('movie', 'series'));
