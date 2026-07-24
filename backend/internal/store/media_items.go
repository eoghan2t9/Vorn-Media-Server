package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type MediaItem struct {
	ID                   string
	LibraryID            string
	ParentID             *string
	Kind                 string // "movie" | "series" | "season" | "episode" | "artist" | "album" | "track" | "audiobook" | "book" | "chapter"
	Title                string
	SortTitle            string
	Overview             string
	SeasonNumber         *int
	EpisodeNumber        *int
	ReleaseDate          *time.Time
	Path                 *string
	TmdbID               *int
	MetadataLocked       bool
	AddedAt              time.Time
	UpdatedAt            time.Time
	PosterURL            string
	BackdropURL          string
	Author               string // audiobook/book only, from metadata->>'author'
	LogoURL              string // from Fanart.tv enrichment, from metadata->>'logoUrl'
	RatingIMDb           string // from OMDb enrichment, from metadata->>'ratingImdb'
	RatingRottenTomatoes string // from OMDb enrichment, from metadata->>'ratingRottenTomatoes'
}

const mediaItemColumns = `id, library_id, parent_id, kind, title, sort_title, overview, season_number, episode_number,
	release_date, path, tmdb_id, metadata_locked, added_at, updated_at,
	coalesce(metadata->>'posterUrl', ''), coalesce(metadata->>'backdropUrl', ''), coalesce(metadata->>'author', ''),
	coalesce(metadata->>'logoUrl', ''), coalesce(metadata->>'ratingImdb', ''), coalesce(metadata->>'ratingRottenTomatoes', '')`

func scanMediaItem(row interface{ Scan(...any) error }, m *MediaItem) error {
	return row.Scan(&m.ID, &m.LibraryID, &m.ParentID, &m.Kind, &m.Title, &m.SortTitle, &m.Overview,
		&m.SeasonNumber, &m.EpisodeNumber, &m.ReleaseDate, &m.Path, &m.TmdbID, &m.MetadataLocked, &m.AddedAt, &m.UpdatedAt,
		&m.PosterURL, &m.BackdropURL, &m.Author, &m.LogoURL, &m.RatingIMDb, &m.RatingRottenTomatoes)
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

type PromoteTrackInput struct {
	LibraryID   string
	Artist      string
	Album       string
	Title       string
	TrackNumber int // 0 if unknown
	Path        string
	PosterURL   string // embedded cover art URL, "" if the file has none
}

// PromoteTrack finds-or-creates an artist -> album -> track chain, mirroring
// PromoteEpisode's series -> season -> episode pattern. TrackNumber reuses
// the episode_number column (see the 000008 migration) purely as "position
// within parent" -- ListChildren already orders by it.
func (s *Store) PromoteTrack(in PromoteTrackInput) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	artistID, err := findOrCreateMediaItem(tx, in.LibraryID, nil, "artist", in.Artist, in.Artist, "", nil, nil)
	if err != nil {
		return "", err
	}
	albumID, err := findOrCreateMediaItem(tx, in.LibraryID, &artistID, "album", in.Album, in.Album, "", nil, nil)
	if err != nil {
		return "", err
	}

	var trackNum *int
	if in.TrackNumber > 0 {
		n := in.TrackNumber
		trackNum = &n
	}
	title := in.Title
	if title == "" {
		title = fmt.Sprintf("Track %d", in.TrackNumber)
	}
	trackID, err := findOrCreateMediaItem(tx, in.LibraryID, &albumID, "track", title, title, in.Path, nil, trackNum)
	if err != nil {
		return "", err
	}
	if err := setMetadataIfEmpty(tx, trackID, "posterUrl", in.PosterURL); err != nil {
		return "", err
	}
	// Opportunistic: most rips embed the same cover art in every track, so
	// the album can usually get a poster immediately from its first
	// promoted track rather than waiting on a MusicBrainz sync.
	if err := setMetadataIfEmpty(tx, albumID, "posterUrl", in.PosterURL); err != nil {
		return "", err
	}

	return trackID, tx.Commit()
}

type PromoteAudiobookInput struct {
	LibraryID string
	Title     string
	Author    string
	Path      string
	PosterURL string
}

// PromoteAudiobook creates a flat, directly-playable item for a single-file
// audiobook -- the "book" and "chapter" kinds are only used when a book has
// more than one file (see PromoteChapter).
func (s *Store) PromoteAudiobook(in PromoteAudiobookInput) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	id, err := findOrCreateMediaItem(tx, in.LibraryID, nil, "audiobook", in.Title, in.Title, in.Path, nil, nil)
	if err != nil {
		return "", err
	}
	if err := setMetadataIfEmpty(tx, id, "posterUrl", in.PosterURL); err != nil {
		return "", err
	}
	if err := setMetadataIfEmpty(tx, id, "author", in.Author); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

type PromoteChapterInput struct {
	LibraryID     string
	BookTitle     string
	Author        string
	ChapterNumber int
	ChapterTitle  string
	Path          string
	PosterURL     string
}

// PromoteChapter finds-or-creates a "book" parent (used only for multi-file
// audiobooks) and a "chapter" child under it. ChapterNumber reuses
// episode_number the same way PromoteTrack reuses it for track number.
func (s *Store) PromoteChapter(in PromoteChapterInput) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	bookID, err := findOrCreateMediaItem(tx, in.LibraryID, nil, "book", in.BookTitle, in.BookTitle, "", nil, nil)
	if err != nil {
		return "", err
	}
	if err := setMetadataIfEmpty(tx, bookID, "author", in.Author); err != nil {
		return "", err
	}

	num := in.ChapterNumber
	title := in.ChapterTitle
	if title == "" {
		title = fmt.Sprintf("Chapter %d", num)
	}
	chapterID, err := findOrCreateMediaItem(tx, in.LibraryID, &bookID, "chapter", title, title, in.Path, nil, &num)
	if err != nil {
		return "", err
	}
	if err := setMetadataIfEmpty(tx, chapterID, "posterUrl", in.PosterURL); err != nil {
		return "", err
	}
	// Opportunistic, same reasoning as PromoteTrack -> album: the book gets
	// a cover from its first chapter's embedded art rather than waiting on
	// an Open Library sync.
	if err := setMetadataIfEmpty(tx, bookID, "posterUrl", in.PosterURL); err != nil {
		return "", err
	}

	return chapterID, tx.Commit()
}

// setMetadataIfEmpty merges {key: value} into a media item's metadata jsonb,
// but only if it doesn't already have that key -- so promotion (which may
// run repeatedly as new files are found) never clobbers a value a later
// metadata sync or manual admin edit has since set.
func setMetadataIfEmpty(tx *sql.Tx, itemID, key, value string) error {
	if value == "" {
		return nil
	}
	_, err := tx.Exec(
		`UPDATE media_items SET metadata = metadata || jsonb_build_object($1::text, $2::text)
		 WHERE id = $3 AND NOT (metadata ? $1)`,
		key, value, itemID,
	)
	return err
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
	GuessedArtist string
	GuessedAlbum  string
}

func (s *Store) ListUnmatchedScanFiles(libraryID string) ([]*UnmatchedScanFile, error) {
	rows, err := s.db.Query(
		`SELECT id, path, guessed_kind, guessed_title, guessed_year, season_number, episode_number, guessed_artist, guessed_album
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
		if err := rows.Scan(&f.ID, &f.Path, &f.GuessedKind, &f.GuessedTitle, &f.GuessedYear, &f.SeasonNumber, &f.EpisodeNumber, &f.GuessedArtist, &f.GuessedAlbum); err != nil {
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
