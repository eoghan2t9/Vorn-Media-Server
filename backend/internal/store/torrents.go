package store

import (
	"database/sql"
	"errors"
	"time"
)

type Torrent struct {
	ID          string
	LibraryID   *string
	InfoHash    string
	Name        string
	MagnetURI   string
	SavePath    string
	Sequential  bool
	Status      string // "downloading" | "seeding" | "completed" | "error" | "removed"
	BytesTotal  int64
	BytesDone   int64
	Error       string
	Promoted    bool
	AddedAt     time.Time
	CompletedAt *time.Time
}

const torrentColumns = `id, library_id, info_hash, name, magnet_uri, save_path, sequential, status,
	bytes_total, bytes_done, error, promoted, added_at, completed_at`

func scanTorrent(row interface{ Scan(...any) error }, t *Torrent) error {
	return row.Scan(&t.ID, &t.LibraryID, &t.InfoHash, &t.Name, &t.MagnetURI, &t.SavePath, &t.Sequential, &t.Status,
		&t.BytesTotal, &t.BytesDone, &t.Error, &t.Promoted, &t.AddedAt, &t.CompletedAt)
}

type CreateTorrentInput struct {
	LibraryID  *string
	InfoHash   string
	Name       string
	MagnetURI  string
	SavePath   string
	Sequential bool
}

func (s *Store) CreateTorrent(in CreateTorrentInput) (*Torrent, error) {
	t := &Torrent{}
	row := s.db.QueryRow(
		`INSERT INTO torrents (library_id, info_hash, name, magnet_uri, save_path, sequential)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+torrentColumns,
		in.LibraryID, in.InfoHash, in.Name, in.MagnetURI, in.SavePath, in.Sequential,
	)
	if err := scanTorrent(row, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) ListTorrents() ([]*Torrent, error) {
	rows, err := s.db.Query(`SELECT ` + torrentColumns + ` FROM torrents WHERE status != 'removed' ORDER BY added_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Torrent
	for rows.Next() {
		t := &Torrent{}
		if err := scanTorrent(rows, t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetTorrent(id string) (*Torrent, error) {
	t := &Torrent{}
	err := scanTorrent(s.db.QueryRow(`SELECT `+torrentColumns+` FROM torrents WHERE id = $1`, id), t)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return t, nil
}

// ListActiveTorrents returns everything the client should have loaded on
// startup or should keep polling for progress (i.e. not yet terminal).
func (s *Store) ListActiveTorrents() ([]*Torrent, error) {
	rows, err := s.db.Query(`SELECT ` + torrentColumns + ` FROM torrents WHERE status IN ('downloading', 'seeding')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Torrent
	for rows.Next() {
		t := &Torrent{}
		if err := scanTorrent(rows, t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) UpdateTorrentProgress(id string, name string, bytesTotal, bytesDone int64, status string) error {
	_, err := s.db.Exec(
		`UPDATE torrents SET name = $1, bytes_total = $2, bytes_done = $3, status = $4 WHERE id = $5`,
		name, bytesTotal, bytesDone, status, id,
	)
	return err
}

func (s *Store) FinishTorrent(id string, err error) error {
	if err != nil {
		_, execErr := s.db.Exec(`UPDATE torrents SET status = 'error', error = $1 WHERE id = $2`, err.Error(), id)
		return execErr
	}
	_, execErr := s.db.Exec(`UPDATE torrents SET status = 'completed', completed_at = now() WHERE id = $1`, id)
	return execErr
}

func (s *Store) MarkTorrentPromoted(id string) error {
	_, err := s.db.Exec(`UPDATE torrents SET promoted = true WHERE id = $1`, id)
	return err
}

func (s *Store) RemoveTorrent(id string) error {
	_, err := s.db.Exec(`UPDATE torrents SET status = 'removed' WHERE id = $1`, id)
	return err
}

type TorrentIndexer struct {
	ID      string
	Name    string
	BaseURL string
	APIKey  string
	// Provider is "torznab" (default, any Torznab-compatible indexer manager
	// -- Prowlarr, Jackett, etc.) or "torbox" (TorBox's own IMDb-ID-driven
	// torrent search API, search-api.torbox.app -- see torrent.SearchByIMDb).
	// A torbox row's BaseURL is unused (the endpoint is fixed internally).
	Provider  string
	Enabled   bool
	CreatedAt time.Time
	// SupportsImdbSearch/SupportsTvdbSearch record whether this indexer's
	// t=caps document advertised imdbid/tvdbid support (always true for
	// provider == "torbox", which isn't a Torznab endpoint at all) --
	// SearchByIMDb skips indexers with neither set, since Vorn's
	// acquisition system only ever searches by id (resolveImdbSearchParams
	// in acquisition/service.go), never free text.
	SupportsImdbSearch bool
	SupportsTvdbSearch bool
	// DisabledReason explains why Enabled was flipped off automatically
	// (e.g. by the startup capability sweep) -- empty for an indexer an
	// admin disabled by hand, or one that's still enabled.
	DisabledReason string
}

func (s *Store) CreateTorrentIndexer(name, baseURL, apiKey, provider string, supportsImdb, supportsTvdb bool) (*TorrentIndexer, error) {
	if provider == "" {
		provider = "torznab"
	}
	idx := &TorrentIndexer{
		Name: name, BaseURL: baseURL, APIKey: apiKey, Provider: provider, Enabled: true,
		SupportsImdbSearch: supportsImdb, SupportsTvdbSearch: supportsTvdb,
	}
	err := s.db.QueryRow(
		`INSERT INTO torrent_indexers (name, base_url, api_key, provider, supports_imdb_search, supports_tvdb_search)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		name, baseURL, apiKey, provider, supportsImdb, supportsTvdb,
	).Scan(&idx.ID, &idx.CreatedAt)
	return idx, err
}

// UpdateTorrentIndexerInput fields are pointers so nil means "leave this
// field unchanged" -- in particular, an admin rotating name/baseUrl/provider
// shouldn't have to resend the API key, and the API never echoes it back for
// them to resend in the first place. A non-nil empty APIKey explicitly
// clears it.
type UpdateTorrentIndexerInput struct {
	Name               *string
	BaseURL            *string
	APIKey             *string
	Provider           *string
	Enabled            *bool
	SupportsImdbSearch *bool
	SupportsTvdbSearch *bool
	DisabledReason     *string
}

func (s *Store) UpdateTorrentIndexer(id string, in UpdateTorrentIndexerInput) (*TorrentIndexer, error) {
	idx := &TorrentIndexer{}
	err := s.db.QueryRow(
		`SELECT id, name, base_url, api_key, provider, enabled, created_at, supports_imdb_search, supports_tvdb_search, disabled_reason
		 FROM torrent_indexers WHERE id = $1`, id,
	).Scan(&idx.ID, &idx.Name, &idx.BaseURL, &idx.APIKey, &idx.Provider, &idx.Enabled, &idx.CreatedAt, &idx.SupportsImdbSearch, &idx.SupportsTvdbSearch, &idx.DisabledReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		idx.Name = *in.Name
	}
	if in.BaseURL != nil {
		idx.BaseURL = *in.BaseURL
	}
	if in.APIKey != nil {
		idx.APIKey = *in.APIKey
	}
	if in.Provider != nil {
		idx.Provider = *in.Provider
	}
	if in.Enabled != nil {
		idx.Enabled = *in.Enabled
	}
	if in.SupportsImdbSearch != nil {
		idx.SupportsImdbSearch = *in.SupportsImdbSearch
	}
	if in.SupportsTvdbSearch != nil {
		idx.SupportsTvdbSearch = *in.SupportsTvdbSearch
	}
	if in.DisabledReason != nil {
		idx.DisabledReason = *in.DisabledReason
	}
	if _, err := s.db.Exec(
		`UPDATE torrent_indexers SET name = $1, base_url = $2, api_key = $3, provider = $4, enabled = $5,
		 supports_imdb_search = $6, supports_tvdb_search = $7, disabled_reason = $8 WHERE id = $9`,
		idx.Name, idx.BaseURL, idx.APIKey, idx.Provider, idx.Enabled, idx.SupportsImdbSearch, idx.SupportsTvdbSearch, idx.DisabledReason, id,
	); err != nil {
		return nil, err
	}
	return idx, nil
}

func (s *Store) ListTorrentIndexers() ([]*TorrentIndexer, error) {
	rows, err := s.db.Query(`SELECT id, name, base_url, api_key, provider, enabled, created_at, supports_imdb_search, supports_tvdb_search, disabled_reason FROM torrent_indexers ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*TorrentIndexer
	for rows.Next() {
		idx := &TorrentIndexer{}
		if err := rows.Scan(&idx.ID, &idx.Name, &idx.BaseURL, &idx.APIKey, &idx.Provider, &idx.Enabled, &idx.CreatedAt, &idx.SupportsImdbSearch, &idx.SupportsTvdbSearch, &idx.DisabledReason); err != nil {
			return nil, err
		}
		out = append(out, idx)
	}
	return out, rows.Err()
}

func (s *Store) DeleteTorrentIndexer(id string) error {
	res, err := s.db.Exec(`DELETE FROM torrent_indexers WHERE id = $1`, id)
	return checkRowsAffected(res, err)
}
