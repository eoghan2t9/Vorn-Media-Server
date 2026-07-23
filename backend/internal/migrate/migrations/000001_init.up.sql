-- Phase 0 placeholder migration: confirms the migration runner is wired up.
-- Real schema (users, libraries, media_items, etc.) lands in Phase 1.
CREATE TABLE IF NOT EXISTS schema_bootstrap (
    id SERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
