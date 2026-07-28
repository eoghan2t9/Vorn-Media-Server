package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/metadata"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const tmdbNotConfigured = "TMDb is not configured (set a TMDb API key in Admin > Integrations)"

type discoverResultResponse struct {
	TmdbID      int     `json:"tmdbId"`
	Title       string  `json:"title"`
	Overview    string  `json:"overview,omitempty"`
	ReleaseDate string  `json:"releaseDate,omitempty"`
	PosterURL   string  `json:"posterUrl,omitempty"`
	Rating      float64 `json:"rating,omitempty"`
}

// handleDiscoverSearch searches TMDb directly (not Vorn's own library) so a
// user can find and request something Vorn doesn't have yet.
func (s *Server) handleDiscoverSearch(w http.ResponseWriter, r *http.Request) {
	if s.tmdb.Load() == nil {
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
		results, err = s.tmdb.Load().DiscoverMovies(r.Context(), q)
	case "series":
		results, err = s.tmdb.Load().DiscoverSeries(r.Context(), q)
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
			TmdbID: r.TmdbID, Title: r.Title, Overview: r.Overview, ReleaseDate: r.ReleaseDate, PosterURL: r.PosterURL, Rating: r.Rating,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

type fulfillmentResponse struct {
	LibraryID         string `json:"libraryId"`
	LibraryName       string `json:"libraryName"`
	Is4K              bool   `json:"is4K"`
	AcquisitionStatus string `json:"acquisitionStatus"`
	AcquisitionError  string `json:"acquisitionError,omitempty"`
}

type contentRequestResponse struct {
	ID           string                `json:"id"`
	RequestedBy  string                `json:"requestedBy"`
	Requester    string                `json:"requester"`
	MediaType    string                `json:"mediaType"`
	TmdbID       int                   `json:"tmdbId"`
	Title        string                `json:"title"`
	Overview     string                `json:"overview,omitempty"`
	ReleaseDate  string                `json:"releaseDate,omitempty"`
	PosterURL    string                `json:"posterUrl,omitempty"`
	Status       string                `json:"status"`
	DecidedAt    *string               `json:"decidedAt,omitempty"`
	CreatedAt    string                `json:"createdAt"`
	Fulfillments []fulfillmentResponse `json:"fulfillments"`
}

func toContentRequestResponse(r *store.ContentRequest, fulfillments []*store.ContentRequestFulfillment) contentRequestResponse {
	resp := contentRequestResponse{
		ID:           r.ID,
		RequestedBy:  r.RequestedBy,
		Requester:    r.RequestedByUsername,
		MediaType:    r.MediaType,
		TmdbID:       r.TmdbID,
		Title:        r.Title,
		Overview:     r.Overview,
		ReleaseDate:  r.ReleaseDate,
		PosterURL:    r.PosterURL,
		Status:       r.Status,
		CreatedAt:    r.CreatedAt.Format(time.RFC3339),
		Fulfillments: make([]fulfillmentResponse, 0, len(fulfillments)),
	}
	if r.DecidedAt != nil {
		s := r.DecidedAt.Format(time.RFC3339)
		resp.DecidedAt = &s
	}
	for _, f := range fulfillments {
		resp.Fulfillments = append(resp.Fulfillments, fulfillmentResponse{
			LibraryID: f.LibraryID, LibraryName: f.LibraryName, Is4K: f.Is4K,
			AcquisitionStatus: f.AcquisitionStatus, AcquisitionError: f.AcquisitionError,
		})
	}
	return resp
}

// loadFulfillments fetches r's fulfillments, logging and returning an empty
// slice on error rather than failing the whole request listing over what's
// secondary information.
func (s *Server) loadFulfillments(r *store.ContentRequest) []*store.ContentRequestFulfillment {
	f, err := s.store.ListContentRequestFulfillments(r.ID)
	if err != nil {
		log.Printf("content requests: loading fulfillments for %s: %v", r.ID, err)
		return nil
	}
	return f
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

	// Fulfillment (materializing metadata + starting acquisition) doesn't
	// happen here -- it's gated behind admin approval, see
	// handleDecideContentRequest. A pending request shouldn't already be
	// occupying debrid/usenet provider quota before anyone's reviewed it.
	writeJSON(w, http.StatusCreated, toContentRequestResponse(created, nil))
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
		resp = append(resp, toContentRequestResponse(req, s.loadFulfillments(req)))
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
		resp = append(resp, toContentRequestResponse(req, s.loadFulfillments(req)))
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleAdminDeleteContentRequest lets an admin remove any request
// regardless of status -- unlike handleDeleteContentRequest (self-service
// withdraw, which only lets the original requester remove their own
// still-pending one), this covers cleaning up a request that was approved
// or declined but never usefully fulfilled (e.g. no default request target
// was configured at the time it was created) or just tidying the queue.
// content_request_fulfillments rows cascade-delete with it (see migration
// 000016's ON DELETE CASCADE), so no separate cleanup is needed here.
func (s *Server) handleAdminDeleteContentRequest(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteContentRequest(id); err != nil {
		s.writeStoreErr(w, err, "deleting request")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

	// Approval is what actually starts work: materializing metadata and
	// racing debrid/usenet candidates for a streamable link, in the
	// background -- MaterializePlaceholder makes a blocking TMDb call and
	// the admin shouldn't wait on it before seeing the decision recorded.
	if req.Status == "approved" && s.acquisition.Load() != nil {
		go s.acquisition.Load().FulfillRequest(context.Background(), updated.ID, updated.MediaType, updated.TmdbID)
	}

	writeJSON(w, http.StatusOK, toContentRequestResponse(updated, s.loadFulfillments(updated)))
}
