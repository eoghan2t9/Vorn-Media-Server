package store

import (
	"database/sql"
	"errors"
	"time"
)

type Session struct {
	UserID    string
	ExpiresAt time.Time
}

func (s *Store) CreateSession(tokenHash, userID string, expiresAt time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		tokenHash, userID, expiresAt,
	)
	return err
}

func (s *Store) GetSession(tokenHash string) (*Session, error) {
	sess := &Session{}
	err := s.db.QueryRow(
		`SELECT user_id, expires_at FROM sessions WHERE token_hash = $1 AND expires_at > now()`,
		tokenHash,
	).Scan(&sess.UserID, &sess.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) DeleteSession(tokenHash string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}

func (s *Store) DeleteExpiredSessions() error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at <= now()`)
	return err
}
