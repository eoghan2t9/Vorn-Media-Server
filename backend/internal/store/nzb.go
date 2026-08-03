package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type UsenetServer struct {
	ID        string
	Name      string
	APIKey    string
	Enabled   bool
	CreatedAt time.Time
}

func (s *Store) CreateUsenetServer(in UsenetServer) (*UsenetServer, error) {
	out := in
	err := s.db.QueryRow(
		`INSERT INTO usenet_servers (name, api_key) VALUES ($1, $2) RETURNING id, enabled, created_at`,
		out.Name, out.APIKey,
	).Scan(&out.ID, &out.Enabled, &out.CreatedAt)
	return &out, err
}

func (s *Store) ListUsenetServers() ([]*UsenetServer, error) {
	rows, err := s.db.Query(
		`SELECT id, name, api_key, enabled, created_at FROM usenet_servers ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*UsenetServer
	for rows.Next() {
		u := &UsenetServer{}
		if err := rows.Scan(&u.ID, &u.Name, &u.APIKey, &u.Enabled, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) DeleteUsenetServer(id string) error {
	res, err := s.db.Exec(`DELETE FROM usenet_servers WHERE id = $1`, id)
	return checkRowsAffected(res, err)
}

// UpdateUsenetServerInput fields are pointers so nil means "leave this field
// unchanged" -- in particular, the API never echoes secrets back for an
// admin to resend, so a nil APIKey leaves the stored key untouched. A
// non-nil empty APIKey explicitly clears it.
type UpdateUsenetServerInput struct {
	Name    *string
	APIKey  *string
	Enabled *bool
}

func (s *Store) UpdateUsenetServer(id string, in UpdateUsenetServerInput) (*UsenetServer, error) {
	u := &UsenetServer{}
	err := s.db.QueryRow(
		`SELECT id, name, api_key, enabled, created_at FROM usenet_servers WHERE id = $1`, id,
	).Scan(&u.ID, &u.Name, &u.APIKey, &u.Enabled, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		u.Name = *in.Name
	}
	if in.APIKey != nil {
		u.APIKey = *in.APIKey
	}
	if in.Enabled != nil {
		u.Enabled = *in.Enabled
	}
	if _, err := s.db.Exec(
		`UPDATE usenet_servers SET name = $1, api_key = $2, enabled = $3 WHERE id = $4`,
		u.Name, u.APIKey, u.Enabled, id,
	); err != nil {
		return nil, err
	}
	return u, nil
}

type NZBDownload struct {
	ID        string
	LibraryID *string
	// MediaItemID is set only for the on-demand acquisition path (see
	// acquisition.Service.raceNZBCandidates) -- it's what lets nzb.Service's
	// onComplete callback promote into this exact placeholder instead of
	// filename-guessing into LibraryID at large (PromoteCompleted). nil for
	// the manual admin-driven Admin > NZB flow, same as debrid_items.
	MediaItemID *string
	Name        string
	Status      string // "downloading" | "repairing" | "completed" | "error" | "removed"
	BytesTotal  int64
	BytesDone   int64
	Error       string
	Promoted    bool
	AddedAt     time.Time
	CompletedAt *time.Time
	// ProviderRef is TorBox's own usenetdownload_id for this download, set
	// once CreateUsenetDownload succeeds -- used by Service.Remove to delete
	// it from the TorBox account, reclaiming storage/active-download quota.
	ProviderRef string
}

const nzbColumns = `id, library_id, media_item_id, name, status, bytes_total, bytes_done, error, promoted, added_at, completed_at, provider_ref`

func scanNZBDownload(row interface{ Scan(...any) error }, n *NZBDownload) error {
	return row.Scan(&n.ID, &n.LibraryID, &n.MediaItemID, &n.Name, &n.Status, &n.BytesTotal, &n.BytesDone, &n.Error, &n.Promoted, &n.AddedAt, &n.CompletedAt, &n.ProviderRef)
}

// SetNZBDownloadProviderRef records TorBox's own id for id's usenet
// download, called once CreateUsenetDownload succeeds (the ref isn't known
// at CreateNZBDownload time, since that happens before the resolve
// goroutine even starts).
func (s *Store) SetNZBDownloadProviderRef(id, providerRef string) error {
	_, err := s.db.Exec(`UPDATE nzb_downloads SET provider_ref = $1 WHERE id = $2`, providerRef, id)
	return err
}

// SetNZBDownloadLibrary assigns an NZB download to a library, used by
// reconciliation to retroactively assign a library to orphaned downloads
// that were discovered without one.
func (s *Store) SetNZBDownloadLibrary(id, libraryID string) error {
	res, err := s.db.Exec(`UPDATE nzb_downloads SET library_id = $1 WHERE id = $2`, libraryID, id)
	return checkRowsAffected(res, err)
}

type CreateNZBDownloadInput struct {
	LibraryID   *string
	MediaItemID *string
	Name        string
}

func (s *Store) CreateNZBDownload(in CreateNZBDownloadInput) (*NZBDownload, error) {
	n := &NZBDownload{}
	row := s.db.QueryRow(
		`INSERT INTO nzb_downloads (library_id, media_item_id, name) VALUES ($1, $2, $3) RETURNING `+nzbColumns,
		in.LibraryID, in.MediaItemID, in.Name,
	)
	if err := scanNZBDownload(row, n); err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Store) ListNZBDownloads() ([]*NZBDownload, error) {
	rows, err := s.db.Query(`SELECT ` + nzbColumns + ` FROM nzb_downloads WHERE status != 'removed' ORDER BY added_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*NZBDownload
	for rows.Next() {
		n := &NZBDownload{}
		if err := scanNZBDownload(rows, n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) GetNZBDownload(id string) (*NZBDownload, error) {
	n := &NZBDownload{}
	err := scanNZBDownload(s.db.QueryRow(`SELECT `+nzbColumns+` FROM nzb_downloads WHERE id = $1`, id), n)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return n, nil
}

func (s *Store) UpdateNZBProgress(id string, bytesTotal, bytesDone int64, status string) error {
	_, err := s.db.Exec(`UPDATE nzb_downloads SET bytes_total = $1, bytes_done = $2, status = $3 WHERE id = $4`,
		bytesTotal, bytesDone, status, id)
	return err
}

func (s *Store) FinishNZBDownload(id string, ferr error) error {
	if ferr != nil {
		_, err := s.db.Exec(`UPDATE nzb_downloads SET status = 'error', error = $1 WHERE id = $2`, ferr.Error(), id)
		return err
	}
	_, err := s.db.Exec(`UPDATE nzb_downloads SET status = 'completed', completed_at = now() WHERE id = $1`, id)
	return err
}

func (s *Store) MarkNZBPromoted(id string) error {
	_, err := s.db.Exec(`UPDATE nzb_downloads SET promoted = true WHERE id = $1`, id)
	return err
}

func (s *Store) RemoveNZBDownload(id string) error {
	_, err := s.db.Exec(`UPDATE nzb_downloads SET status = 'removed' WHERE id = $1`, id)
	return err
}

// NZBFile is an nzb_downloads counterpart to DebridFile: one row per file
// TorBox cached, holding a direct HTTP stream URL instead of requiring Vorn
// to fetch and store the bytes itself.
type NZBFile struct {
	ID            string
	NZBDownloadID string
	Name          string
	SizeBytes     int64
	StreamURL     string
	// WebDAVURL is the stable WebDAV URL for this file, matched by size
	// after TorBox finishes caching (TorBox WebDAV uses random hash
	// filenames, so the only shared key between the API response and the
	// WebDAV listing is the file size). Empty if unmatched or not yet
	// resolved. Preferred over StreamURL (which expires) as the media_item
	// Path wherever possible.
	WebDAVURL string
}

func (s *Store) AddNZBFile(downloadID, name string, sizeBytes int64, streamURL string) (*NZBFile, error) {
	f := &NZBFile{NZBDownloadID: downloadID, Name: name, SizeBytes: sizeBytes, StreamURL: streamURL}
	err := s.db.QueryRow(
		`INSERT INTO nzb_files (nzb_download_id, name, size_bytes, stream_url) VALUES ($1, $2, $3, $4) RETURNING id`,
		downloadID, name, sizeBytes, streamURL,
	).Scan(&f.ID)
	return f, err
}

// SetNZBFileWebDAVURL records the stable WebDAV URL for an NZB file, found
// by matching the file size against WebDAV PROPFIND results after TorBox
// finishes caching (see nzb.Service.matchWebDAVURLs).
func (s *Store) SetNZBFileWebDAVURL(fileID, webdavURL string) error {
	_, err := s.db.Exec(`UPDATE nzb_files SET webdav_url = $1 WHERE id = $2`, webdavURL, fileID)
	return err
}

// FindNZBFileBySize returns the NZB file with the given size (most recent
// first) -- optionally scoped to libraryID and/or extension. libraryID
// prevents cross-library false matches; extension (e.g. ".mkv") reduces
// same-size collision risk when two completely different files happen to
// have identical byte sizes. Both are optional: empty string means "don't
// filter on that dimension." Returns ErrNotFound if no match exists.
func (s *Store) FindNZBFileBySize(libraryID string, sizeBytes int64, extension string) (*NZBFile, error) {
	// Build the query dynamically instead of maintaining 4 copy-pasted
	// branches — fewer places for the Scan columns to drift.
	var (
		clauses []string
		args    []any
		argIdx  = 1
	)
	fromClause := "FROM nzb_files nf"
	if libraryID != "" {
		fromClause += " JOIN nzb_downloads nd ON nd.id = nf.nzb_download_id"
	}
	args = append(args, sizeBytes)
	clauses = append(clauses, fmt.Sprintf("nf.size_bytes = $%d", argIdx))
	argIdx++
	if libraryID != "" {
		args = append(args, libraryID)
		clauses = append(clauses, fmt.Sprintf("nd.library_id = $%d", argIdx))
		argIdx++
	}
	if extension != "" {
		args = append(args, strings.ToLower(extension))
		clauses = append(clauses, fmt.Sprintf("lower(nf.name) LIKE '%%' || $%d", argIdx))
		argIdx++
	}

	query := fmt.Sprintf(
		`SELECT nf.id, nf.nzb_download_id, nf.name, nf.size_bytes, nf.stream_url, coalesce(nf.webdav_url, '')
		 %s WHERE %s ORDER BY nf.id DESC LIMIT 1`,
		fromClause, strings.Join(clauses, " AND "),
	)

	f := &NZBFile{}
	err := s.db.QueryRow(query, args...).Scan(&f.ID, &f.NZBDownloadID, &f.Name, &f.SizeBytes, &f.StreamURL, &f.WebDAVURL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return f, err
}

// ListNZBFileWebDAVDirs returns the unique parent directories of all
// nzb_files with a populated webdav_url, plus any WebDAV hash directories
// derived from nzb_downloads.provider_ref (the "hash" part of
// "usenetID:hash"), for the given library. These are the hash-named
// subdirectories TorBox creates for NZB-cached files that won't appear in
// a root PROPFIND, so the scanner can walk them directly.
func (s *Store) ListNZBFileWebDAVDirs(libraryID string) ([]string, error) {
	dirs := make(map[string]bool)

	// From nzb_files.webdav_url (already matched files).
	rows, err := s.db.Query(
		`SELECT DISTINCT regexp_replace(nf.webdav_url, '/[^/]+$', '/')
		 FROM nzb_files nf
		 JOIN nzb_downloads nd ON nd.id = nf.nzb_download_id
		 WHERE nd.library_id = $1 AND nf.webdav_url != ''`,
		libraryID,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return nil, err
		}
		dirs[d] = true
	}
	rows.Close()

	// From provider_ref hashes (for downloads whose files haven't been
	// WebDAV-matched yet — runTorBox stores "usenetID:hash" at creation
	// time, before matchWebDAVURLs runs). The scanner uses these to
	// discover files even when the API's mylist endpoint is empty.
	rows2, err := s.db.Query(
		`SELECT DISTINCT provider_ref
		 FROM nzb_downloads
		 WHERE library_id = $1 AND provider_ref LIKE '%:%' AND status NOT IN ('error', 'removed')`,
		libraryID,
	)
	if err != nil {
		return nil, err
	}
	defer rows2.Close()
	for rows2.Next() {
		var ref string
		if err := rows2.Scan(&ref); err != nil {
			return nil, err
		}
		if idx := strings.LastIndex(ref, ":"); idx >= 0 && idx < len(ref)-1 {
			hash := ref[idx+1:]
			// The scanner walks WebDAV folders attached to the library,
			// so the hash alone is enough — walkWebDAVDir prepends the
			// folder's URL.
			// Store just the hash, since the scanner code constructs the
			// full URL from the WebDAV folder's base URL.
			dirs[hash] = true
		}
	}

	out := make([]string, 0, len(dirs))
	for d := range dirs {
		out = append(out, d)
	}
	return out, nil
}

func (s *Store) ListNZBFiles(downloadID string) ([]*NZBFile, error) {
	rows, err := s.db.Query(`SELECT id, nzb_download_id, name, size_bytes, stream_url, coalesce(webdav_url, '') FROM nzb_files WHERE nzb_download_id = $1`, downloadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*NZBFile
	for rows.Next() {
		f := &NZBFile{}
		if err := rows.Scan(&f.ID, &f.NZBDownloadID, &f.Name, &f.SizeBytes, &f.StreamURL, &f.WebDAVURL); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

type NZBIndexer struct {
	ID        string
	Name      string
	BaseURL   string
	APIKey    string
	Enabled   bool
	CreatedAt time.Time
	// SupportsImdbSearch/SupportsTvdbSearch/DisabledReason -- see
	// TorrentIndexer's identical fields for the reasoning (same Newznab/
	// Torznab capability model).
	SupportsImdbSearch bool
	SupportsTvdbSearch bool
	DisabledReason     string
}

func (s *Store) CreateNZBIndexer(name, baseURL, apiKey string, supportsImdb, supportsTvdb bool) (*NZBIndexer, error) {
	idx := &NZBIndexer{
		Name: name, BaseURL: baseURL, APIKey: apiKey, Enabled: true,
		SupportsImdbSearch: supportsImdb, SupportsTvdbSearch: supportsTvdb,
	}
	err := s.db.QueryRow(
		`INSERT INTO nzb_indexers (name, base_url, api_key, supports_imdb_search, supports_tvdb_search)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		name, baseURL, apiKey, supportsImdb, supportsTvdb,
	).Scan(&idx.ID, &idx.CreatedAt)
	return idx, err
}

func (s *Store) ListNZBIndexers() ([]*NZBIndexer, error) {
	rows, err := s.db.Query(`SELECT id, name, base_url, api_key, enabled, created_at, supports_imdb_search, supports_tvdb_search, disabled_reason FROM nzb_indexers ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*NZBIndexer
	for rows.Next() {
		idx := &NZBIndexer{}
		if err := rows.Scan(&idx.ID, &idx.Name, &idx.BaseURL, &idx.APIKey, &idx.Enabled, &idx.CreatedAt, &idx.SupportsImdbSearch, &idx.SupportsTvdbSearch, &idx.DisabledReason); err != nil {
			return nil, err
		}
		out = append(out, idx)
	}
	return out, rows.Err()
}

func (s *Store) DeleteNZBIndexer(id string) error {
	res, err := s.db.Exec(`DELETE FROM nzb_indexers WHERE id = $1`, id)
	return checkRowsAffected(res, err)
}

// UpdateNZBIndexerInput fields are pointers so nil means "leave this field
// unchanged" -- see UpdateTorrentIndexerInput for the reasoning. A non-nil
// empty APIKey explicitly clears it.
type UpdateNZBIndexerInput struct {
	Name               *string
	BaseURL            *string
	APIKey             *string
	Enabled            *bool
	SupportsImdbSearch *bool
	SupportsTvdbSearch *bool
	DisabledReason     *string
}

func (s *Store) UpdateNZBIndexer(id string, in UpdateNZBIndexerInput) (*NZBIndexer, error) {
	idx := &NZBIndexer{}
	err := s.db.QueryRow(
		`SELECT id, name, base_url, api_key, enabled, created_at, supports_imdb_search, supports_tvdb_search, disabled_reason
		 FROM nzb_indexers WHERE id = $1`, id,
	).Scan(&idx.ID, &idx.Name, &idx.BaseURL, &idx.APIKey, &idx.Enabled, &idx.CreatedAt, &idx.SupportsImdbSearch, &idx.SupportsTvdbSearch, &idx.DisabledReason)
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
		`UPDATE nzb_indexers SET name = $1, base_url = $2, api_key = $3, enabled = $4,
		 supports_imdb_search = $5, supports_tvdb_search = $6, disabled_reason = $7 WHERE id = $8`,
		idx.Name, idx.BaseURL, idx.APIKey, idx.Enabled, idx.SupportsImdbSearch, idx.SupportsTvdbSearch, idx.DisabledReason, id,
	); err != nil {
		return nil, err
	}
	return idx, nil
}
