package httpapi

import (
	"net/http"
	"time"
)

const currentlyWatchingWindow = 2 * time.Minute

type currentlyWatchingResponse struct {
	Username        string            `json:"username"`
	Item            mediaItemResponse `json:"item"`
	PositionSeconds float64           `json:"positionSeconds"`
	DurationSeconds float64           `json:"durationSeconds"`
	UpdatedAt       string            `json:"updatedAt"`
}

// handleCurrentlyWatching backs the admin "currently being watched" view.
// Vorn doesn't track live connection state, so "currently watching" is a
// heuristic: any unfinished playback whose progress was reported within the
// last couple of minutes (the player pings progress periodically while
// actively playing).
func (s *Server) handleCurrentlyWatching(w http.ResponseWriter, r *http.Request) {
	entries, err := s.store.ListCurrentlyWatching(currentlyWatchingWindow)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading currently watching")
		return
	}
	resp := make([]currentlyWatchingResponse, 0, len(entries))
	for _, e := range entries {
		resp = append(resp, currentlyWatchingResponse{
			Username:        e.Username,
			Item:            toMediaItemResponse(e.Item),
			PositionSeconds: e.PositionSeconds,
			DurationSeconds: e.DurationSeconds,
			UpdatedAt:       e.UpdatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}
