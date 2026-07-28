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
	RatingTMDb           string // TMDb's own vote_average, from metadata->>'ratingTmdb' -- always available, no OMDb needed
	TrailerURL           string // YouTube watch URL from TMDb, from metadata->>'trailerUrl'
	AcquisitionStatus    string // "owned" | "placeholder" | "searching" | "acquiring" | "error"
	AcquisitionError     string
	ActiveDebridItemID   *string // fences which resolve attempt is currently authorized to write Path -- see SetMediaItemActiveDebridItem
	ActiveNZBDownloadID  *string // NZB's counterpart to ActiveDebridItemID -- see SetMediaItemActiveNZBDownload
	Monitored            bool
	CurrentReleaseTitle  string // the release title Path was last promoted from, for the quality-upgrade comparison
	// RuntimeMinutes is TMDb's reported runtime, from metadata->>'runtimeMinutes'
	// -- nil until acquisition's ensureExpectedRuntime backfills it (or the
	// item was materialized after this field existed). Used only by the
	// content-verification check in debrid/nzb's promote.go, comparing
	// against a resolved release's actual probed duration.
	RuntimeMinutes *int
	// ImdbID (e.g. "tt0137523"), from metadata->>'imdbId' -- distinct from
	// RatingIMDb (an OMDb rating *string*, not an ID). nil until
	// ensureExpectedRuntime backfills it. Used to query IMDb-ID-driven
	// torrent/NZB indexers (see torrent.SearchByIMDb/nzb.SearchByIMDb).
	ImdbID *string
	// TvdbID (series only), from metadata->>'tvdbId' -- most real-world
	// Newznab indexers (confirmed against a live NZBGeek account: its own
	// caps document lists tv-search's supportedParams as "q,rid,tvdbid,
	// tvmazeid,season,ep" -- no imdbid at all) key TV search off TheTVDB ID,
	// not IMDb ID, unlike movie search. nil until ensureExpectedRuntime
	// backfills it.
	TvdbID *int
}

const mediaItemColumns = `id, library_id, parent_id, kind, title, sort_title, overview, season_number, episode_number,
	release_date, path, tmdb_id, metadata_locked, added_at, updated_at,
	coalesce(metadata->>'posterUrl', ''), coalesce(metadata->>'backdropUrl', ''), coalesce(metadata->>'author', ''),
	coalesce(metadata->>'logoUrl', ''), coalesce(metadata->>'ratingImdb', ''), coalesce(metadata->>'ratingRottenTomatoes', ''),
	coalesce(metadata->>'ratingTmdb', ''), coalesce(metadata->>'trailerUrl', ''),
	acquisition_status, acquisition_error, active_debrid_item_id, active_nzb_download_id, monitored, current_release_title,
	(metadata->>'runtimeMinutes')::int, metadata->>'imdbId', (metadata->>'tvdbId')::int`

func scanMediaItem(row interface{ Scan(...any) error }, m *MediaItem) error {
	return row.Scan(&m.ID, &m.LibraryID, &m.ParentID, &m.Kind, &m.Title, &m.SortTitle, &m.Overview,
		&m.SeasonNumber, &m.EpisodeNumber, &m.ReleaseDate, &m.Path, &m.TmdbID, &m.MetadataLocked, &m.AddedAt, &m.UpdatedAt,
		&m.PosterURL, &m.BackdropURL, &m.Author, &m.LogoURL, &m.RatingIMDb, &m.RatingRottenTomatoes, &m.RatingTMDb, &m.TrailerURL,
		&m.AcquisitionStatus, &m.AcquisitionError, &m.ActiveDebridItemID, &m.ActiveNZBDownloadID, &m.Monitored, &m.CurrentReleaseTitle,
		&m.RuntimeMinutes, &m.ImdbID, &m.TvdbID)
}

// SetMediaItemPath is called once acquisition produces a real file/stream
// URL for a placeholder item, flipping it back to the normal "owned" state
// -- the same state every scan/torrent/NZB-promoted item has always been in.
// releaseTitle is stored for the quality-upgrade check to later compare
// against a newer candidate's parsed resolution/codec.
func (s *Store) SetMediaItemPath(id, path, releaseTitle string) error {
	_, err := s.db.Exec(
		`UPDATE media_items SET path = $1, acquisition_status = 'owned', acquisition_error = '', current_release_title = $2, updated_at = now() WHERE id = $3`,
		path, releaseTitle, id,
	)
	return err
}

// SetMediaItemAcquisitionError records a failed acquisition attempt so a
// polling client sees the failure instead of the item hanging on
// "acquiring" forever.
func (s *Store) SetMediaItemAcquisitionError(id, message string) error {
	_, err := s.db.Exec(
		`UPDATE media_items SET acquisition_status = 'error', acquisition_error = $1, updated_at = now() WHERE id = $2`,
		message, id,
	)
	return err
}

// SetMediaItemAcquisitionStatus transitions a placeholder item's status
// (e.g. 'placeholder' -> 'searching' -> 'acquiring') without touching path
// or error.
func (s *Store) SetMediaItemAcquisitionStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE media_items SET acquisition_status = $1, updated_at = now() WHERE id = $2`, status, id)
	return err
}

// ResetStuckAcquisitions flips every item still in 'searching'/'acquiring'
// back to 'error' -- both states only ever exist while a background
// runAcquire goroutine (acquisition.Service) is actively working an item,
// and that goroutine runs on context.Background() with no persistence of
// its own, so a process restart (deploy, crash, host reboot) abandons it
// mid-flight with no code path left to ever finish or retry it: Acquire/
// Reacquire only start a fresh attempt for 'placeholder'/'error'/'owned'
// items, never for one already 'searching'/'acquiring'. Called once at
// server startup (before anything can be in flight, since that requires a
// running server) so a play click on a previously-wedged item retries
// cleanly instead of reporting "acquiring" forever.
func (s *Store) ResetStuckAcquisitions() (int, error) {
	res, err := s.db.Exec(
		`UPDATE media_items SET acquisition_status = 'error', acquisition_error = 'interrupted by a server restart', updated_at = now() WHERE acquisition_status IN ('searching', 'acquiring')`,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// SetMediaItemActiveDebridItem resets id's debrid claim slot -- called once,
// right before acquisition launches a fresh batch of racing candidates
// against id, so a stale value left over from an earlier round (or a
// crashed process) can't block this round's claim. Pass "" (the only
// caller-facing use today) to clear to NULL.
func (s *Store) SetMediaItemActiveDebridItem(id, debridItemID string) error {
	var arg any
	if debridItemID != "" {
		arg = debridItemID
	}
	_, err := s.db.Exec(`UPDATE media_items SET active_debrid_item_id = $1, updated_at = now() WHERE id = $2`, arg, id)
	return err
}

// SetMediaItemActiveNZBDownload is SetMediaItemActiveDebridItem's NZB
// counterpart.
func (s *Store) SetMediaItemActiveNZBDownload(id, nzbDownloadID string) error {
	var arg any
	if nzbDownloadID != "" {
		arg = nzbDownloadID
	}
	_, err := s.db.Exec(`UPDATE media_items SET active_nzb_download_id = $1, updated_at = now() WHERE id = $2`, arg, id)
	return err
}

// ClaimMediaItemForDebridItem atomically claims id for debridItemID -- the
// mechanism that decides a winner when acquisition races several candidates
// concurrently for the same media item. Succeeds (true) only if nothing has
// claimed id yet (active_debrid_item_id IS NULL) and it isn't already
// owned; Postgres row-level locking makes concurrent claims against the same
// row serialize, so exactly one caller ever wins even if several resolves
// finish at almost the same instant. A losing caller must not touch the
// media item at all -- see debrid/promote.go.
func (s *Store) ClaimMediaItemForDebridItem(id, debridItemID string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE media_items SET active_debrid_item_id = $1, updated_at = now()
		 WHERE id = $2 AND acquisition_status != 'owned' AND active_debrid_item_id IS NULL`,
		debridItemID, id,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ClaimMediaItemForNZBDownload is ClaimMediaItemForDebridItem's NZB
// counterpart.
func (s *Store) ClaimMediaItemForNZBDownload(id, nzbDownloadID string) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE media_items SET active_nzb_download_id = $1, updated_at = now()
		 WHERE id = $2 AND acquisition_status != 'owned' AND active_nzb_download_id IS NULL`,
		nzbDownloadID, id,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// SetMediaItemMonitored subscribes/unsubscribes id (a movie or series) to
// proactive re-checking (see acquisition.MonitorScheduler). Toggling a
// series cascades to every current season/episode under it in the same
// direction -- there is no per-episode override in this pass, so
// un-monitoring a series un-monitors everything under it too.
func (s *Store) SetMediaItemMonitored(id string, monitored bool) error {
	item, err := s.GetMediaItem(id)
	if err != nil {
		return err
	}
	if item.Kind == "series" {
		_, err := s.db.Exec(
			`UPDATE media_items SET monitored = $1, updated_at = now()
			 WHERE id = $2 OR parent_id = $2 OR parent_id IN (SELECT id FROM media_items WHERE parent_id = $2)`,
			monitored, id,
		)
		return err
	}
	_, err = s.db.Exec(`UPDATE media_items SET monitored = $1, updated_at = now() WHERE id = $2`, monitored, id)
	return err
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
			// acquisition_status reset to 'owned' here too: a scan can land
			// on a row that was previously created as an acquisition
			// placeholder (e.g. the user added the file manually before
			// on-demand acquisition finished), and a real file on disk
			// always wins over placeholder bookkeeping.
			if _, err := tx.Exec(`UPDATE media_items SET path = $1, acquisition_status = 'owned', updated_at = now() WHERE id = $2`, path, id); err != nil {
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
