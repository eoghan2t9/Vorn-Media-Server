package store

import "time"

// CurrentlyWatchingEntry pairs an active-ish playback session with who's
// watching and what. "Active" is a heuristic: progress updated recently and
// not yet finished, since Vorn doesn't otherwise track connection state.
type CurrentlyWatchingEntry struct {
	Username        string
	Item            *MediaItem
	PositionSeconds float64
	DurationSeconds float64
	UpdatedAt       time.Time
}

// ListCurrentlyWatching returns playback sessions updated within `within`
// that haven't finished (position < 98% of duration), most recent first.
func (s *Store) ListCurrentlyWatching(within time.Duration) ([]*CurrentlyWatchingEntry, error) {
	// Written with explicit, table-qualified columns rather than the shared
	// mediaItemColumns helper: that constant's bare `updated_at` would be
	// ambiguous here since playback_state also has an updated_at.
	rows, err := s.db.Query(
		`SELECT u.username,
		        m.id, m.library_id, m.parent_id, m.kind, m.title, m.sort_title, m.overview,
		        m.season_number, m.episode_number, m.release_date, m.path, m.tmdb_id, m.metadata_locked,
		        m.added_at, m.updated_at,
		        p.position_seconds, p.duration_seconds, p.updated_at
		 FROM playback_state p
		 JOIN media_items m ON m.id = p.media_item_id
		 JOIN users u ON u.id = p.user_id
		 WHERE p.updated_at > now() - ($1 * interval '1 second')
		   AND p.duration_seconds > 0
		   AND p.position_seconds < p.duration_seconds * 0.98
		 ORDER BY p.updated_at DESC`,
		within.Seconds(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*CurrentlyWatchingEntry
	for rows.Next() {
		e := &CurrentlyWatchingEntry{Item: &MediaItem{}}
		if err := rows.Scan(&e.Username,
			&e.Item.ID, &e.Item.LibraryID, &e.Item.ParentID, &e.Item.Kind, &e.Item.Title, &e.Item.SortTitle, &e.Item.Overview,
			&e.Item.SeasonNumber, &e.Item.EpisodeNumber, &e.Item.ReleaseDate, &e.Item.Path, &e.Item.TmdbID, &e.Item.MetadataLocked,
			&e.Item.AddedAt, &e.Item.UpdatedAt,
			&e.PositionSeconds, &e.DurationSeconds, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
