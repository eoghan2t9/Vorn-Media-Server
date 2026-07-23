package httpapi

import (
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

type metadataJobResponse struct {
	ID           string  `json:"id"`
	LibraryID    string  `json:"libraryId"`
	Status       string  `json:"status"`
	ItemsFound   int64   `json:"itemsFound"`
	ItemsMatched int64   `json:"itemsMatched"`
	Error        *string `json:"error,omitempty"`
	StartedAt    string  `json:"startedAt"`
	FinishedAt   *string `json:"finishedAt,omitempty"`
}

func toMetadataJobResponse(j *store.MetadataSyncJob) metadataJobResponse {
	resp := metadataJobResponse{
		ID:           j.ID,
		LibraryID:    j.LibraryID,
		Status:       j.Status,
		ItemsFound:   j.ItemsFound,
		ItemsMatched: j.ItemsMatched,
		Error:        j.Error,
		StartedAt:    j.StartedAt.Format(time.RFC3339),
	}
	if j.FinishedAt != nil {
		s := j.FinishedAt.Format(time.RFC3339)
		resp.FinishedAt = &s
	}
	return resp
}

func (s *Server) handleStartMetadataSync(w http.ResponseWriter, r *http.Request) {
	libraryID := r.PathValue("id")
	if s.metadataSvc == nil {
		writeError(w, http.StatusServiceUnavailable, "metadata sync is not configured (set VORN_TMDB_API_KEY)")
		return
	}
	if _, err := s.store.GetLibrary(libraryID); err != nil {
		s.writeStoreErr(w, err, "loading library")
		return
	}
	job, err := s.metadataSvc.StartLibrarySync(libraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "starting metadata sync")
		return
	}
	writeJSON(w, http.StatusAccepted, toMetadataJobResponse(job))
}

func (s *Server) handleGetMetadataJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, err := s.store.GetMetadataSyncJob(id)
	if err != nil {
		s.writeStoreErr(w, err, "loading metadata sync job")
		return
	}
	writeJSON(w, http.StatusOK, toMetadataJobResponse(job))
}

type updateMetadataRequest struct {
	Title       string `json:"title,omitempty"`
	Overview    string `json:"overview,omitempty"`
	ReleaseDate string `json:"releaseDate,omitempty"` // YYYY-MM-DD
	TmdbID      *int   `json:"tmdbId,omitempty"`
}

// handleUpdateItemMetadata lets an admin manually correct an item's identity
// (e.g. a scan/metadata mismatch) and locks it so future automatic syncs
// won't overwrite the fix.
func (s *Server) handleUpdateItemMetadata(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req updateMetadataRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	update := store.MetadataUpdate{Title: req.Title, Overview: req.Overview, TmdbID: req.TmdbID}
	if req.ReleaseDate != "" {
		d, err := time.Parse("2006-01-02", req.ReleaseDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "releaseDate must be YYYY-MM-DD")
			return
		}
		update.ReleaseDate = &d
	}

	if err := s.store.ApplyMetadata(id, update, true); err != nil {
		writeError(w, http.StatusInternalServerError, "updating metadata")
		return
	}
	item, err := s.store.GetMediaItem(id)
	if err != nil {
		s.writeStoreErr(w, err, "loading updated item")
		return
	}
	writeJSON(w, http.StatusOK, toMediaItemResponse(item))
}
