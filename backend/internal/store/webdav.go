package store

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"time"
)

// WebDAVFolder maps one WebDAV server root URL to a library -- the scanner
// lists files from it via PROPFIND alongside any local filesystem folders,
// and each discovered file is promoted into a media_item with its full
// WebDAV URL stored as the Path (see internal/webdav for the client).
type WebDAVFolder struct {
	ID        string
	LibraryID string
	URL       string
	APIKey    string
	Enabled   bool
	CreatedAt time.Time
}

func (s *Store) CreateWebDAVFolder(libraryID, url, apiKey string) (*WebDAVFolder, error) {
	f := &WebDAVFolder{LibraryID: libraryID, URL: url, APIKey: apiKey, Enabled: true}
	err := s.db.QueryRow(
		`INSERT INTO webdav_folders (library_id, url, api_key) VALUES ($1, $2, $3) RETURNING id, created_at`,
		libraryID, url, apiKey,
	).Scan(&f.ID, &f.CreatedAt)
	return f, err
}

func (s *Store) ListWebDAVFolders() ([]*WebDAVFolder, error) {
	rows, err := s.db.Query(
		`SELECT id, library_id, url, api_key, enabled, created_at FROM webdav_folders ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*WebDAVFolder
	for rows.Next() {
		f := &WebDAVFolder{}
		if err := rows.Scan(&f.ID, &f.LibraryID, &f.URL, &f.APIKey, &f.Enabled, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ListWebDAVFoldersByLibrary returns every (enabled or not) webdav folder
// for libraryID -- the scanner uses this to decide what to list alongside
// local folders.
func (s *Store) ListWebDAVFoldersByLibrary(libraryID string) ([]*WebDAVFolder, error) {
	rows, err := s.db.Query(
		`SELECT id, library_id, url, api_key, enabled, created_at FROM webdav_folders WHERE library_id = $1 ORDER BY created_at`,
		libraryID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*WebDAVFolder
	for rows.Next() {
		f := &WebDAVFolder{}
		if err := rows.Scan(&f.ID, &f.LibraryID, &f.URL, &f.APIKey, &f.Enabled, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetWebDAVFolderByURL returns the first enabled webdav folder whose URL is
// a prefix of candidate (e.g. candidate "https://webdav.torbox.app/Movies/
// Foo.mkv" matches a folder with url "https://webdav.torbox.app"), so the
// streaming proxy can look up the API key for any webdav-backed media_item
// by its stored Path. Returns ErrNotFound when no match.
func (s *Store) GetWebDAVFolderByURL(candidate string) (*WebDAVFolder, error) {
	rows, err := s.db.Query(
		`SELECT id, library_id, url, api_key, enabled, created_at FROM webdav_folders WHERE enabled = true ORDER BY created_at`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		f := &WebDAVFolder{}
		if err := rows.Scan(&f.ID, &f.LibraryID, &f.URL, &f.APIKey, &f.Enabled, &f.CreatedAt); err != nil {
			return nil, err
		}
		if len(candidate) >= len(f.URL) && candidate[:len(f.URL)] == f.URL {
			return f, nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nil, ErrNotFound
}

// WebDAVProbeHeaders returns the HTTP headers a caller needs to
// authenticate a request (ffprobe, or any other direct GET) against path if
// it's a WebDAV-backed URL, or nil if it isn't (a plain debrid/NZB CDN URL,
// which already embeds its own auth in the URL and needs nothing extra).
// Same "torbox" Basic Auth username convention as
// httpapi.handleDirectStream's proxy uses for the same URLs -- centralized
// here so every other caller that probes a stored media_item/scan path
// (transcode.Probe and friends) doesn't have to duplicate the WebDAV-folder
// lookup and header construction, which had silently only ever been done in
// the streaming-proxy path and nowhere else, making every ffprobe call
// against a WebDAV URL fail with 401.
func (s *Store) WebDAVProbeHeaders(path string) map[string]string {
	wf, err := s.GetWebDAVFolderByURL(path)
	if err != nil {
		return nil
	}
	token := base64.StdEncoding.EncodeToString([]byte("torbox:" + wf.APIKey))
	return map[string]string{"Authorization": "Basic " + token}
}

func (s *Store) GetWebDAVFolder(id string) (*WebDAVFolder, error) {
	f := &WebDAVFolder{}
	err := s.db.QueryRow(
		`SELECT id, library_id, url, api_key, enabled, created_at FROM webdav_folders WHERE id = $1`, id,
	).Scan(&f.ID, &f.LibraryID, &f.URL, &f.APIKey, &f.Enabled, &f.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (s *Store) UpdateWebDAVFolder(id, url, apiKey string, enabled *bool) error {
	if url != "" {
		res, err := s.db.Exec(`UPDATE webdav_folders SET url = $1 WHERE id = $2`, url, id)
		if err := checkRowsAffected(res, err); err != nil {
			return err
		}
	}
	if apiKey != "" {
		res, err := s.db.Exec(`UPDATE webdav_folders SET api_key = $1 WHERE id = $2`, apiKey, id)
		if err := checkRowsAffected(res, err); err != nil {
			return err
		}
	}
	if enabled != nil {
		res, err := s.db.Exec(`UPDATE webdav_folders SET enabled = $1 WHERE id = $2`, *enabled, id)
		if err := checkRowsAffected(res, err); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeleteWebDAVFolder(id string) error {
	res, err := s.db.Exec(`DELETE FROM webdav_folders WHERE id = $1`, id)
	return checkRowsAffected(res, err)
}
