package httpapi

import "net/http"

type serverStatsResponse struct {
	LibraryCount int64 `json:"libraryCount"`
	UserCount    int64 `json:"userCount"`
	MovieCount   int64 `json:"movieCount"`
	SeriesCount  int64 `json:"seriesCount"`
	EpisodeCount int64 `json:"episodeCount"`
	ActiveUsers  int64 `json:"activeUsers"`
}

func (s *Server) handleServerStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.store.GetServerStats()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading stats")
		return
	}
	writeJSON(w, http.StatusOK, serverStatsResponse{
		LibraryCount: stats.LibraryCount,
		UserCount:    stats.UserCount,
		MovieCount:   stats.MovieCount,
		SeriesCount:  stats.SeriesCount,
		EpisodeCount: stats.EpisodeCount,
		ActiveUsers:  stats.ActiveUsers,
	})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSON(w, http.StatusOK, []mediaItemResponse{})
		return
	}
	user := userFromContext(r.Context())

	items, err := s.store.SearchMediaItems(query, user.IsAdmin, user.ID, 25)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "searching")
		return
	}
	resp := make([]mediaItemResponse, 0, len(items))
	for _, m := range items {
		resp = append(resp, toMediaItemResponse(m))
	}
	writeJSON(w, http.StatusOK, resp)
}
