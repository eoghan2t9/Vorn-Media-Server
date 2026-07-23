package store

import "time"

type PlaybackState struct {
	MediaItemID     string
	PositionSeconds float64
	DurationSeconds float64
	UpdatedAt       time.Time
}

func (s *Store) UpsertPlaybackState(userID, mediaItemID string, position, duration float64) error {
	_, err := s.db.Exec(
		`INSERT INTO playback_state (user_id, media_item_id, position_seconds, duration_seconds, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (user_id, media_item_id) DO UPDATE SET
		   position_seconds = EXCLUDED.position_seconds,
		   duration_seconds = EXCLUDED.duration_seconds,
		   updated_at = now()`,
		userID, mediaItemID, position, duration,
	)
	return err
}

func (s *Store) GetPlaybackState(userID, mediaItemID string) (*PlaybackState, error) {
	p := &PlaybackState{MediaItemID: mediaItemID}
	err := s.db.QueryRow(
		`SELECT position_seconds, duration_seconds, updated_at FROM playback_state WHERE user_id = $1 AND media_item_id = $2`,
		userID, mediaItemID,
	).Scan(&p.PositionSeconds, &p.DurationSeconds, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// ContinueWatchingItem pairs a media item with the user's progress on it.
type ContinueWatchingItem struct {
	Item     *MediaItem
	Progress PlaybackState
}

// ListContinueWatching returns items a user has started but not finished
// (progress between 5% and 95% of duration), most recently watched first,
// scoped to the libraries they can access.
func (s *Store) ListContinueWatching(userID string, isAdmin bool, limit int) ([]*ContinueWatchingItem, error) {
	query := `
		SELECT m.id, m.library_id, m.parent_id, m.kind, m.title, m.sort_title, m.overview,
		       m.season_number, m.episode_number, m.release_date, m.path, m.tmdb_id, m.metadata_locked, m.added_at, m.updated_at,
		       coalesce(m.metadata->>'posterUrl', ''), coalesce(m.metadata->>'backdropUrl', ''),
		       p.position_seconds, p.duration_seconds, p.updated_at
		FROM playback_state p
		JOIN media_items m ON m.id = p.media_item_id
		WHERE p.user_id = $1
		  AND p.duration_seconds > 0
		  AND p.position_seconds > p.duration_seconds * 0.05
		  AND p.position_seconds < p.duration_seconds * 0.95
		  AND ($2 OR m.library_id IN (SELECT library_id FROM library_permissions WHERE user_id = $1))
		ORDER BY p.updated_at DESC
		LIMIT $3`

	rows, err := s.db.Query(query, userID, isAdmin, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*ContinueWatchingItem
	for rows.Next() {
		m := &MediaItem{}
		p := PlaybackState{}
		if err := rows.Scan(&m.ID, &m.LibraryID, &m.ParentID, &m.Kind, &m.Title, &m.SortTitle, &m.Overview,
			&m.SeasonNumber, &m.EpisodeNumber, &m.ReleaseDate, &m.Path, &m.TmdbID, &m.MetadataLocked, &m.AddedAt, &m.UpdatedAt,
			&m.PosterURL, &m.BackdropURL,
			&p.PositionSeconds, &p.DurationSeconds, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.MediaItemID = m.ID
		out = append(out, &ContinueWatchingItem{Item: m, Progress: p})
	}
	return out, rows.Err()
}
