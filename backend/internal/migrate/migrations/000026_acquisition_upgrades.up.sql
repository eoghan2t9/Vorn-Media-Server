CREATE TABLE acquisition_upgrades (
    id          SERIAL PRIMARY KEY,
    item_id     TEXT NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    title       TEXT NOT NULL,
    old_release TEXT NOT NULL DEFAULT '',
    new_release TEXT NOT NULL,
    source      TEXT NOT NULL DEFAULT '',  -- 'torrent' or 'nzb'
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_acquisition_upgrades_item ON acquisition_upgrades(item_id);
CREATE INDEX idx_acquisition_upgrades_created ON acquisition_upgrades(created_at DESC);
