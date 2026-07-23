package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/lib/pq"
)

var ErrNotFound = errors.New("store: not found")
var ErrConflict = errors.New("store: already exists")

type User struct {
	ID           string
	Username     string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    time.Time
}

func (s *Store) CreateUser(username, passwordHash string, isAdmin bool) (*User, error) {
	u := &User{Username: username, PasswordHash: passwordHash, IsAdmin: isAdmin}
	err := s.db.QueryRow(
		`INSERT INTO users (username, password_hash, is_admin) VALUES ($1, $2, $3)
		 RETURNING id, created_at`,
		username, passwordHash, isAdmin,
	).Scan(&u.ID, &u.CreatedAt)
	if isUniqueViolation(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) GetUserByUsername(username string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, is_admin, created_at FROM users WHERE username = $1`,
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) GetUserByID(id string) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(
		`SELECT id, username, password_hash, is_admin, created_at FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(`SELECT id, username, password_hash, is_admin, created_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) UpdateUserPassword(id, passwordHash string) error {
	res, err := s.db.Exec(`UPDATE users SET password_hash = $1 WHERE id = $2`, passwordHash, id)
	return checkRowsAffected(res, err)
}

func (s *Store) UpdateUserAdmin(id string, isAdmin bool) error {
	res, err := s.db.Exec(`UPDATE users SET is_admin = $1 WHERE id = $2`, isAdmin, id)
	return checkRowsAffected(res, err)
}

func (s *Store) DeleteUser(id string) error {
	res, err := s.db.Exec(`DELETE FROM users WHERE id = $1`, id)
	return checkRowsAffected(res, err)
}

func checkRowsAffected(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// isUniqueViolation reports whether err is a Postgres unique_violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}
