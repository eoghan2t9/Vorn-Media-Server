package store

import (
	"database/sql"
	"errors"
)

// GetMediaItemByTmdbID finds a library's top-level (parent_id IS NULL)
// movie or series row by its TMDb ID -- the identity key on-demand
// acquisition uses instead of title matching, since a TMDb ID is exact
// where a title can collide.
func (s *Store) GetMediaItemByTmdbID(libraryID string, tmdbID int, kind string) (*MediaItem, error) {
	row := s.db.QueryRow(
		`SELECT `+mediaItemColumns+` FROM media_items
		 WHERE library_id = $1 AND kind = $2 AND tmdb_id = $3 AND parent_id IS NULL`,
		libraryID, kind, tmdbID,
	)
	m := &MediaItem{}
	if err := scanMediaItem(row, m); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return m, nil
}

// FindPlaceholderChild looks up an existing season/episode row under
// parentID by the same natural-identity key findOrCreateMediaItem uses for
// scan-promoted rows (library + kind + parent + title + season/episode),
// so SyncSeriesTree can run repeatedly without duplicating rows, and a
// season/episode that already has a real scanned file is found and left
// alone rather than getting a duplicate placeholder sibling.
func (s *Store) FindPlaceholderChild(libraryID string, parentID *string, kind, title string, seasonNumber, episodeNumber *int) (*MediaItem, error) {
	row := s.db.QueryRow(
		`SELECT `+mediaItemColumns+` FROM media_items
		 WHERE library_id = $1 AND kind = $2 AND parent_id IS NOT DISTINCT FROM $3
		   AND title = $4 AND season_number IS NOT DISTINCT FROM $5 AND episode_number IS NOT DISTINCT FROM $6`,
		libraryID, kind, parentID, title, seasonNumber, episodeNumber,
	)
	m := &MediaItem{}
	if err := scanMediaItem(row, m); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return m, nil
}

// ListMonitoredTopLevel returns every monitored movie/series row, across
// all libraries -- the "grab new content" half of acquisition.MonitorScheduler's
// tick: a monitored series gets its tree re-synced for new episodes, a
// monitored movie still without a file gets retried.
func (s *Store) ListMonitoredTopLevel() ([]*MediaItem, error) {
	rows, err := s.db.Query(`SELECT ` + mediaItemColumns + ` FROM media_items WHERE monitored = true AND kind IN ('movie', 'series') AND parent_id IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMediaItems(rows)
}

// ListMonitoredOwned returns every monitored, already-owned movie/episode
// row -- the "check for a better release" half of
// acquisition.MonitorScheduler's tick.
func (s *Store) ListMonitoredOwned() ([]*MediaItem, error) {
	rows, err := s.db.Query(`SELECT ` + mediaItemColumns + ` FROM media_items WHERE monitored = true AND acquisition_status = 'owned' AND kind IN ('movie', 'episode')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMediaItems(rows)
}

// ListOwnedRemoteItems returns every owned movie/episode whose path is a
// provider CDN URL (debrid/NZB-backed), regardless of monitored status --
// the proactive-link-refresh half of acquisition.MonitorScheduler's tick.
// Excludes locally-scanned files (a plain filesystem path never expires,
// so there's nothing to refresh) and everything not yet 'owned' (nothing
// playable there to check the liveness of).
func (s *Store) ListOwnedRemoteItems() ([]*MediaItem, error) {
	rows, err := s.db.Query(
		`SELECT ` + mediaItemColumns + ` FROM media_items
		 WHERE acquisition_status = 'owned' AND kind IN ('movie', 'episode') AND path LIKE 'http%'`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMediaItems(rows)
}

// CreatePlaceholderInput is the bare identity of a not-yet-owned
// media_item -- full metadata (overview/poster/etc) is filled in
// afterward via ApplyMetadata, reusing the same metadata-writing path the
// regular TMDb sync job already uses rather than duplicating it here.
type CreatePlaceholderInput struct {
	LibraryID     string
	ParentID      *string
	Kind          string // "movie" | "series" | "season" | "episode"
	Title         string
	SortTitle     string
	SeasonNumber  *int
	EpisodeNumber *int
	TmdbID        *int
	// Monitored inherits the parent series' current monitored value for a
	// season/episode created by a resync of an already-monitored series --
	// existing children get this via SetMediaItemMonitored's cascade
	// instead, this only matters for rows created after that cascade ran.
	Monitored bool
}

// CreatePlaceholder inserts a media_item with no file/stream yet
// (acquisition_status='placeholder', path left NULL).
func (s *Store) CreatePlaceholder(in CreatePlaceholderInput) (*MediaItem, error) {
	sortTitle := in.SortTitle
	if sortTitle == "" {
		sortTitle = in.Title
	}
	var id string
	err := s.db.QueryRow(
		`INSERT INTO media_items (library_id, parent_id, kind, title, sort_title, season_number, episode_number, tmdb_id, acquisition_status, monitored)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'placeholder', $9) RETURNING id`,
		in.LibraryID, in.ParentID, in.Kind, in.Title, sortTitle, in.SeasonNumber, in.EpisodeNumber, in.TmdbID, in.Monitored,
	).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetMediaItem(id)
}
