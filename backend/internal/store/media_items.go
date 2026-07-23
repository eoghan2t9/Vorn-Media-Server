package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type MediaItem struct {
	ID             string
	LibraryID      string
	ParentID       *string
	Kind           string // "movie" | "series" | "season" | "episode"
	Title          string
	SortTitle      string
	Overview       string
	SeasonNumber   *int
	EpisodeNumber  *int
	ReleaseDate    *time.Time
	Path           *string
	TmdbID         *int
	MetadataLocked bool
	AddedAt        time.Time
	UpdatedAt      time.Time
	PosterURL      string
	BackdropURL    string
}

const mediaItemColumns = `id, library_id, parent_id, kind, title, sort_title, overview, season_number, episode_number,
	release_date, path, tmdb_id, metadata_locked, added_at, updated_at,
	coalesce(metadata->>'posterUrl', ''), coalesce(metadata->>'backdropUrl', '')`

func scanMediaItem(row interface{ Scan(...any) error }, m *MediaItem) error {
	return row.Scan(&m.ID, &m.LibraryID, &m.ParentID, &m.Kind, &m.Title, &m.SortTitle, &m.Overview,
		&m.SeasonNumber, &m.EpisodeNumber, &m.ReleaseDate, &m.Path, &m.TmdbID, &m.MetadataLocked, &m.AddedAt, &m.UpdatedAt,
		&m.PosterURL, &m.BackdropURL)
}

// findOrCreateMediaItem looks up a media item by its natural identity
// (library + kind + parent + title + season/episode) and creates it if
// missing. Used by scan-file promotion so repeated scans converge on the
// same series/season/movie/episode rows instead of duplicating them.
func findOrCreateMediaItem(tx *sql.Tx, libraryID string, parentID *string, kind, title, sortTitle, path string, seasonNumber, episodeNumber *int) (string, error) {
	var id string
	err := tx.QueryRow(
		`SELECT id FROM media_items
		 WHERE library_id = $1 AND kind = $2 AND parent_id IS NOT DISTINCT FROM $3
		   AND title = $4 AND season_number IS NOT DISTINCT FROM $5 AND episode_number IS NOT DISTINCT FROM $6`,
		libraryID, kind, parentID, title, seasonNumber, episodeNumber,
	).Scan(&id)
	if err == nil {
		if path != "" {
			if _, err := tx.Exec(`UPDATE media_items SET path = $1, updated_at = now() WHERE id = $2`, path, id); err != nil {
				return "", err
			}
		}
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	var pathArg any
	if path != "" {
		pathArg = path
	}
	err = tx.QueryRow(
		`INSERT INTO media_items (library_id, parent_id, kind, title, sort_title, season_number, episode_number, path)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		libraryID, parentID, kind, title, sortTitle, seasonNumber, episodeNumber, pathArg,
	).Scan(&id)
	return id, err
}

type PromoteMovieInput struct {
	LibraryID string
	Title     string
	Year      *int
	Path      string
}

func (s *Store) PromoteMovie(in PromoteMovieInput) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	id, err := findOrCreateMediaItem(tx, in.LibraryID, nil, "movie", in.Title, in.Title, in.Path, nil, nil)
	if err != nil {
		return "", err
	}
	if in.Year != nil {
		date := time.Date(*in.Year, 1, 1, 0, 0, 0, 0, time.UTC)
		if _, err := tx.Exec(`UPDATE media_items SET release_date = $1 WHERE id = $2 AND release_date IS NULL`, date, id); err != nil {
			return "", err
		}
	}
	return id, tx.Commit()
}

type PromoteEpisodeInput struct {
	LibraryID     string
	SeriesTitle   string
	SeasonNumber  int
	EpisodeNumber int
	Path          string
}

func (s *Store) PromoteEpisode(in PromoteEpisodeInput) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	seriesID, err := findOrCreateMediaItem(tx, in.LibraryID, nil, "series", in.SeriesTitle, in.SeriesTitle, "", nil, nil)
	if err != nil {
		return "", err
	}
	season := in.SeasonNumber
	seasonTitle := seasonDisplayTitle(season)
	seasonID, err := findOrCreateMediaItem(tx, in.LibraryID, &seriesID, "season", seasonTitle, seasonTitle, "", &season, nil)
	if err != nil {
		return "", err
	}
	episode := in.EpisodeNumber
	episodeTitle := episodeDisplayTitle(season, episode)
	episodeID, err := findOrCreateMediaItem(tx, in.LibraryID, &seasonID, "episode", episodeTitle, episodeTitle, in.Path, &season, &episode)
	if err != nil {
		return "", err
	}

	return episodeID, tx.Commit()
}

func seasonDisplayTitle(season int) string {
	return fmt.Sprintf("Season %d", season)
}

func episodeDisplayTitle(season, episode int) string {
	return fmt.Sprintf("S%02dE%02d", season, episode)
}

func (s *Store) MarkScanFilePromoted(scanFileID, mediaItemID string) error {
	_, err := s.db.Exec(`UPDATE scan_files SET matched = true, media_item_id = $1 WHERE id = $2`, mediaItemID, scanFileID)
	return err
}

type UnmatchedScanFile struct {
	ID            string
	Path          string
	GuessedKind   string
	GuessedTitle  string
	GuessedYear   *int
	SeasonNumber  *int
	EpisodeNumber *int
}

func (s *Store) ListUnmatchedScanFiles(libraryID string) ([]*UnmatchedScanFile, error) {
	rows, err := s.db.Query(
		`SELECT id, path, guessed_kind, guessed_title, guessed_year, season_number, episode_number
		 FROM scan_files WHERE library_id = $1 AND matched = false`,
		libraryID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*UnmatchedScanFile
	for rows.Next() {
		f := &UnmatchedScanFile{}
		if err := rows.Scan(&f.ID, &f.Path, &f.GuessedKind, &f.GuessedTitle, &f.GuessedYear, &f.SeasonNumber, &f.EpisodeNumber); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

type ListItemsOptions struct {
	Kind string // "movie" | "series"; empty means both
	Sort string // "recent" | "alpha" (default alpha)
}

func (s *Store) ListMediaItems(libraryID string, opts ListItemsOptions) ([]*MediaItem, error) {
	orderBy := "sort_title ASC"
	if opts.Sort == "recent" {
		orderBy = "added_at DESC"
	}

	query := `SELECT ` + mediaItemColumns + ` FROM media_items WHERE library_id = $1 AND parent_id IS NULL`
	args := []any{libraryID}
	if opts.Kind != "" {
		query += ` AND kind = $2`
		args = append(args, opts.Kind)
	}
	query += ` ORDER BY ` + orderBy

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMediaItems(rows)
}

func (s *Store) GetMediaItem(id string) (*MediaItem, error) {
	row := s.db.QueryRow(`SELECT `+mediaItemColumns+` FROM media_items WHERE id = $1`, id)
	m := &MediaItem{}
	err := scanMediaItem(row, m)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// GetMediaItemImageURLs returns the poster/backdrop URLs a metadata sync (or
// manual override) wrote into an item's metadata blob, if any. Used by
// client-API compatibility layers (Jellyfin, Emby, Plex) whose image
// endpoints redirect straight to the provider-hosted art rather than Vorn
// caching it locally.
func (s *Store) GetMediaItemImageURLs(id string) (posterURL, backdropURL string, err error) {
	err = s.db.QueryRow(
		`SELECT coalesce(metadata->>'posterUrl', ''), coalesce(metadata->>'backdropUrl', '') FROM media_items WHERE id = $1`,
		id,
	).Scan(&posterURL, &backdropURL)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return posterURL, backdropURL, err
}

// ListChildren returns the direct children of a media item (e.g. seasons of
// a series, or episodes of a season), ordered by season/episode number.
func (s *Store) ListChildren(parentID string) ([]*MediaItem, error) {
	rows, err := s.db.Query(
		`SELECT `+mediaItemColumns+` FROM media_items WHERE parent_id = $1
		 ORDER BY coalesce(season_number, 0), coalesce(episode_number, 0)`,
		parentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMediaItems(rows)
}

func scanMediaItems(rows *sql.Rows) ([]*MediaItem, error) {
	var out []*MediaItem
	for rows.Next() {
		m := &MediaItem{}
		if err := scanMediaItem(rows, m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
