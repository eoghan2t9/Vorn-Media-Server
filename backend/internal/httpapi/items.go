package httpapi

import (
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

type mediaItemResponse struct {
	ID            string  `json:"id"`
	LibraryID     string  `json:"libraryId"`
	ParentID      *string `json:"parentId,omitempty"`
	Kind          string  `json:"kind"`
	Title         string  `json:"title"`
	Overview      string  `json:"overview,omitempty"`
	SeasonNumber  *int    `json:"seasonNumber,omitempty"`
	EpisodeNumber *int    `json:"episodeNumber,omitempty"`
	ReleaseDate   *string `json:"releaseDate,omitempty"`
	AddedAt       string  `json:"addedAt"`
	PosterURL     string  `json:"posterUrl,omitempty"`
	BackdropURL   string  `json:"backdropUrl,omitempty"`
	Author        string  `json:"author,omitempty"`
}

func toMediaItemResponse(m *store.MediaItem) mediaItemResponse {
	resp := mediaItemResponse{
		ID:            m.ID,
		LibraryID:     m.LibraryID,
		ParentID:      m.ParentID,
		Kind:          m.Kind,
		Title:         m.Title,
		Overview:      m.Overview,
		SeasonNumber:  m.SeasonNumber,
		EpisodeNumber: m.EpisodeNumber,
		AddedAt:       m.AddedAt.Format(time.RFC3339),
		PosterURL:     m.PosterURL,
		BackdropURL:   m.BackdropURL,
		Author:        m.Author,
	}
	if m.ReleaseDate != nil {
		d := m.ReleaseDate.Format("2006-01-02")
		resp.ReleaseDate = &d
	}
	return resp
}

func (s *Server) handleListLibraryItems(w http.ResponseWriter, r *http.Request) {
	libraryID := r.PathValue("id")
	user := userFromContext(r.Context())

	ok, err := s.canAccessLibrary(user, libraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checking permissions")
		return
	}
	if !ok {
		writeError(w, http.StatusForbidden, "no access to this library")
		return
	}

	opts := store.ListItemsOptions{
		Kind: r.URL.Query().Get("kind"),
		Sort: r.URL.Query().Get("sort"),
	}
	items, err := s.store.ListMediaItems(libraryID, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing items")
		return
	}

	resp := make([]mediaItemResponse, 0, len(items))
	for _, m := range items {
		resp = append(resp, toMediaItemResponse(m))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user := userFromContext(r.Context())

	item, err := s.store.GetMediaItem(id)
	if err != nil {
		s.writeStoreErr(w, err, "loading item")
		return
	}
	ok, err := s.canAccessLibrary(user, item.LibraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checking permissions")
		return
	}
	if !ok {
		writeError(w, http.StatusForbidden, "no access to this item")
		return
	}

	type itemDetailResponse struct {
		mediaItemResponse
		Children []mediaItemResponse `json:"children,omitempty"`
	}
	resp := itemDetailResponse{mediaItemResponse: toMediaItemResponse(item)}

	if item.Kind == "series" || item.Kind == "season" || item.Kind == "artist" || item.Kind == "album" || item.Kind == "book" {
		children, err := s.store.ListChildren(item.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "loading children")
			return
		}
		resp.Children = make([]mediaItemResponse, 0, len(children))
		for _, c := range children {
			resp.Children = append(resp.Children, toMediaItemResponse(c))
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

type updateProgressRequest struct {
	PositionSeconds float64 `json:"positionSeconds"`
	DurationSeconds float64 `json:"durationSeconds"`
}

func (s *Server) handleUpdateProgress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user := userFromContext(r.Context())

	var req updateProgressRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.store.UpsertPlaybackState(user.ID, id, req.PositionSeconds, req.DurationSeconds); err != nil {
		writeError(w, http.StatusInternalServerError, "saving progress")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type progressResponse struct {
	PositionSeconds float64 `json:"positionSeconds"`
	DurationSeconds float64 `json:"durationSeconds"`
}

func (s *Server) handleGetProgress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user := userFromContext(r.Context())

	p, err := s.store.GetPlaybackState(user.ID, id)
	if err != nil {
		// No progress yet is a normal "starting fresh" state, not an error.
		writeJSON(w, http.StatusOK, progressResponse{})
		return
	}
	writeJSON(w, http.StatusOK, progressResponse{PositionSeconds: p.PositionSeconds, DurationSeconds: p.DurationSeconds})
}

type continueWatchingResponse struct {
	Item            mediaItemResponse `json:"item"`
	PositionSeconds float64           `json:"positionSeconds"`
	DurationSeconds float64           `json:"durationSeconds"`
}

func (s *Server) handleContinueWatching(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	items, err := s.store.ListContinueWatching(user.ID, user.IsAdmin, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading continue watching")
		return
	}
	resp := make([]continueWatchingResponse, 0, len(items))
	for _, c := range items {
		resp = append(resp, continueWatchingResponse{
			Item:            toMediaItemResponse(c.Item),
			PositionSeconds: c.Progress.PositionSeconds,
			DurationSeconds: c.Progress.DurationSeconds,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}
