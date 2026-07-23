package httpapi

import "net/http"

type maintenanceResultResponse struct {
	Cleared int    `json:"cleared"`
	Detail  string `json:"detail,omitempty"`
}

// handleClearScanCache flushes every scan-staging key in DragonflyDB. It's
// a maintenance action, not something a normal scan needs: a crashed or
// killed scan job can leave its staging queue behind forever, and this is
// how an admin reclaims that memory without waiting for one to happen to
// reuse the same job ID.
func (s *Server) handleClearScanCache(w http.ResponseWriter, r *http.Request) {
	if s.scanner == nil {
		writeError(w, http.StatusServiceUnavailable, "scanner is not configured")
		return
	}
	n, err := s.scanner.FlushStagingCache(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "clearing scan cache")
		return
	}
	writeJSON(w, http.StatusOK, maintenanceResultResponse{Cleared: int(n)})
}

// handleClearTranscodeCache removes tracking and on-disk output for every
// finished (stopped/failed) transcode session. Actively running sessions
// are left untouched.
func (s *Server) handleClearTranscodeCache(w http.ResponseWriter, r *http.Request) {
	if s.transcodeMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "transcoding is not available")
		return
	}
	n, err := s.transcodeMgr.ClearFinished()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "clearing transcode cache")
		return
	}
	writeJSON(w, http.StatusOK, maintenanceResultResponse{Cleared: n})
}
