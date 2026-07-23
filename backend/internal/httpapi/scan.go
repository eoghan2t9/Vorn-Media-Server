package httpapi

import (
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

type scanJobResponse struct {
	ID          string  `json:"id"`
	LibraryID   string  `json:"libraryId"`
	Kind        string  `json:"kind"`
	Status      string  `json:"status"`
	FilesFound  int64   `json:"filesFound"`
	FilesSynced int64   `json:"filesSynced"`
	Error       *string `json:"error,omitempty"`
	StartedAt   string  `json:"startedAt"`
	FinishedAt  *string `json:"finishedAt,omitempty"`
}

func toScanJobResponse(j *store.ScanJob) scanJobResponse {
	resp := scanJobResponse{
		ID:          j.ID,
		LibraryID:   j.LibraryID,
		Kind:        j.Kind,
		Status:      j.Status,
		FilesFound:  j.FilesFound,
		FilesSynced: j.FilesSynced,
		Error:       j.Error,
		StartedAt:   j.StartedAt.Format(time.RFC3339),
	}
	if j.FinishedAt != nil {
		s := j.FinishedAt.Format(time.RFC3339)
		resp.FinishedAt = &s
	}
	return resp
}

func (s *Server) handleStartLibraryScan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	library, err := s.store.GetLibrary(id)
	if err != nil {
		s.writeStoreErr(w, err, "loading library")
		return
	}
	job, err := s.scanner.StartLibraryScan(library)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "starting scan")
		return
	}
	writeJSON(w, http.StatusAccepted, toScanJobResponse(job))
}

func (s *Server) handleListScanJobs(w http.ResponseWriter, r *http.Request) {
	libraryID := r.URL.Query().Get("libraryId")
	jobs, err := s.store.ListScanJobs(libraryID, 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing scan jobs")
		return
	}
	resp := make([]scanJobResponse, 0, len(jobs))
	for _, j := range jobs {
		resp = append(resp, toScanJobResponse(j))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetScanJob(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, err := s.store.GetScanJob(id)
	if err != nil {
		s.writeStoreErr(w, err, "loading scan job")
		return
	}
	writeJSON(w, http.StatusOK, toScanJobResponse(job))
}

type syntheticScanRequest struct {
	LibraryID string `json:"libraryId"`
	Count     int    `json:"count"`
}

// handleSyntheticScan populates the staging pipeline with fabricated file
// records (no disk I/O) so scan-speed benchmarking doesn't require actually
// owning a library with thousands of real files. Gated behind dev mode so
// it's never reachable on a production deployment by accident.
func (s *Server) handleSyntheticScan(w http.ResponseWriter, r *http.Request) {
	if !s.devMode {
		writeError(w, http.StatusForbidden, "synthetic scans require VORN_DEV_MODE=true")
		return
	}

	var req syntheticScanRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Count <= 0 {
		writeError(w, http.StatusBadRequest, "count must be positive")
		return
	}
	if _, err := s.store.GetLibrary(req.LibraryID); err != nil {
		s.writeStoreErr(w, err, "loading library")
		return
	}

	job, err := s.scanner.StartSyntheticScan(req.LibraryID, req.Count)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "starting synthetic scan")
		return
	}
	writeJSON(w, http.StatusAccepted, toScanJobResponse(job))
}
