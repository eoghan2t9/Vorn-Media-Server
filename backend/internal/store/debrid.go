package store

import (
	"database/sql"
	"errors"
	"time"
)

type DebridAccount struct {
	ID        string
	Provider  string // "realdebrid" | "torbox"
	APIKey    string
	Enabled   bool
	CreatedAt time.Time
}

func (s *Store) CreateDebridAccount(provider, apiKey string) (*DebridAccount, error) {
	a := &DebridAccount{Provider: provider, APIKey: apiKey, Enabled: true}
	err := s.db.QueryRow(
		`INSERT INTO debrid_accounts (provider, api_key) VALUES ($1, $2) RETURNING id, created_at`,
		provider, apiKey,
	).Scan(&a.ID, &a.CreatedAt)
	return a, err
}

func (s *Store) ListDebridAccounts() ([]*DebridAccount, error) {
	rows, err := s.db.Query(`SELECT id, provider, api_key, enabled, created_at FROM debrid_accounts ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*DebridAccount
	for rows.Next() {
		a := &DebridAccount{}
		if err := rows.Scan(&a.ID, &a.Provider, &a.APIKey, &a.Enabled, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetDebridAccount(id string) (*DebridAccount, error) {
	a := &DebridAccount{}
	err := s.db.QueryRow(`SELECT id, provider, api_key, enabled, created_at FROM debrid_accounts WHERE id = $1`, id).
		Scan(&a.ID, &a.Provider, &a.APIKey, &a.Enabled, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (s *Store) DeleteDebridAccount(id string) error {
	res, err := s.db.Exec(`DELETE FROM debrid_accounts WHERE id = $1`, id)
	return checkRowsAffected(res, err)
}

type DebridItem struct {
	ID          string
	LibraryID   *string
	AccountID   string
	SourceRef   string
	Name        string
	Status      string // "resolving" | "ready" | "error" | "removed"
	Error       string
	Promoted    bool
	AddedAt     time.Time
	MediaItemID *string // set when this resolve is fulfilling a specific placeholder media_item, nil for manual/admin-added links
	// ProviderRef is the debrid provider's own id for this resolve (torrent
	// id, magnet id, transfer id...), set once Resolve succeeds -- empty
	// until then, and permanently empty for providers/paths that never
	// create a deletable server-side entity (e.g. Premiumize's directDL
	// fast path). Used by Service.Remove to delete it from the provider's
	// account, reclaiming storage/active-download quota.
	ProviderRef string
}

type CreateDebridItemInput struct {
	LibraryID   *string
	AccountID   string
	SourceRef   string
	Name        string
	MediaItemID *string
}

func (s *Store) CreateDebridItem(in CreateDebridItemInput) (*DebridItem, error) {
	item := &DebridItem{LibraryID: in.LibraryID, AccountID: in.AccountID, SourceRef: in.SourceRef, Name: in.Name, Status: "resolving", MediaItemID: in.MediaItemID}
	err := s.db.QueryRow(
		`INSERT INTO debrid_items (library_id, account_id, source_ref, name, media_item_id) VALUES ($1, $2, $3, $4, $5) RETURNING id, added_at`,
		in.LibraryID, in.AccountID, in.SourceRef, in.Name, in.MediaItemID,
	).Scan(&item.ID, &item.AddedAt)
	return item, err
}

const debridItemColumns = `id, library_id, account_id, source_ref, name, status, error, promoted, added_at, media_item_id, provider_ref`

func scanDebridItem(row interface{ Scan(...any) error }, item *DebridItem) error {
	return row.Scan(&item.ID, &item.LibraryID, &item.AccountID, &item.SourceRef, &item.Name, &item.Status, &item.Error, &item.Promoted, &item.AddedAt, &item.MediaItemID, &item.ProviderRef)
}

func (s *Store) ListDebridItems() ([]*DebridItem, error) {
	rows, err := s.db.Query(`SELECT ` + debridItemColumns + ` FROM debrid_items WHERE status != 'removed' ORDER BY added_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*DebridItem
	for rows.Next() {
		item := &DebridItem{}
		if err := scanDebridItem(rows, item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetDebridItem(id string) (*DebridItem, error) {
	item := &DebridItem{}
	err := scanDebridItem(s.db.QueryRow(`SELECT `+debridItemColumns+` FROM debrid_items WHERE id = $1`, id), item)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return item, nil
}

// ListDebridItemsByMediaItemID returns the most recently promoted debrid
// items for mediaItemID, ordered newest-first. Used by acquisition's fast
// re-resolution path (Reacquire) to reuse a known info-hash instead of
// searching indexers again when a previous stream URL has expired.
func (s *Store) ListDebridItemsByMediaItemID(mediaItemID string) ([]*DebridItem, error) {
	rows, err := s.db.Query(
		`SELECT `+debridItemColumns+` FROM debrid_items
		 WHERE media_item_id = $1 AND status = 'ready' AND promoted = true
		 ORDER BY added_at DESC`, mediaItemID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*DebridItem
	for rows.Next() {
		item := &DebridItem{}
		if err := scanDebridItem(rows, item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// SetDebridItemProviderRef records the provider's own id for id's resolve,
// called once Resolve succeeds (the ref isn't known at CreateDebridItem
// time, since that happens before the resolve goroutine even starts).
func (s *Store) SetDebridItemProviderRef(id, providerRef string) error {
	_, err := s.db.Exec(`UPDATE debrid_items SET provider_ref = $1 WHERE id = $2`, providerRef, id)
	return err
}

func (s *Store) FinishDebridItem(id string, ferr error) error {
	if ferr != nil {
		_, err := s.db.Exec(`UPDATE debrid_items SET status = 'error', error = $1 WHERE id = $2`, ferr.Error(), id)
		return err
	}
	_, err := s.db.Exec(`UPDATE debrid_items SET status = 'ready' WHERE id = $1`, id)
	return err
}

func (s *Store) MarkDebridItemPromoted(id string) error {
	_, err := s.db.Exec(`UPDATE debrid_items SET promoted = true WHERE id = $1`, id)
	return err
}

func (s *Store) RemoveDebridItem(id string) error {
	_, err := s.db.Exec(`UPDATE debrid_items SET status = 'removed' WHERE id = $1`, id)
	return err
}

type DebridFile struct {
	ID           string
	DebridItemID string
	Name         string
	SizeBytes    int64
	StreamURL    string
}

func (s *Store) AddDebridFile(itemID, name string, sizeBytes int64, streamURL string) (*DebridFile, error) {
	f := &DebridFile{DebridItemID: itemID, Name: name, SizeBytes: sizeBytes, StreamURL: streamURL}
	err := s.db.QueryRow(
		`INSERT INTO debrid_files (debrid_item_id, name, size_bytes, stream_url) VALUES ($1, $2, $3, $4) RETURNING id`,
		itemID, name, sizeBytes, streamURL,
	).Scan(&f.ID)
	return f, err
}

func (s *Store) ListDebridFiles(itemID string) ([]*DebridFile, error) {
	rows, err := s.db.Query(`SELECT id, debrid_item_id, name, size_bytes, stream_url FROM debrid_files WHERE debrid_item_id = $1`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*DebridFile
	for rows.Next() {
		f := &DebridFile{}
		if err := rows.Scan(&f.ID, &f.DebridItemID, &f.Name, &f.SizeBytes, &f.StreamURL); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
