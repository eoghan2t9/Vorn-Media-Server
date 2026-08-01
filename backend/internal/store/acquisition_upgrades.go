package store

import "time"

// AcquisitionUpgrade represents a single quality upgrade event recorded
// whenever the monitor's checkUpgrade successfully swaps in a better
// release for a monitored item.
type AcquisitionUpgrade struct {
	ID         int       `json:"id"`
	ItemID     string    `json:"itemId"`
	Title      string    `json:"title"`
	OldRelease string    `json:"oldRelease"`
	NewRelease string    `json:"newRelease"`
	Source     string    `json:"source"` // "torrent" or "nzb"
	CreatedAt  time.Time `json:"createdAt"`
}

// RecordUpgrade writes a new upgrade event. Called from the acquisition
// monitor's checkUpgrade when it successfully swaps in a better release.
func (s *Store) RecordUpgrade(itemID, title, oldRelease, newRelease, source string) error {
	_, err := s.db.Exec(`INSERT INTO acquisition_upgrades (item_id, title, old_release, new_release, source) VALUES ($1, $2, $3, $4, $5)`,
		itemID, title, oldRelease, newRelease, source)
	return err
}

// ListUpgrades returns recent upgrade events ordered by creation time
// (newest first), limited to the given count. Admin dashboard use only.
func (s *Store) ListUpgrades(limit int) ([]*AcquisitionUpgrade, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT id, item_id, title, old_release, new_release, source, created_at FROM acquisition_upgrades ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*AcquisitionUpgrade
	for rows.Next() {
		u := &AcquisitionUpgrade{}
		if err := rows.Scan(&u.ID, &u.ItemID, &u.Title, &u.OldRelease, &u.NewRelease, &u.Source, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
