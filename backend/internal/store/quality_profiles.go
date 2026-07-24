package store

import (
	"database/sql"
	"errors"
	"time"
)

// QualityProfile controls automatic release selection for a library's
// on-demand acquisitions. A library with no row in quality_profiles gets
// these exact defaults (see GetQualityProfile) rather than needing a row
// inserted at library-creation time.
type QualityProfile struct {
	LibraryID      string
	MinResolution  string // "480p" | "720p" | "1080p" | "2160p"
	MaxResolution  string
	PreferredCodec string // "" | "x264" | "x265" | "av1"
	MinSeeders     int
	PreferRemux    bool
	UpdatedAt      time.Time
}

func defaultQualityProfile(libraryID string) QualityProfile {
	return QualityProfile{
		LibraryID:     libraryID,
		MinResolution: "720p",
		MaxResolution: "2160p",
		MinSeeders:    1,
	}
}

// GetQualityProfile returns libraryID's saved quality profile, or hardcoded
// defaults if none has been configured yet.
func (s *Store) GetQualityProfile(libraryID string) (QualityProfile, error) {
	p := QualityProfile{LibraryID: libraryID}
	err := s.db.QueryRow(
		`SELECT library_id, min_resolution, max_resolution, preferred_codec, min_seeders, prefer_remux, updated_at
		 FROM quality_profiles WHERE library_id = $1`,
		libraryID,
	).Scan(&p.LibraryID, &p.MinResolution, &p.MaxResolution, &p.PreferredCodec, &p.MinSeeders, &p.PreferRemux, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultQualityProfile(libraryID), nil
	}
	if err != nil {
		return QualityProfile{}, err
	}
	return p, nil
}

// UpsertQualityProfile creates or replaces libraryID's quality profile.
func (s *Store) UpsertQualityProfile(p QualityProfile) (QualityProfile, error) {
	out := p
	err := s.db.QueryRow(
		`INSERT INTO quality_profiles (library_id, min_resolution, max_resolution, preferred_codec, min_seeders, prefer_remux)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (library_id) DO UPDATE SET
		   min_resolution = excluded.min_resolution, max_resolution = excluded.max_resolution,
		   preferred_codec = excluded.preferred_codec, min_seeders = excluded.min_seeders,
		   prefer_remux = excluded.prefer_remux, updated_at = now()
		 RETURNING updated_at`,
		p.LibraryID, p.MinResolution, p.MaxResolution, p.PreferredCodec, p.MinSeeders, p.PreferRemux,
	).Scan(&out.UpdatedAt)
	return out, err
}
