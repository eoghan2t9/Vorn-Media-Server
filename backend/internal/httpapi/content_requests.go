package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/metadata"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const tmdbNotConfigured = "TMDb is not configured (set a TMDb API key in Admin > Integrations)"

type discoverResultResponse struct {
	TmdbID      int    `json:"tmdbId"`
	Title       string `json:"title"`
	Overview    string `json:"overview,omitempty"`
	ReleaseDate string `json:"releaseDate,omitempty"`
	PosterURL   string `json:"posterUrl,omitempty"`
}

// handleDiscoverSearch searches TMDb directly (not Vorn's own library) so a
// user can find and request something Vorn doesn't have yet.
func (s *Server) handleDiscoverSearch(w http.ResponseWriter, r *http.Request) {
	if s.tmdb == nil {
		writeError(w, http.StatusServiceUnavailable, tmdbNotConfigured)
		return
	}
	q := r.URL.Query().Get("q")
	mediaType := r.URL.Query().Get("type")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}

	var results []metadata.SearchResult
	var err error
	switch mediaType {
	case "movie":
		results, err = s.tmdb.DiscoverMovies(r.Context(), q)
	case "series":
		results, err = s.tmdb.DiscoverSeries(r.Context(), q)
	default:
		writeError(w, http.StatusBadRequest, "type must be 'movie' or 'series'")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "searching TMDb")
		return
	}

	resp := make([]discoverResultResponse, 0, len(results))
	for _, r := range results {
		resp = append(resp, discoverResultResponse{
			TmdbID: r.TmdbID, Title: r.Title, Overview: r.Overview, ReleaseDate: r.ReleaseDate, PosterURL: r.PosterURL,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

type contentRequestResponse struct {
	ID          string  `json:"id"`
	RequestedBy string  `json:"requestedBy"`
	Requester   string  `json:"requester"`
	MediaType   string  `json:"mediaType"`
	TmdbID      int     `json:"tmdbId"`
	Title       string  `json:"title"`
	Overview    string  `json:"overview,omitempty"`
	ReleaseDate string  `json:"releaseDate,omitempty"`
	PosterURL   string  `json:"posterUrl,omitempty"`
	Status      string  `json:"status"`
	DecidedAt   *string `json:"decidedAt,omitempty"`
	CreatedAt   string  `json:"createdAt"`
}

func toContentRequestResponse(r *store.ContentRequest) contentRequestResponse {
	resp := contentRequestResponse{
		ID:          r.ID,
		RequestedBy: r.RequestedBy,
		Requester:   r.RequestedByUsername,
		MediaType:   r.MediaType,
		TmdbID:      r.TmdbID,
		Title:       r.Title,
		Overview:    r.Overview,
		ReleaseDate: r.ReleaseDate,
		PosterURL:   r.PosterURL,
		Status:      r.Status,
		CreatedAt:   r.CreatedAt.Format(time.RFC3339),
	}
	if r.DecidedAt != nil {
		s := r.DecidedAt.Format(time.RFC3339)
		resp.DecidedAt = &s
	}
	return resp
}

type createContentRequestRequest struct {
	MediaType   string `json:"mediaType"`
	TmdbID      int    `json:"tmdbId"`
	Title       string `json:"title"`
	Overview    string `json:"overview"`
	ReleaseDate string `json:"releaseDate"`
	PosterURL   string `json:"posterUrl"`
}

// handleCreateContentRequest takes the title fields straight from the
// client's own discover-search result rather than re-querying TMDb
// server-side -- the client already has exactly the record the user picked.
func (s *Server) handleCreateContentRequest(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	var req createContentRequestRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.MediaType != "movie" && req.MediaType != "series" {
		writeError(w, http.StatusBadRequest, "mediaType must be 'movie' or 'series'")
		return
	}
	if req.TmdbID == 0 || req.Title == "" {
		writeError(w, http.StatusBadRequest, "tmdbId and title are required")
		return
	}

	created, err := s.store.CreateContentRequest(store.CreateContentRequestInput{
		RequestedBy: user.ID,
		MediaType:   req.MediaType,
		TmdbID:      req.TmdbID,
		Title:       req.Title,
		Overview:    req.Overview,
		ReleaseDate: req.ReleaseDate,
		PosterURL:   req.PosterURL,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "this title has already been requested")
			return
		}
		writeError(w, http.StatusInternalServerError, "creating request")
		return
	}
	writeJSON(w, http.StatusCreated, toContentRequestResponse(created))
}

// handleListMyContentRequests is the viewer's own "my requests" list.
func (s *Server) handleListMyContentRequests(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	requests, err := s.store.ListContentRequestsByUser(user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing requests")
		return
	}
	resp := make([]contentRequestResponse, 0, len(requests))
	for _, req := range requests {
		resp = append(resp, toContentRequestResponse(req))
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleDeleteContentRequest lets a user withdraw their own still-pending
// request -- not admins acting on someone else's, and not one already
// decided (approving/declining is done via the admin endpoint below).
func (s *Server) handleDeleteContentRequest(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	id := r.PathValue("id")

	existing, err := s.store.GetContentRequest(id)
	if err != nil {
		s.writeStoreErr(w, err, "loading request")
		return
	}
	if existing.RequestedBy != user.ID {
		writeError(w, http.StatusForbidden, "you can only withdraw your own requests")
		return
	}
	if existing.Status != "pending" {
		writeError(w, http.StatusBadRequest, "only pending requests can be withdrawn")
		return
	}
	if err := s.store.DeleteContentRequest(id); err != nil {
		s.writeStoreErr(w, err, "withdrawing request")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListAdminContentRequests is the admin review queue, optionally
// filtered by ?status=pending.
func (s *Server) handleListAdminContentRequests(w http.ResponseWriter, r *http.Request) {
	requests, err := s.store.ListContentRequests(r.URL.Query().Get("status"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing requests")
		return
	}
	resp := make([]contentRequestResponse, 0, len(requests))
	for _, req := range requests {
		resp = append(resp, toContentRequestResponse(req))
	}
	writeJSON(w, http.StatusOK, resp)
}

type decideContentRequestRequest struct {
	Status string `json:"status"`
}

func (s *Server) handleDecideContentRequest(w http.ResponseWriter, r *http.Request) {
	admin := userFromContext(r.Context())
	id := r.PathValue("id")

	var req decideContentRequestRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Status != "approved" && req.Status != "declined" {
		writeError(w, http.StatusBadRequest, "status must be 'approved' or 'declined'")
		return
	}

	updated, err := s.store.DecideContentRequest(id, req.Status, admin.ID)
	if err != nil {
		s.writeStoreErr(w, err, "deciding request")
		return
	}
	writeJSON(w, http.StatusOK, toContentRequestResponse(updated))
}
