package store

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

type ScanJob struct {
	ID          string
	LibraryID   string
	Kind        string // "real" | "synthetic"
	Status      string // "running" | "completed" | "failed"
	FilesFound  int64
	FilesSynced int64
	Error       *string
	StartedAt   time.Time
	FinishedAt  *time.Time
}

func (s *Store) CreateScanJob(libraryID, kind string) (*ScanJob, error) {
	j := &ScanJob{LibraryID: libraryID, Kind: kind, Status: "running"}
	err := s.db.QueryRow(
		`INSERT INTO scan_jobs (library_id, kind) VALUES ($1, $2) RETURNING id, started_at`,
		libraryID, kind,
	).Scan(&j.ID, &j.StartedAt)
	if err != nil {
		return nil, err
	}
	return j, nil
}

func (s *Store) SetScanJobCounts(id string, found, synced int64) error {
	_, err := s.db.Exec(
		`UPDATE scan_jobs SET files_found = $1, files_synced = $2 WHERE id = $3`,
		found, synced, id,
	)
	return err
}

func (s *Store) FinishScanJob(id string, scanErr error) error {
	status := "completed"
	var errMsg *string
	if scanErr != nil {
		status = "failed"
		msg := scanErr.Error()
		errMsg = &msg
	}
	_, err := s.db.Exec(
		`UPDATE scan_jobs SET status = $1, error = $2, finished_at = now() WHERE id = $3`,
		status, errMsg, id,
	)
	return err
}

func (s *Store) GetScanJob(id string) (*ScanJob, error) {
	j := &ScanJob{}
	err := s.db.QueryRow(
		`SELECT id, library_id, kind, status, files_found, files_synced, error, started_at, finished_at
		 FROM scan_jobs WHERE id = $1`, id,
	).Scan(&j.ID, &j.LibraryID, &j.Kind, &j.Status, &j.FilesFound, &j.FilesSynced, &j.Error, &j.StartedAt, &j.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return j, nil
}

func (s *Store) ListScanJobs(libraryID string, limit int) ([]*ScanJob, error) {
	rows, err := s.db.Query(
		`SELECT id, library_id, kind, status, files_found, files_synced, error, started_at, finished_at
		 FROM scan_jobs WHERE ($1 = '' OR library_id = $1) ORDER BY started_at DESC LIMIT $2`,
		libraryID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*ScanJob
	for rows.Next() {
		j := &ScanJob{}
		if err := rows.Scan(&j.ID, &j.LibraryID, &j.Kind, &j.Status, &j.FilesFound, &j.FilesSynced, &j.Error, &j.StartedAt, &j.FinishedAt); err != nil {
			return nil, err
		}
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

type ScanFileInsert struct {
	LibraryID     string
	ScanJobID     string
	Path          string
	SizeBytes     int64
	ModifiedAt    *time.Time
	GuessedKind   string
	GuessedTitle  string
	GuessedYear   *int
	SeasonNumber  *int
	EpisodeNumber *int
}

// UpsertScanFiles batch-inserts discovered file candidates in a single
// statement, decoupling flush throughput from per-row round trips. Existing
// rows for the same (library_id, path) are refreshed in place.
func (s *Store) UpsertScanFiles(batch []ScanFileInsert) error {
	if len(batch) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString(`INSERT INTO scan_files
		(library_id, scan_job_id, path, size_bytes, modified_at, guessed_kind, guessed_title, guessed_year, season_number, episode_number)
		VALUES `)
	args := make([]any, 0, len(batch)*10)
	for i, f := range batch {
		if i > 0 {
			sb.WriteString(",")
		}
		base := i * 10
		sb.WriteString(placeholders(base+1, 10))
		args = append(args, f.LibraryID, f.ScanJobID, f.Path, f.SizeBytes, f.ModifiedAt,
			f.GuessedKind, f.GuessedTitle, f.GuessedYear, f.SeasonNumber, f.EpisodeNumber)
	}
	sb.WriteString(` ON CONFLICT (library_id, path) DO UPDATE SET
		scan_job_id = EXCLUDED.scan_job_id,
		size_bytes = EXCLUDED.size_bytes,
		modified_at = EXCLUDED.modified_at,
		guessed_kind = EXCLUDED.guessed_kind,
		guessed_title = EXCLUDED.guessed_title,
		guessed_year = EXCLUDED.guessed_year,
		season_number = EXCLUDED.season_number,
		episode_number = EXCLUDED.episode_number`)

	_, err := s.db.Exec(sb.String(), args...)
	return err
}

func (s *Store) CountScanFiles(libraryID string) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT count(*) FROM scan_files WHERE library_id = $1`, libraryID).Scan(&n)
	return n, err
}

func placeholders(start, count int) string {
	var sb strings.Builder
	sb.WriteString("(")
	for i := 0; i < count; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("$")
		sb.WriteString(strconv.Itoa(start + i))
	}
	sb.WriteString(")")
	return sb.String()
}
