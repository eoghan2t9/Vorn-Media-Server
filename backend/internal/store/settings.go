package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

func (s *Store) GetSetting(key string, out any) (bool, error) {
	var raw []byte
	err := s.db.QueryRow(`SELECT value FROM server_settings WHERE key = $1`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) SetSetting(key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO server_settings (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		key, raw,
	)
	return err
}
