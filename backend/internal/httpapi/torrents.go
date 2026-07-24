package httpapi

import (
	"io"
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/torrent"
)

const torrentServiceUnavailable = "torrent acquisition is not configured (set VORN_TORRENT_ENABLED=true)"

type torrentResponse struct {
	ID          string  `json:"id"`
	LibraryID   *string `json:"libraryId,omitempty"`
	InfoHash    string  `json:"infoHash"`
	Name        string  `json:"name"`
	Sequential  bool    `json:"sequential"`
	Status      string  `json:"status"`
	BytesTotal  int64   `json:"bytesTotal"`
	BytesDone   int64   `json:"bytesDone"`
	Error       string  `json:"error,omitempty"`
	AddedAt     string  `json:"addedAt"`
	CompletedAt *string `json:"completedAt,omitempty"`
}

func toTorrentResponse(t *store.Torrent) torrentResponse {
	resp := torrentResponse{
		ID:         t.ID,
		LibraryID:  t.LibraryID,
		InfoHash:   t.InfoHash,
		Name:       t.Name,
		Sequential: t.Sequential,
		Status:     t.Status,
		BytesTotal: t.BytesTotal,
		BytesDone:  t.BytesDone,
		Error:      t.Error,
		AddedAt:    t.AddedAt.Format(time.RFC3339),
	}
	if t.CompletedAt != nil {
		s := t.CompletedAt.Format(time.RFC3339)
		resp.CompletedAt = &s
	}
	return resp
}

func (s *Server) handleListTorrents(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc == nil {
		writeJSON(w, http.StatusOK, []torrentResponse{})
		return
	}
	torrents, err := s.torrentSvc.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing torrents")
		return
	}
	resp := make([]torrentResponse, 0, len(torrents))
	for _, t := range torrents {
		resp = append(resp, toTorrentResponse(t))
	}
	writeJSON(w, http.StatusOK, resp)
}

type addMagnetRequest struct {
	MagnetURI  string  `json:"magnetUri"`
	LibraryID  *string `json:"libraryId"`
	Sequential bool    `json:"sequential"`
}

func (s *Server) handleAddMagnet(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc == nil {
		writeError(w, http.StatusServiceUnavailable, torrentServiceUnavailable)
		return
	}
	var req addMagnetRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.MagnetURI == "" {
		writeError(w, http.StatusBadRequest, "magnetUri is required")
		return
	}
	if req.LibraryID != nil {
		if _, err := s.store.GetLibrary(*req.LibraryID); err != nil {
			s.writeStoreErr(w, err, "loading library")
			return
		}
	}

	t, err := s.torrentSvc.AddMagnet(req.MagnetURI, req.LibraryID, req.Sequential)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, toTorrentResponse(t))
}

// handleAddTorrentFile accepts a raw .torrent file body. libraryId and
// sequential are passed as query parameters since the body is the file
// itself, not JSON.
func (s *Server) handleAddTorrentFile(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc == nil {
		writeError(w, http.StatusServiceUnavailable, torrentServiceUnavailable)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil || len(data) == 0 {
		writeError(w, http.StatusBadRequest, "missing or unreadable torrent file body")
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
	sequential := r.URL.Query().Get("sequential") == "true"

	t, err := s.torrentSvc.AddTorrentFile(data, libraryID, sequential)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, toTorrentResponse(t))
}

func (s *Server) handleRemoveTorrent(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc == nil {
		writeError(w, http.StatusServiceUnavailable, torrentServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	deleteFiles := r.URL.Query().Get("deleteFiles") == "true"
	if err := s.torrentSvc.Remove(id, deleteFiles); err != nil {
		s.writeStoreErr(w, err, "removing torrent")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type torrentSearchResult struct {
	IndexerName string `json:"indexerName"`
	Title       string `json:"title"`
	SizeBytes   int64  `json:"sizeBytes"`
	Seeders     int    `json:"seeders"`
	Peers       int    `json:"peers"`
	DownloadURL string `json:"downloadUrl"`
	PublishedAt string `json:"publishedAt,omitempty"`
}

func (s *Server) handleTorrentSearch(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc == nil {
		writeError(w, http.StatusServiceUnavailable, torrentServiceUnavailable)
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	results, err := s.torrentSvc.Search(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "searching indexers")
		return
	}
	resp := make([]torrentSearchResult, 0, len(results))
	for _, res := range results {
		item := torrentSearchResult{
			IndexerName: res.IndexerName,
			Title:       res.Title,
			SizeBytes:   res.SizeBytes,
			Seeders:     res.Seeders,
			Peers:       res.Peers,
			DownloadURL: res.DownloadURL,
		}
		if !res.PublishedAt.IsZero() {
			item.PublishedAt = res.PublishedAt.Format(time.RFC3339)
		}
		resp = append(resp, item)
	}
	writeJSON(w, http.StatusOK, resp)
}

type torrentIndexerResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	BaseURL   string `json:"baseUrl"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"createdAt"`
}

func toTorrentIndexerResponse(idx *store.TorrentIndexer) torrentIndexerResponse {
	return torrentIndexerResponse{
		ID:        idx.ID,
		Name:      idx.Name,
		BaseURL:   idx.BaseURL,
		Enabled:   idx.Enabled,
		CreatedAt: idx.CreatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleListTorrentIndexers(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc == nil {
		writeJSON(w, http.StatusOK, []torrentIndexerResponse{})
		return
	}
	indexers, err := s.torrentSvc.ListIndexers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing indexers")
		return
	}
	resp := make([]torrentIndexerResponse, 0, len(indexers))
	for _, idx := range indexers {
		resp = append(resp, toTorrentIndexerResponse(idx))
	}
	writeJSON(w, http.StatusOK, resp)
}

type createIndexerRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
}

func (s *Server) handleCreateTorrentIndexer(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc == nil {
		writeError(w, http.StatusServiceUnavailable, torrentServiceUnavailable)
		return
	}
	var req createIndexerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "name and baseUrl are required")
		return
	}
	idx, err := s.torrentSvc.AddIndexer(req.Name, req.BaseURL, req.APIKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "creating indexer")
		return
	}
	writeJSON(w, http.StatusCreated, toTorrentIndexerResponse(idx))
}

type testIndexerRequest struct {
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
}

// handleTestTorrentIndexer checks a Torznab indexer's base URL/API key
// (via its capabilities document) using whatever's currently in the
// add-indexer form, without requiring it to be saved first.
func (s *Server) handleTestTorrentIndexer(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc == nil {
		writeError(w, http.StatusServiceUnavailable, torrentServiceUnavailable)
		return
	}
	var req testIndexerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "baseUrl is required")
		return
	}
	if err := torrent.TestIndexer(r.Context(), req.BaseURL, req.APIKey); err != nil {
		writeJSON(w, http.StatusOK, testResultResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, testResultResponse{OK: true})
}

func (s *Server) handleDeleteTorrentIndexer(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc == nil {
		writeError(w, http.StatusServiceUnavailable, torrentServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := s.torrentSvc.RemoveIndexer(id); err != nil {
		s.writeStoreErr(w, err, "deleting indexer")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
