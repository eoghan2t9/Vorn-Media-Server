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

// systemStatsResponse reports availability per metric rather than one
// blanket flag -- which stats are obtainable genuinely varies by host OS
// (see the sysstats package), so e.g. macOS can report disk+memory but not
// CPU or network with the approach used there.
type systemStatsResponse struct {
	CPUAvailable bool    `json:"cpuAvailable"`
	CPUPercent   float64 `json:"cpuPercent"`

	MemAvailable  bool   `json:"memAvailable"`
	MemUsedBytes  uint64 `json:"memUsedBytes"`
	MemTotalBytes uint64 `json:"memTotalBytes"`

	DiskAvailable  bool   `json:"diskAvailable"`
	DiskUsedBytes  uint64 `json:"diskUsedBytes"`
	DiskTotalBytes uint64 `json:"diskTotalBytes"`

	NetAvailable     bool    `json:"netAvailable"`
	NetRxBytesPerSec float64 `json:"netRxBytesPerSec"`
	NetTxBytesPerSec float64 `json:"netTxBytesPerSec"`
}

func (s *Server) handleSystemStats(w http.ResponseWriter, r *http.Request) {
	if s.sysStats == nil {
		writeJSON(w, http.StatusOK, systemStatsResponse{})
		return
	}
	snap := s.sysStats.Latest()
	writeJSON(w, http.StatusOK, systemStatsResponse{
		CPUAvailable:     snap.CPUAvailable,
		CPUPercent:       snap.CPUPercent,
		MemAvailable:     snap.MemAvailable,
		MemUsedBytes:     snap.MemUsedBytes,
		MemTotalBytes:    snap.MemTotalBytes,
		DiskAvailable:    snap.DiskAvailable,
		DiskUsedBytes:    snap.DiskUsedBytes,
		DiskTotalBytes:   snap.DiskTotalBytes,
		NetAvailable:     snap.NetAvailable,
		NetRxBytesPerSec: snap.NetRxBytesPerSec,
		NetTxBytesPerSec: snap.NetTxBytesPerSec,
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
