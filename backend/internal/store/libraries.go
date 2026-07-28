package store

import (
	"database/sql"
	"errors"
	"time"
)

type Library struct {
	ID                   string
	Name                 string
	Type                 string // "movie" | "series" | "music" | "audiobook" (see httpapi.validLibraryTypes)
	Is4K                 bool   // purely a display label -- see the 000015 migration's comment
	DefaultRequestTarget bool   // where a content request auto-fulfills into, see 000016
	CreatedAt            time.Time
	Folders              []string
}

func (s *Store) CreateLibrary(name, kind string, folders []string, is4K bool) (*Library, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	lib := &Library{Name: name, Type: kind, Is4K: is4K}
	err = tx.QueryRow(
		`INSERT INTO libraries (name, type, is_4k) VALUES ($1, $2, $3) RETURNING id, created_at`,
		name, kind, is4K,
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
	rows, err := s.db.Query(`SELECT id, name, type, is_4k, default_request_target, created_at FROM libraries ORDER BY sort_order, created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var libs []*Library
	for rows.Next() {
		l := &Library{}
		if err := rows.Scan(&l.ID, &l.Name, &l.Type, &l.Is4K, &l.DefaultRequestTarget, &l.CreatedAt); err != nil {
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
		`SELECT id, name, type, is_4k, default_request_target, created_at FROM libraries WHERE id = $1`, id,
	).Scan(&l.ID, &l.Name, &l.Type, &l.Is4K, &l.DefaultRequestTarget, &l.CreatedAt)
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

	// Non-nil even when empty -- a library with no folders (a debrid-only
	// target) is legitimate since folders became optional for movie/series
	// libraries, and a nil slice here would marshal to JSON `null`, which
	// AdminLibraries.tsx's `l.folders.join(', ')` can't handle.
	folders := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		folders = append(folders, p)
	}
	return folders, rows.Err()
}

// UpdateLibrary renames a library, replaces its folder mappings entirely
// (pass nil to leave folders untouched), and/or changes its 4K label (pass
// nil to leave it untouched).
func (s *Store) UpdateLibrary(id string, name string, folders []string, is4K *bool) error {
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

	if is4K != nil {
		res, err := tx.Exec(`UPDATE libraries SET is_4k = $1 WHERE id = $2`, *is4K, id)
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

// SetLibraryDefaultRequestTarget marks (or unmarks) id as the default
// library a content request fans out into for its (type, is_4k) group.
// Setting it true first clears any other library already default in that
// same group, so the admin can just click a new one rather than having to
// unset the old default first to dodge the partial unique index.
func (s *Store) SetLibraryDefaultRequestTarget(id string, isDefault bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if isDefault {
		if _, err := tx.Exec(
			`UPDATE libraries SET default_request_target = false
			 WHERE type = (SELECT type FROM libraries WHERE id = $1)
			   AND is_4k = (SELECT is_4k FROM libraries WHERE id = $1)`,
			id,
		); err != nil {
			return err
		}
	}

	res, err := tx.Exec(`UPDATE libraries SET default_request_target = $1 WHERE id = $2`, isDefault, id)
	if err := checkRowsAffected(res, err); err != nil {
		return err
	}
	return tx.Commit()
}

// ListDefaultRequestTargets returns the default library for each (is_4k)
// bucket of mediaType -- 0, 1, or 2 rows (standard and/or 4K), whichever an
// admin has configured.
func (s *Store) ListDefaultRequestTargets(mediaType string) ([]*Library, error) {
	rows, err := s.db.Query(
		`SELECT id, name, type, is_4k, default_request_target, created_at FROM libraries
		 WHERE type = $1 AND default_request_target = true ORDER BY is_4k`,
		mediaType,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var libs []*Library
	for rows.Next() {
		l := &Library{}
		if err := rows.Scan(&l.ID, &l.Name, &l.Type, &l.Is4K, &l.DefaultRequestTarget, &l.CreatedAt); err != nil {
			return nil, err
		}
		libs = append(libs, l)
	}
	return libs, rows.Err()
}

// ReorderLibraries persists the display order an admin dragged/moved
// libraries into on the admin Libraries page -- orderedIDs is every
// library's ID in its new order, and each one's sort_order becomes its
// index. ListLibraries (which drives the viewer Home page) sorts by this
// column first, falling back to created_at for any not included here (e.g.
// a library created concurrently with the reorder).
func (s *Store) ReorderLibraries(orderedIDs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i, id := range orderedIDs {
		if _, err := tx.Exec(`UPDATE libraries SET sort_order = $1 WHERE id = $2`, i, id); err != nil {
			return err
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
