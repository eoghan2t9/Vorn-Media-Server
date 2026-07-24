package store

import (
	"database/sql"
	"errors"
	"time"
)

type UsenetServer struct {
	ID             string
	Name           string
	Host           string
	Port           int
	UseTLS         bool
	Username       string
	Password       string
	MaxConnections int
	Enabled        bool
	CreatedAt      time.Time
}

func (s *Store) CreateUsenetServer(in UsenetServer) (*UsenetServer, error) {
	out := in
	err := s.db.QueryRow(
		`INSERT INTO usenet_servers (name, host, port, use_tls, username, password, max_connections)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, enabled, created_at`,
		in.Name, in.Host, in.Port, in.UseTLS, in.Username, in.Password, in.MaxConnections,
	).Scan(&out.ID, &out.Enabled, &out.CreatedAt)
	return &out, err
}

func (s *Store) ListUsenetServers() ([]*UsenetServer, error) {
	rows, err := s.db.Query(
		`SELECT id, name, host, port, use_tls, username, password, max_connections, enabled, created_at
		 FROM usenet_servers ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*UsenetServer
	for rows.Next() {
		u := &UsenetServer{}
		if err := rows.Scan(&u.ID, &u.Name, &u.Host, &u.Port, &u.UseTLS, &u.Username, &u.Password, &u.MaxConnections, &u.Enabled, &u.CreatedAt); err != nil {
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

type NZBDownload struct {
	ID          string
	LibraryID   *string
	Name        string
	SavePath    string
	Status      string // "downloading" | "repairing" | "completed" | "error" | "removed"
	BytesTotal  int64
	BytesDone   int64
	Error       string
	Promoted    bool
	AddedAt     time.Time
	CompletedAt *time.Time
}

const nzbColumns = `id, library_id, name, save_path, status, bytes_total, bytes_done, error, promoted, added_at, completed_at`

func scanNZBDownload(row interface{ Scan(...any) error }, n *NZBDownload) error {
	return row.Scan(&n.ID, &n.LibraryID, &n.Name, &n.SavePath, &n.Status, &n.BytesTotal, &n.BytesDone, &n.Error, &n.Promoted, &n.AddedAt, &n.CompletedAt)
}

type CreateNZBDownloadInput struct {
	LibraryID *string
	Name      string
	SavePath  string
}

func (s *Store) CreateNZBDownload(in CreateNZBDownloadInput) (*NZBDownload, error) {
	n := &NZBDownload{}
	row := s.db.QueryRow(
		`INSERT INTO nzb_downloads (library_id, name, save_path) VALUES ($1, $2, $3) RETURNING `+nzbColumns,
		in.LibraryID, in.Name, in.SavePath,
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
