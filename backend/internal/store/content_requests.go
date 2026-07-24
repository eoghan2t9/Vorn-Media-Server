package store

import (
	"database/sql"
	"errors"
	"time"
)

// ContentRequest is a user's ask for a movie/series to be added, identified
// by its TMDb ID (not a media_items row) since the whole point is
// requesting titles Vorn doesn't have yet. RequestedByUsername is always
// populated via a join, since a request is never useful to display without
// knowing who asked.
type ContentRequest struct {
	ID                  string
	RequestedBy         string
	RequestedByUsername string
	MediaType           string // "movie" | "series"
	TmdbID              int
	Title               string
	Overview            string
	ReleaseDate         string
	PosterURL           string
	Status              string // "pending" | "approved" | "declined"
	DecidedBy           *string
	DecidedAt           *time.Time
	CreatedAt           time.Time
}

const contentRequestColumns = `cr.id, cr.requested_by, u.username, cr.media_type, cr.tmdb_id, cr.title,
	cr.overview, cr.release_date, cr.poster_url, cr.status, cr.decided_by, cr.decided_at, cr.created_at`

const contentRequestFrom = `content_requests cr JOIN users u ON u.id = cr.requested_by`

func scanContentRequest(row interface{ Scan(...any) error }, r *ContentRequest) error {
	return row.Scan(&r.ID, &r.RequestedBy, &r.RequestedByUsername, &r.MediaType, &r.TmdbID, &r.Title,
		&r.Overview, &r.ReleaseDate, &r.PosterURL, &r.Status, &r.DecidedBy, &r.DecidedAt, &r.CreatedAt)
}

type CreateContentRequestInput struct {
	RequestedBy string
	MediaType   string
	TmdbID      int
	Title       string
	Overview    string
	ReleaseDate string
	PosterURL   string
}

// CreateContentRequest returns ErrConflict if there's already a pending
// request for the same title (see the partial unique index added in
// migration 000010), so a second person asking for the same thing gets a
// clear "already requested" instead of a silently duplicated row.
func (s *Store) CreateContentRequest(in CreateContentRequestInput) (*ContentRequest, error) {
	var id string
	err := s.db.QueryRow(
		`INSERT INTO content_requests (requested_by, media_type, tmdb_id, title, overview, release_date, poster_url)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		in.RequestedBy, in.MediaType, in.TmdbID, in.Title, in.Overview, in.ReleaseDate, in.PosterURL,
	).Scan(&id)
	if isUniqueViolation(err) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	return s.GetContentRequest(id)
}

func (s *Store) GetContentRequest(id string) (*ContentRequest, error) {
	r := &ContentRequest{}
	row := s.db.QueryRow(`SELECT `+contentRequestColumns+` FROM `+contentRequestFrom+` WHERE cr.id = $1`, id)
	if err := scanContentRequest(row, r); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return r, nil
}

// ListContentRequests is the admin view: every request, optionally
// filtered by status ("" means all), newest first.
func (s *Store) ListContentRequests(status string) ([]*ContentRequest, error) {
	query := `SELECT ` + contentRequestColumns + ` FROM ` + contentRequestFrom
	args := []any{}
	if status != "" {
		query += ` WHERE cr.status = $1`
		args = append(args, status)
	}
	query += ` ORDER BY cr.created_at DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ContentRequest
	for rows.Next() {
		r := &ContentRequest{}
		if err := scanContentRequest(rows, r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListContentRequestsByUser is the viewer's own "my requests" view.
func (s *Store) ListContentRequestsByUser(userID string) ([]*ContentRequest, error) {
	rows, err := s.db.Query(
		`SELECT `+contentRequestColumns+` FROM `+contentRequestFrom+`
		 WHERE cr.requested_by = $1 ORDER BY cr.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ContentRequest
	for rows.Next() {
		r := &ContentRequest{}
		if err := scanContentRequest(rows, r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DecideContentRequest marks a request approved/declined.
func (s *Store) DecideContentRequest(id, status, decidedBy string) (*ContentRequest, error) {
	res, err := s.db.Exec(
		`UPDATE content_requests SET status = $1, decided_by = $2, decided_at = now() WHERE id = $3`,
		status, decidedBy, id,
	)
	if err := checkRowsAffected(res, err); err != nil {
		return nil, err
	}
	return s.GetContentRequest(id)
}

// DeleteContentRequest lets a user withdraw their own pending request.
func (s *Store) DeleteContentRequest(id string) error {
	res, err := s.db.Exec(`DELETE FROM content_requests WHERE id = $1`, id)
	return checkRowsAffected(res, err)
}
