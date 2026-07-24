package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type MetadataSyncJob struct {
	ID           string
	LibraryID    string
	Status       string
	ItemsFound   int64
	ItemsMatched int64
	Error        *string
	StartedAt    time.Time
	FinishedAt   *time.Time
}

func (s *Store) CreateMetadataSyncJob(libraryID string) (*MetadataSyncJob, error) {
	j := &MetadataSyncJob{LibraryID: libraryID, Status: "running"}
	err := s.db.QueryRow(
		`INSERT INTO metadata_sync_jobs (library_id) VALUES ($1) RETURNING id, started_at`,
		libraryID,
	).Scan(&j.ID, &j.StartedAt)
	if err != nil {
		return nil, err
	}
	return j, nil
}

func (s *Store) SetMetadataSyncJobCounts(id string, found, matched int64) error {
	_, err := s.db.Exec(`UPDATE metadata_sync_jobs SET items_found = $1, items_matched = $2 WHERE id = $3`, found, matched, id)
	return err
}

func (s *Store) FinishMetadataSyncJob(id string, syncErr error) error {
	status := "completed"
	var errMsg *string
	if syncErr != nil {
		status = "failed"
		msg := syncErr.Error()
		errMsg = &msg
	}
	_, err := s.db.Exec(
		`UPDATE metadata_sync_jobs SET status = $1, error = $2, finished_at = now() WHERE id = $3`,
		status, errMsg, id,
	)
	return err
}

func (s *Store) GetMetadataSyncJob(id string) (*MetadataSyncJob, error) {
	j := &MetadataSyncJob{}
	err := s.db.QueryRow(
		`SELECT id, library_id, status, items_found, items_matched, error, started_at, finished_at
		 FROM metadata_sync_jobs WHERE id = $1`, id,
	).Scan(&j.ID, &j.LibraryID, &j.Status, &j.ItemsFound, &j.ItemsMatched, &j.Error, &j.StartedAt, &j.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

// ListItemsNeedingMetadata returns items in a library that haven't been
// matched to a provider yet and aren't manually locked: top-level
// movies/series (against tmdb_id), plus albums and top-level
// books/audiobooks (against the metadata->>'externalId' key, since
// MusicBrainz/Open Library IDs aren't TMDb IDs and don't belong in that
// column -- see metadata.MusicProvider/AudiobookProvider). Albums aren't
// top-level (they're children of an artist), so parent_id IS NULL only
// applies to the movie/series/book/audiobook branch.
func (s *Store) ListItemsNeedingMetadata(libraryID string) ([]*MediaItem, error) {
	rows, err := s.db.Query(
		`SELECT `+mediaItemColumns+` FROM media_items
		 WHERE library_id = $1 AND metadata_locked = false
		   AND (
		     (parent_id IS NULL AND kind IN ('movie', 'series') AND tmdb_id IS NULL)
		     OR (parent_id IS NULL AND kind IN ('book', 'audiobook') AND NOT (metadata ? 'externalId'))
		     OR (kind = 'album' AND NOT (metadata ? 'externalId'))
		   )`,
		libraryID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMediaItems(rows)
}

// ListItemsNeedingCastSync returns top-level movie/series items that
// already have a tmdb_id (matched before cast/crew/similar existed, or
// simply skipped because a previous sync only ran the search-based
// ListItemsNeedingMetadata pass) but haven't been backfilled with
// cast/similar yet. A separate, cheaper pass than ListItemsNeedingMetadata
// -- these items skip the title-search step entirely since the TMDb id is
// already known.
func (s *Store) ListItemsNeedingCastSync(libraryID string) ([]*MediaItem, error) {
	rows, err := s.db.Query(
		`SELECT `+mediaItemColumns+` FROM media_items
		 WHERE library_id = $1 AND metadata_locked = false
		   AND parent_id IS NULL AND kind IN ('movie', 'series')
		   AND tmdb_id IS NOT NULL AND NOT (metadata ? 'cast')`,
		libraryID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMediaItems(rows)
}

// CastMember is one billed actor, as stored under metadata->'cast'.
type CastMember struct {
	Name      string `json:"name"`
	Character string `json:"character,omitempty"`
	PhotoURL  string `json:"photoUrl,omitempty"`
}

// SimilarTitle is one TMDb-recommended title, as stored under
// metadata->'similar' -- not necessarily materialized as a local media_item
// yet (see httpapi.handleOpenCatalogEntry, which is how a click on one
// becomes a real item).
type SimilarTitle struct {
	TmdbID      int    `json:"tmdbId"`
	Title       string `json:"title"`
	Overview    string `json:"overview,omitempty"`
	ReleaseDate string `json:"releaseDate,omitempty"`
	PosterURL   string `json:"posterUrl,omitempty"`
}

type MetadataUpdate struct {
	TmdbID      *int
	ExternalID  *string // MusicBrainz release MBID / Open Library work key
	Title       string
	Overview    string
	ReleaseDate *time.Time
	PosterURL   string
	BackdropURL string
	TrailerURL  string
	Author      string // audiobook/book only, stored in metadata jsonb

	// Optional enrichment layered on top of a movie/series match --
	// LogoURL from Fanart.tv, the two ratings from OMDb. All empty for a
	// plain TMDb/TheTVDB match with no enrichment providers configured.
	LogoURL              string
	RatingIMDb           string
	RatingRottenTomatoes string

	// Cast/Directors/Similar are movie/series only, from TMDb's credits and
	// similar endpoints. Empty for a plain TheTVDB fallback match, or when
	// backfilling an item TMDb has no data for.
	Cast      []CastMember
	Directors []string
	Similar   []SimilarTitle
}

// ApplyMetadata writes a provider match (or a manual admin correction) onto
// a media item. Passing lock=true marks it so future automatic syncs won't
// overwrite the correction.
func (s *Store) ApplyMetadata(itemID string, update MetadataUpdate, lock bool) error {
	metadataJSON := map[string]any{}
	if update.PosterURL != "" {
		metadataJSON["posterUrl"] = update.PosterURL
	}
	if update.BackdropURL != "" {
		metadataJSON["backdropUrl"] = update.BackdropURL
	}
	if update.TrailerURL != "" {
		metadataJSON["trailerUrl"] = update.TrailerURL
	}
	if update.ExternalID != nil {
		metadataJSON["externalId"] = *update.ExternalID
	}
	if len(update.Cast) > 0 {
		metadataJSON["cast"] = update.Cast
	}
	if len(update.Directors) > 0 {
		metadataJSON["directors"] = update.Directors
	}
	if len(update.Similar) > 0 {
		metadataJSON["similar"] = update.Similar
	}
	if update.Author != "" {
		metadataJSON["author"] = update.Author
	}
	if update.LogoURL != "" {
		metadataJSON["logoUrl"] = update.LogoURL
	}
	if update.RatingIMDb != "" {
		metadataJSON["ratingImdb"] = update.RatingIMDb
	}
	if update.RatingRottenTomatoes != "" {
		metadataJSON["ratingRottenTomatoes"] = update.RatingRottenTomatoes
	}

	_, err := s.db.Exec(
		`UPDATE media_items SET
		   title = CASE WHEN $1 = '' THEN title ELSE $1 END,
		   overview = CASE WHEN $2 = '' THEN overview ELSE $2 END,
		   release_date = coalesce($3, release_date),
		   tmdb_id = coalesce($4, tmdb_id),
		   metadata = metadata || $5::jsonb,
		   metadata_locked = metadata_locked OR $6,
		   updated_at = now()
		 WHERE id = $7`,
		update.Title, update.Overview, update.ReleaseDate, update.TmdbID, toJSONB(metadataJSON), lock, itemID,
	)
	return err
}

func toJSONB(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// GetItemCastAndSimilar returns the cast/crew and similar-titles a metadata
// sync wrote into itemID's metadata blob, empty slices if none yet. A
// dedicated lightweight query rather than folding into mediaItemColumns
// (see ListChildren for the same "extra per-item data only the detail
// handler needs" pattern) -- every list-view query already selects
// mediaItemColumns and doesn't need this nested JSON.
func (s *Store) GetItemCastAndSimilar(itemID string) (cast []CastMember, directors []string, similar []SimilarTitle, err error) {
	var castJSON, directorsJSON, similarJSON []byte
	err = s.db.QueryRow(
		`SELECT coalesce(metadata->'cast', '[]'), coalesce(metadata->'directors', '[]'), coalesce(metadata->'similar', '[]')
		 FROM media_items WHERE id = $1`, itemID,
	).Scan(&castJSON, &directorsJSON, &similarJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, nil, err
	}
	_ = json.Unmarshal(castJSON, &cast)
	_ = json.Unmarshal(directorsJSON, &directors)
	_ = json.Unmarshal(similarJSON, &similar)
	return cast, directors, similar, nil
}
