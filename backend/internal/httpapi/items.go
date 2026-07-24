package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

type mediaItemResponse struct {
	ID                   string  `json:"id"`
	LibraryID            string  `json:"libraryId"`
	ParentID             *string `json:"parentId,omitempty"`
	Kind                 string  `json:"kind"`
	Title                string  `json:"title"`
	Overview             string  `json:"overview,omitempty"`
	SeasonNumber         *int    `json:"seasonNumber,omitempty"`
	EpisodeNumber        *int    `json:"episodeNumber,omitempty"`
	ReleaseDate          *string `json:"releaseDate,omitempty"`
	AddedAt              string  `json:"addedAt"`
	PosterURL            string  `json:"posterUrl,omitempty"`
	BackdropURL          string  `json:"backdropUrl,omitempty"`
	Author               string  `json:"author,omitempty"`
	LogoURL              string  `json:"logoUrl,omitempty"`
	RatingIMDb           string  `json:"ratingImdb,omitempty"`
	RatingRottenTomatoes string  `json:"ratingRottenTomatoes,omitempty"`
	AcquisitionStatus    string  `json:"acquisitionStatus"`
	AcquisitionError     string  `json:"acquisitionError,omitempty"`
	Monitored            bool    `json:"monitored"`
	CurrentReleaseTitle  string  `json:"currentReleaseTitle,omitempty"`
}

func toMediaItemResponse(m *store.MediaItem) mediaItemResponse {
	resp := mediaItemResponse{
		ID:                   m.ID,
		LibraryID:            m.LibraryID,
		ParentID:             m.ParentID,
		Kind:                 m.Kind,
		Title:                m.Title,
		Overview:             m.Overview,
		SeasonNumber:         m.SeasonNumber,
		EpisodeNumber:        m.EpisodeNumber,
		AddedAt:              m.AddedAt.Format(time.RFC3339),
		PosterURL:            m.PosterURL,
		BackdropURL:          m.BackdropURL,
		Author:               m.Author,
		LogoURL:              m.LogoURL,
		RatingIMDb:           m.RatingIMDb,
		RatingRottenTomatoes: m.RatingRottenTomatoes,
		AcquisitionStatus:    m.AcquisitionStatus,
		AcquisitionError:     m.AcquisitionError,
		Monitored:            m.Monitored,
		CurrentReleaseTitle:  m.CurrentReleaseTitle,
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

	// Episodes show their parent series' cast/crew rather than fetching
	// per-episode guest stars -- two parent hops up (episode -> season ->
	// series, see store.PromoteEpisode).
	castSourceID := item.ID
	if item.Kind == "episode" && item.ParentID != nil {
		if season, err := s.store.GetMediaItem(*item.ParentID); err == nil && season.ParentID != nil {
			castSourceID = *season.ParentID
		}
	}
	if item.Kind == "movie" || item.Kind == "series" || item.Kind == "episode" {
		cast, directors, similar, err := s.store.GetItemCastAndSimilar(castSourceID)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusInternalServerError, "loading cast")
			return
		}
		resp.Cast = make([]castMemberResponse, 0, len(cast))
		for _, c := range cast {
			resp.Cast = append(resp.Cast, castMemberResponse{Name: c.Name, Character: c.Character, PhotoURL: c.PhotoURL})
		}
		resp.Directors = directors
		resp.Similar = make([]catalogEntryResponse, 0, len(similar))
		for _, sm := range similar {
			resp.Similar = append(resp.Similar, catalogEntryResponse{TmdbID: sm.TmdbID, Title: sm.Title, Overview: sm.Overview, ReleaseDate: sm.ReleaseDate, PosterURL: sm.PosterURL})
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

type castMemberResponse struct {
	Name      string `json:"name"`
	Character string `json:"character,omitempty"`
	PhotoURL  string `json:"photoUrl,omitempty"`
}

type itemDetailResponse struct {
	mediaItemResponse
	Children  []mediaItemResponse    `json:"children,omitempty"`
	Cast      []castMemberResponse   `json:"cast,omitempty"`
	Directors []string               `json:"directors,omitempty"`
	Similar   []catalogEntryResponse `json:"similar,omitempty"`
}

type setMonitoredRequest struct {
	Monitored bool `json:"monitored"`
}

// handleSetItemMonitored subscribes/unsubscribes a movie or series to
// acquisition.MonitorScheduler's recurring re-check (grab new episodes as
// they air, keep retrying an unavailable movie, auto-upgrade quality once
// owned). Same access pattern as play/acquire -- any user with library
// access can monitor something, not just admins, consistent with how
// playing an unacquired item already triggers acquisition on its own.
func (s *Server) handleSetItemMonitored(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user := userFromContext(r.Context())

	item, err := s.store.GetMediaItem(id)
	if err != nil {
		s.writeStoreErr(w, err, "loading item")
		return
	}
	if item.Kind != "movie" && item.Kind != "series" {
		writeError(w, http.StatusBadRequest, "only movies and series can be monitored")
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

	var req setMonitoredRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.store.SetMediaItemMonitored(id, req.Monitored); err != nil {
		s.writeStoreErr(w, err, "updating monitored status")
		return
	}
	fresh, err := s.store.GetMediaItem(id)
	if err != nil {
		s.writeStoreErr(w, err, "reloading item")
		return
	}
	writeJSON(w, http.StatusOK, toMediaItemResponse(fresh))
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
