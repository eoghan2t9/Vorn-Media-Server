package httpapi

import (
	"net/http"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

type qualityProfileResponse struct {
	LibraryID      string `json:"libraryId"`
	MinResolution  string `json:"minResolution"`
	MaxResolution  string `json:"maxResolution"`
	PreferredCodec string `json:"preferredCodec"`
	MinSeeders     int    `json:"minSeeders"`
	PreferRemux    bool   `json:"preferRemux"`
}

func toQualityProfileResponse(p store.QualityProfile) qualityProfileResponse {
	return qualityProfileResponse{
		LibraryID:      p.LibraryID,
		MinResolution:  p.MinResolution,
		MaxResolution:  p.MaxResolution,
		PreferredCodec: p.PreferredCodec,
		MinSeeders:     p.MinSeeders,
		PreferRemux:    p.PreferRemux,
	}
}

// handleGetQualityProfile returns libraryID's quality profile, synthesizing
// hardcoded defaults (see store.GetQualityProfile) if the admin hasn't
// configured one yet -- there's always something sensible for the
// acquisition orchestrator to score releases against.
func (s *Server) handleGetQualityProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.GetLibrary(id); err != nil {
		s.writeStoreErr(w, err, "loading library")
		return
	}
	profile, err := s.store.GetQualityProfile(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading quality profile")
		return
	}
	writeJSON(w, http.StatusOK, toQualityProfileResponse(profile))
}

type updateQualityProfileRequest struct {
	MinResolution  string `json:"minResolution"`
	MaxResolution  string `json:"maxResolution"`
	PreferredCodec string `json:"preferredCodec"`
	MinSeeders     int    `json:"minSeeders"`
	PreferRemux    bool   `json:"preferRemux"`
}

var validResolutions = map[string]bool{"480p": true, "720p": true, "1080p": true, "2160p": true}
var validCodecs = map[string]bool{"": true, "x264": true, "x265": true, "av1": true}

func (s *Server) handleUpdateQualityProfile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.GetLibrary(id); err != nil {
		s.writeStoreErr(w, err, "loading library")
		return
	}
	var req updateQualityProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validResolutions[req.MinResolution] || !validResolutions[req.MaxResolution] {
		writeError(w, http.StatusBadRequest, "minResolution/maxResolution must be one of 480p, 720p, 1080p, 2160p")
		return
	}
	if !validCodecs[req.PreferredCodec] {
		writeError(w, http.StatusBadRequest, "preferredCodec must be one of '', x264, x265, av1")
		return
	}
	if req.MinSeeders < 0 {
		req.MinSeeders = 0
	}

	profile, err := s.store.UpsertQualityProfile(store.QualityProfile{
		LibraryID:      id,
		MinResolution:  req.MinResolution,
		MaxResolution:  req.MaxResolution,
		PreferredCodec: req.PreferredCodec,
		MinSeeders:     req.MinSeeders,
		PreferRemux:    req.PreferRemux,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "saving quality profile")
		return
	}
	writeJSON(w, http.StatusOK, toQualityProfileResponse(profile))
}
