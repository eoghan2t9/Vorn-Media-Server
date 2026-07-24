-- Purely a label an admin sets when creating a library (e.g. "Movies 4K"
-- alongside a regular "Movies" library) so viewers can tell them apart at a
-- glance -- the actual acquisition behavior still comes entirely from that
-- library's quality_profiles row, which the admin API seeds to 2160p-only
-- when a library is created with this flag set.
ALTER TABLE libraries ADD COLUMN is_4k BOOLEAN NOT NULL DEFAULT false;
