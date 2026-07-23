-- Allow music/audiobook libraries. Music uses artist -> album -> track
-- (mirrors series -> season -> episode); audiobooks use book -> chapter for
-- multi-file books, or a single flat "audiobook" item when there's only one
-- file. season_number/episode_number are reused as generic "primary/leaf
-- position within parent" for track and chapter numbers -- no new numeric
-- columns needed, same as how episode_number already orders episodes within
-- a season via ListChildren's ORDER BY.
ALTER TABLE libraries DROP CONSTRAINT libraries_type_check;
ALTER TABLE libraries ADD CONSTRAINT libraries_type_check
    CHECK (type IN ('movie', 'series', 'music', 'audiobook'));

ALTER TABLE media_items DROP CONSTRAINT media_items_kind_check;
ALTER TABLE media_items ADD CONSTRAINT media_items_kind_check
    CHECK (kind IN ('movie', 'series', 'season', 'episode', 'artist', 'album', 'track', 'audiobook', 'book', 'chapter'));

-- scan_files.guessed_kind only ever holds leaf/file-level kinds -- 'artist',
-- 'album', 'book' are containers synthesized during promotion, never guessed
-- directly from a single file. 'audiobook' (the flat single-file case) is
-- also decided during promotion (by counting how many chapter-guessed files
-- share the same guessed_album), so every audiobook-type file is guessed as
-- 'chapter' here regardless of whether it ends up promoted flat or nested.
ALTER TABLE scan_files DROP CONSTRAINT scan_files_guessed_kind_check;
ALTER TABLE scan_files ADD CONSTRAINT scan_files_guessed_kind_check
    CHECK (guessed_kind IN ('movie', 'episode', 'track', 'chapter'));

-- Read from embedded audio tags (ID3/Vorbis/MP4), not filename heuristics --
-- artist/album name for music, book title for audiobooks (falls back to the
-- containing directory name when a file has no tags at all).
ALTER TABLE scan_files ADD COLUMN guessed_artist TEXT NOT NULL DEFAULT '';
ALTER TABLE scan_files ADD COLUMN guessed_album TEXT NOT NULL DEFAULT '';
