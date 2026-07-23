package store

import (
	"database/sql"
	"errors"
	"time"
)

type Library struct {
	ID        string
	Name      string
	Type      string // "movie" | "series" | "music" | "audiobook" (see httpapi.validLibraryTypes)
	CreatedAt time.Time
	Folders   []string
}

func (s *Store) CreateLibrary(name, kind string, folders []string) (*Library, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	lib := &Library{Name: name, Type: kind}
	err = tx.QueryRow(
		`INSERT INTO libraries (name, type) VALUES ($1, $2) RETURNING id, created_at`,
		name, kind,
	).Scan(&lib.ID, &lib.CreatedAt)
	if err != nil {
		return nil, err
	}

	for _, f := range folders {
		if _, err := tx.Exec(`INSERT INTO library_folders (library_id, path) VALUES ($1, $2)`, lib.ID, f); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	lib.Folders = folders
	return lib, nil
}

func (s *Store) ListLibraries() ([]*Library, error) {
	rows, err := s.db.Query(`SELECT id, name, type, created_at FROM libraries ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var libs []*Library
	for rows.Next() {
		l := &Library{}
		if err := rows.Scan(&l.ID, &l.Name, &l.Type, &l.CreatedAt); err != nil {
			return nil, err
		}
		libs = append(libs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, l := range libs {
		folders, err := s.listLibraryFolders(l.ID)
		if err != nil {
			return nil, err
		}
		l.Folders = folders
	}
	return libs, nil
}

func (s *Store) GetLibrary(id string) (*Library, error) {
	l := &Library{}
	err := s.db.QueryRow(
		`SELECT id, name, type, created_at FROM libraries WHERE id = $1`, id,
	).Scan(&l.ID, &l.Name, &l.Type, &l.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	folders, err := s.listLibraryFolders(id)
	if err != nil {
		return nil, err
	}
	l.Folders = folders
	return l, nil
}

func (s *Store) listLibraryFolders(libraryID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT path FROM library_folders WHERE library_id = $1 ORDER BY created_at`, libraryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var folders []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		folders = append(folders, p)
	}
	return folders, rows.Err()
}

// UpdateLibrary renames a library and/or replaces its folder mappings
// entirely (pass nil to leave folders untouched).
func (s *Store) UpdateLibrary(id string, name string, folders []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if name != "" {
		res, err := tx.Exec(`UPDATE libraries SET name = $1 WHERE id = $2`, name, id)
		if err := checkRowsAffected(res, err); err != nil {
			return err
		}
	}

	if folders != nil {
		if _, err := tx.Exec(`DELETE FROM library_folders WHERE library_id = $1`, id); err != nil {
			return err
		}
		for _, f := range folders {
			if _, err := tx.Exec(`INSERT INTO library_folders (library_id, path) VALUES ($1, $2)`, id, f); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func (s *Store) DeleteLibrary(id string) error {
	res, err := s.db.Exec(`DELETE FROM libraries WHERE id = $1`, id)
	return checkRowsAffected(res, err)
}

// SetUserLibraryPermissions replaces the full set of libraries a user can access.
// Admins bypass this table entirely and see every library.
func (s *Store) SetUserLibraryPermissions(userID string, libraryIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM library_permissions WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, libID := range libraryIDs {
		if _, err := tx.Exec(
			`INSERT INTO library_permissions (user_id, library_id) VALUES ($1, $2)`,
			userID, libID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetUserLibraryPermissions(userID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT library_id FROM library_permissions WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
