package store

type ServerStats struct {
	LibraryCount int64
	UserCount    int64
	MovieCount   int64
	SeriesCount  int64
	EpisodeCount int64
	ActiveUsers  int64
}

func (s *Store) GetServerStats() (*ServerStats, error) {
	stats := &ServerStats{}
	if err := s.db.QueryRow(`SELECT count(*) FROM libraries`).Scan(&stats.LibraryCount); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM users`).Scan(&stats.UserCount); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM media_items WHERE kind = 'movie'`).Scan(&stats.MovieCount); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM media_items WHERE kind = 'series'`).Scan(&stats.SeriesCount); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM media_items WHERE kind = 'episode'`).Scan(&stats.EpisodeCount); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT count(DISTINCT user_id) FROM sessions WHERE expires_at > now()`).Scan(&stats.ActiveUsers); err != nil {
		return nil, err
	}
	return stats, nil
}

// SearchMediaItems finds top-level (movie/series) items by title, scoped to
// libraries the caller can access (pass isAdmin=true to search everything).
func (s *Store) SearchMediaItems(query string, isAdmin bool, userID string, limit int) ([]*MediaItem, error) {
	rows, err := s.db.Query(
		`SELECT `+mediaItemColumns+`
		 FROM media_items
		 WHERE parent_id IS NULL
		   AND title ILIKE '%' || $1 || '%'
		   AND ($2 OR library_id IN (SELECT library_id FROM library_permissions WHERE user_id = $3))
		 ORDER BY title
		 LIMIT $4`,
		query, isAdmin, userID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMediaItems(rows)
}
