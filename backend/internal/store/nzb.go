package store

import (
	"database/sql"
	"errors"
	"time"
)

type UsenetServer struct {
	ID             string
	Name           string
	Provider       string // "nntp" (default, real Usenet server) | "torbox"
	Host           string
	Port           int
	UseTLS         bool
	Username       string
	Password       string
	APIKey         string // torbox only
	MaxConnections int
	Enabled        bool
	CreatedAt      time.Time
}

func (s *Store) CreateUsenetServer(in UsenetServer) (*UsenetServer, error) {
	out := in
	if out.Provider == "" {
		out.Provider = "nntp"
	}
	err := s.db.QueryRow(
		`INSERT INTO usenet_servers (name, provider, host, port, use_tls, username, password, api_key, max_connections)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id, enabled, created_at`,
		out.Name, out.Provider, out.Host, out.Port, out.UseTLS, out.Username, out.Password, out.APIKey, out.MaxConnections,
	).Scan(&out.ID, &out.Enabled, &out.CreatedAt)
	return &out, err
}

func (s *Store) ListUsenetServers() ([]*UsenetServer, error) {
	rows, err := s.db.Query(
		`SELECT id, name, provider, host, port, use_tls, username, password, api_key, max_connections, enabled, created_at
		 FROM usenet_servers ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*UsenetServer
	for rows.Next() {
		u := &UsenetServer{}
		if err := rows.Scan(&u.ID, &u.Name, &u.Provider, &u.Host, &u.Port, &u.UseTLS, &u.Username, &u.Password, &u.APIKey, &u.MaxConnections, &u.Enabled, &u.CreatedAt); err != nil {
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
// unchanged" -- in particular, an admin editing host/port/username shouldn't
// have to resend the password/API key, and the API never echoes secrets
// back for them to resend in the first place. A non-nil empty
// Password/APIKey explicitly clears it.
type UpdateUsenetServerInput struct {
	Name           *string
	Provider       *string
	Host           *string
	Port           *int
	UseTLS         *bool
	Username       *string
	Password       *string
	APIKey         *string
	MaxConnections *int
	Enabled        *bool
}

func (s *Store) UpdateUsenetServer(id string, in UpdateUsenetServerInput) (*UsenetServer, error) {
	u := &UsenetServer{}
	err := s.db.QueryRow(
		`SELECT id, name, provider, host, port, use_tls, username, password, api_key, max_connections, enabled, created_at
		 FROM usenet_servers WHERE id = $1`, id,
	).Scan(&u.ID, &u.Name, &u.Provider, &u.Host, &u.Port, &u.UseTLS, &u.Username, &u.Password, &u.APIKey, &u.MaxConnections, &u.Enabled, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		u.Name = *in.Name
	}
	if in.Provider != nil {
		u.Provider = *in.Provider
	}
	if in.Host != nil {
		u.Host = *in.Host
	}
	if in.Port != nil {
		u.Port = *in.Port
	}
	if in.UseTLS != nil {
		u.UseTLS = *in.UseTLS
	}
	if in.Username != nil {
		u.Username = *in.Username
	}
	if in.Password != nil {
		u.Password = *in.Password
	}
	if in.APIKey != nil {
		u.APIKey = *in.APIKey
	}
	if in.MaxConnections != nil {
		u.MaxConnections = *in.MaxConnections
	}
	if in.Enabled != nil {
		u.Enabled = *in.Enabled
	}
	if _, err := s.db.Exec(
		`UPDATE usenet_servers SET name = $1, provider = $2, host = $3, port = $4, use_tls = $5, username = $6,
		 password = $7, api_key = $8, max_connections = $9, enabled = $10 WHERE id = $11`,
		u.Name, u.Provider, u.Host, u.Port, u.UseTLS, u.Username, u.Password, u.APIKey, u.MaxConnections, u.Enabled, id,
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
	SavePath    string
	Status      string // "downloading" | "repairing" | "completed" | "error" | "removed"
	BytesTotal  int64
	BytesDone   int64
	Error       string
	Promoted    bool
	// Provider records which kind of Usenet server actually resolved this
	// download ("nntp" | "torbox") -- set once run() picks a server, right
	// before branching into runNNTP/runTorBox. promote.go relies on this
	// (not "does nzb_files have rows") to decide whether to promote from
	// stream URLs or a local directory, since a TorBox run can legitimately
	// come back with zero cached files and must still be treated as a
	// TorBox-streamed (no local directory ever created) outcome rather than
	// falling back to NNTP's directory-walk.
	Provider    string
	AddedAt     time.Time
	CompletedAt *time.Time
}

const nzbColumns = `id, library_id, media_item_id, name, save_path, status, bytes_total, bytes_done, error, promoted, provider, added_at, completed_at`

func scanNZBDownload(row interface{ Scan(...any) error }, n *NZBDownload) error {
	return row.Scan(&n.ID, &n.LibraryID, &n.MediaItemID, &n.Name, &n.SavePath, &n.Status, &n.BytesTotal, &n.BytesDone, &n.Error, &n.Promoted, &n.Provider, &n.AddedAt, &n.CompletedAt)
}

// SetNZBDownloadProvider records which Usenet server provider resolved id,
// called once at the top of run() right after pickServer succeeds (the
// provider isn't known yet at CreateNZBDownload time, since that happens
// before a goroutine even starts).
func (s *Store) SetNZBDownloadProvider(id, provider string) error {
	_, err := s.db.Exec(`UPDATE nzb_downloads SET provider = $1 WHERE id = $2`, provider, id)
	return err
}

type CreateNZBDownloadInput struct {
	LibraryID   *string
	MediaItemID *string
	Name        string
	SavePath    string
}

func (s *Store) CreateNZBDownload(in CreateNZBDownloadInput) (*NZBDownload, error) {
	n := &NZBDownload{}
	row := s.db.QueryRow(
		`INSERT INTO nzb_downloads (library_id, media_item_id, name, save_path) VALUES ($1, $2, $3, $4) RETURNING `+nzbColumns,
		in.LibraryID, in.MediaItemID, in.Name, in.SavePath,
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

// NZBFile is an nzb_downloads counterpart to DebridFile: populated only
// when the download resolved via a TorBox-provider Usenet server, which
// (unlike plain NNTP) returns a direct HTTP stream URL per file instead of
// requiring Vorn to fetch and store the bytes itself. A download resolved
// over plain NNTP has no NZBFile rows at all.
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
