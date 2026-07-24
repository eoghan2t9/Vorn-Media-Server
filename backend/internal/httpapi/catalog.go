// Package-local naming note: this file is the TMDb catalog browse/open
// feature. It's deliberately not named browse.go / browseResponse etc. --
// those names are already taken by the admin server-side folder picker
// (see browse.go, handleBrowseFilesystem), an unrelated feature that just
// happens to share the word "browse".
package httpapi

import (
	"net/http"
	"strconv"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/metadata"
)

const acquisitionNotConfigured = "on-demand acquisition is not configured (needs a TMDb API key and a torrent indexer)"

type catalogEntryResponse struct {
	TmdbID      int    `json:"tmdbId"`
	Title       string `json:"title"`
	Overview    string `json:"overview,omitempty"`
	ReleaseDate string `json:"releaseDate,omitempty"`
	PosterURL   string `json:"posterUrl,omitempty"`
}

type catalogPageResponse struct {
	Results    []catalogEntryResponse `json:"results"`
	Page       int                    `json:"page"`
	TotalPages int                    `json:"totalPages"`
}

// handleBrowseCatalog lists TMDb's popular/trending catalog directly and
// unpaginated locally, never touching the DB -- browsing something Vorn
// doesn't own yet shouldn't cost a write, only opening one
// (handleOpenCatalogEntry) does.
func (s *Server) handleBrowseCatalog(w http.ResponseWriter, r *http.Request) {
	if s.tmdb == nil {
		writeError(w, http.StatusServiceUnavailable, tmdbNotConfigured)
		return
	}
	mediaType := r.URL.Query().Get("type")
	sortBy := r.URL.Query().Get("sort")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	var paged metadata.PagedResult
	var err error
	switch {
	case mediaType == "movie" && sortBy == "trending":
		paged, err = s.tmdb.TrendingMovies(r.Context(), page)
	case mediaType == "movie":
		paged, err = s.tmdb.PopularMovies(r.Context(), page)
	case mediaType == "series" && sortBy == "trending":
		paged, err = s.tmdb.TrendingSeries(r.Context(), page)
	case mediaType == "series":
		paged, err = s.tmdb.PopularSeries(r.Context(), page)
	default:
		writeError(w, http.StatusBadRequest, "type must be 'movie' or 'series'")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "fetching TMDb catalog")
		return
	}

	resp := catalogPageResponse{Page: paged.Page, TotalPages: paged.TotalPages, Results: make([]catalogEntryResponse, 0, len(paged.Results))}
	for _, r := range paged.Results {
		resp.Results = append(resp.Results, catalogEntryResponse{
			TmdbID: r.TmdbID, Title: r.Title, Overview: r.Overview, ReleaseDate: r.ReleaseDate, PosterURL: r.PosterURL,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

type openCatalogEntryRequest struct {
	TmdbID    int    `json:"tmdbId"`
	MediaType string `json:"mediaType"` // "movie" | "series"
	LibraryID string `json:"libraryId"`
}

// handleOpenCatalogEntry is where a TMDb-only browse card first touches the
// database: it materializes (or finds the already-materialized) local
// placeholder media_item for it, so the frontend can navigate straight into
// the normal item-detail route from here on, same as any owned item.
func (s *Server) handleOpenCatalogEntry(w http.ResponseWriter, r *http.Request) {
	if s.acquisition == nil {
		writeError(w, http.StatusServiceUnavailable, acquisitionNotConfigured)
		return
	}
	var req openCatalogEntryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TmdbID == 0 || req.LibraryID == "" {
		writeError(w, http.StatusBadRequest, "tmdbId and libraryId are required")
		return
	}
	if req.MediaType != "movie" && req.MediaType != "series" {
		writeError(w, http.StatusBadRequest, "mediaType must be 'movie' or 'series'")
		return
	}

	user := userFromContext(r.Context())
	ok, err := s.canAccessLibrary(user, req.LibraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checking permissions")
		return
	}
	if !ok {
		writeError(w, http.StatusForbidden, "no access to this library")
		return
	}

	item, err := s.acquisition.MaterializePlaceholder(r.Context(), req.LibraryID, req.TmdbID, req.MediaType)
	if err != nil {
		writeError(w, http.StatusBadGateway, "fetching details from TMDb")
		return
	}
	writeJSON(w, http.StatusOK, toMediaItemResponse(item))
}
