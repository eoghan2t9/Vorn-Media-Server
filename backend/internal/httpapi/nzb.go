package httpapi

import (
	"io"
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const nzbServiceUnavailable = "NZB acquisition is not configured (set VORN_NZB_ENABLED=true)"

type nzbDownloadResponse struct {
	ID          string  `json:"id"`
	LibraryID   *string `json:"libraryId,omitempty"`
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	BytesTotal  int64   `json:"bytesTotal"`
	BytesDone   int64   `json:"bytesDone"`
	Error       string  `json:"error,omitempty"`
	Promoted    bool    `json:"promoted"`
	AddedAt     string  `json:"addedAt"`
	CompletedAt *string `json:"completedAt,omitempty"`
}

func toNZBDownloadResponse(n *store.NZBDownload) nzbDownloadResponse {
	resp := nzbDownloadResponse{
		ID:         n.ID,
		LibraryID:  n.LibraryID,
		Name:       n.Name,
		Status:     n.Status,
		BytesTotal: n.BytesTotal,
		BytesDone:  n.BytesDone,
		Error:      n.Error,
		Promoted:   n.Promoted,
		AddedAt:    n.AddedAt.Format(time.RFC3339),
	}
	if n.CompletedAt != nil {
		s := n.CompletedAt.Format(time.RFC3339)
		resp.CompletedAt = &s
	}
	return resp
}

func (s *Server) handleListNZBDownloads(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc == nil {
		writeJSON(w, http.StatusOK, []nzbDownloadResponse{})
		return
	}
	downloads, err := s.nzbSvc.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing nzb downloads")
		return
	}
	resp := make([]nzbDownloadResponse, 0, len(downloads))
	for _, n := range downloads {
		resp = append(resp, toNZBDownloadResponse(n))
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleAddNZB accepts a raw .nzb file body. libraryId is passed as a query
// parameter since the body is the file itself, not JSON.
func (s *Server) handleAddNZB(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil || len(data) == 0 {
		writeError(w, http.StatusBadRequest, "missing or unreadable nzb file body")
		return
	}

	var libraryID *string
	if id := r.URL.Query().Get("libraryId"); id != "" {
		if _, err := s.store.GetLibrary(id); err != nil {
			s.writeStoreErr(w, err, "loading library")
			return
		}
		libraryID = &id
	}

	n, err := s.nzbSvc.AddNZB(data, libraryID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, toNZBDownloadResponse(n))
}

func (s *Server) handleRemoveNZB(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	deleteFiles := r.URL.Query().Get("deleteFiles") == "true"
	if err := s.nzbSvc.Remove(id, deleteFiles); err != nil {
		s.writeStoreErr(w, err, "removing nzb download")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type usenetServerResponse struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	UseTLS         bool   `json:"useTls"`
	Username       string `json:"username"`
	MaxConnections int    `json:"maxConnections"`
	Enabled        bool   `json:"enabled"`
	CreatedAt      string `json:"createdAt"`
}

func toUsenetServerResponse(u *store.UsenetServer) usenetServerResponse {
	return usenetServerResponse{
		ID:             u.ID,
		Name:           u.Name,
		Host:           u.Host,
		Port:           u.Port,
		UseTLS:         u.UseTLS,
		Username:       u.Username,
		MaxConnections: u.MaxConnections,
		Enabled:        u.Enabled,
		CreatedAt:      u.CreatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleListUsenetServers(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc == nil {
		writeJSON(w, http.StatusOK, []usenetServerResponse{})
		return
	}
	servers, err := s.nzbSvc.ListServers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing usenet servers")
		return
	}
	resp := make([]usenetServerResponse, 0, len(servers))
	for _, u := range servers {
		resp = append(resp, toUsenetServerResponse(u))
	}
	writeJSON(w, http.StatusOK, resp)
}

type createUsenetServerRequest struct {
	Name           string `json:"name"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	UseTLS         bool   `json:"useTls"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	MaxConnections int    `json:"maxConnections"`
}

func (s *Server) handleCreateUsenetServer(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	var req createUsenetServerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Host == "" || req.Port == 0 {
		writeError(w, http.StatusBadRequest, "name, host, and port are required")
		return
	}
	if req.MaxConnections < 1 {
		req.MaxConnections = 1
	}
	u, err := s.nzbSvc.AddServer(store.UsenetServer{
		Name:           req.Name,
		Host:           req.Host,
		Port:           req.Port,
		UseTLS:         req.UseTLS,
		Username:       req.Username,
		Password:       req.Password,
		MaxConnections: req.MaxConnections,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "creating usenet server")
		return
	}
	writeJSON(w, http.StatusCreated, toUsenetServerResponse(u))
}

func (s *Server) handleDeleteUsenetServer(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := s.nzbSvc.RemoveServer(id); err != nil {
		s.writeStoreErr(w, err, "deleting usenet server")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
