package store

import (
	"database/sql"
	"errors"
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
	// acquisition.Service.tryNZBCandidate) -- it's what lets nzb.Service's
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
}

const nzbColumns = `id, library_id, media_item_id, name, status, bytes_total, bytes_done, error, promoted, added_at, completed_at`

func scanNZBDownload(row interface{ Scan(...any) error }, n *NZBDownload) error {
	return row.Scan(&n.ID, &n.LibraryID, &n.MediaItemID, &n.Name, &n.Status, &n.BytesTotal, &n.BytesDone, &n.Error, &n.Promoted, &n.AddedAt, &n.CompletedAt)
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
}

func (s *Store) AddNZBFile(downloadID, name string, sizeBytes int64, streamURL string) (*NZBFile, error) {
	f := &NZBFile{NZBDownloadID: downloadID, Name: name, SizeBytes: sizeBytes, StreamURL: streamURL}
	err := s.db.QueryRow(
		`INSERT INTO nzb_files (nzb_download_id, name, size_bytes, stream_url) VALUES ($1, $2, $3, $4) RETURNING id`,
		downloadID, name, sizeBytes, streamURL,
	).Scan(&f.ID)
	return f, err
}

func (s *Store) ListNZBFiles(downloadID string) ([]*NZBFile, error) {
	rows, err := s.db.Query(`SELECT id, nzb_download_id, name, size_bytes, stream_url FROM nzb_files WHERE nzb_download_id = $1`, downloadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*NZBFile
	for rows.Next() {
		f := &NZBFile{}
		if err := rows.Scan(&f.ID, &f.NZBDownloadID, &f.Name, &f.SizeBytes, &f.StreamURL); err != nil {
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
}

func (s *Store) CreateNZBIndexer(name, baseURL, apiKey string) (*NZBIndexer, error) {
	idx := &NZBIndexer{Name: name, BaseURL: baseURL, APIKey: apiKey, Enabled: true}
	err := s.db.QueryRow(
		`INSERT INTO nzb_indexers (name, base_url, api_key) VALUES ($1, $2, $3) RETURNING id, created_at`,
		name, baseURL, apiKey,
	).Scan(&idx.ID, &idx.CreatedAt)
	return idx, err
}

func (s *Store) ListNZBIndexers() ([]*NZBIndexer, error) {
	rows, err := s.db.Query(`SELECT id, name, base_url, api_key, enabled, created_at FROM nzb_indexers ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*NZBIndexer
	for rows.Next() {
		idx := &NZBIndexer{}
		if err := rows.Scan(&idx.ID, &idx.Name, &idx.BaseURL, &idx.APIKey, &idx.Enabled, &idx.CreatedAt); err != nil {
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
	Name    *string
	BaseURL *string
	APIKey  *string
	Enabled *bool
}

func (s *Store) UpdateNZBIndexer(id string, in UpdateNZBIndexerInput) (*NZBIndexer, error) {
	idx := &NZBIndexer{}
	err := s.db.QueryRow(
		`SELECT id, name, base_url, api_key, enabled, created_at FROM nzb_indexers WHERE id = $1`, id,
	).Scan(&idx.ID, &idx.Name, &idx.BaseURL, &idx.APIKey, &idx.Enabled, &idx.CreatedAt)
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
	if _, err := s.db.Exec(
		`UPDATE nzb_indexers SET name = $1, base_url = $2, api_key = $3, enabled = $4 WHERE id = $5`,
		idx.Name, idx.BaseURL, idx.APIKey, idx.Enabled, id,
	); err != nil {
		return nil, err
	}
	return idx, nil
}
