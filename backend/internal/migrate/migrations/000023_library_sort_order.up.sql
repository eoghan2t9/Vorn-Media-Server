-- Lets an admin reorder libraries on the viewer's Home page (see
-- Store.ReorderLibraries) instead of always sorting by creation date.
-- Default 0 for every existing row is intentional, not a placeholder to
-- backfill: ListLibraries orders by (sort_order, created_at), so leaving
-- every row at 0 falls through to today's created_at ordering unchanged
-- until an admin actually reorders something.
ALTER TABLE libraries ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;
