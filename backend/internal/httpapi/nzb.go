package httpapi

import (
	"io"
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/nzb"
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

type testUsenetServerRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	UseTLS   bool   `json:"useTls"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type testResultResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// handleTestUsenetServer dials and authenticates against a Usenet server
// using whatever's currently in the add-server form, without requiring it
// to be saved first -- a bad host/port/credential combo otherwise wouldn't
// surface until the first real download attempt fails.
func (s *Server) handleTestUsenetServer(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	var req testUsenetServerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Host == "" || req.Port == 0 {
		writeError(w, http.StatusBadRequest, "host and port are required")
		return
	}
	if err := s.nzbSvc.TestServer(req.Host, req.Port, req.UseTLS, req.Username, req.Password); err != nil {
		writeJSON(w, http.StatusOK, testResultResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, testResultResponse{OK: true})
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

type nzbSearchResult struct {
	IndexerName string `json:"indexerName"`
	Title       string `json:"title"`
	SizeBytes   int64  `json:"sizeBytes"`
	DownloadURL string `json:"downloadUrl"`
	PublishedAt string `json:"publishedAt,omitempty"`
}

func (s *Server) handleNZBSearch(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	results, err := s.nzbSvc.Search(r.Context(), q)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "searching indexers")
		return
	}
	resp := make([]nzbSearchResult, 0, len(results))
	for _, res := range results {
		item := nzbSearchResult{
			IndexerName: res.IndexerName,
			Title:       res.Title,
			SizeBytes:   res.SizeBytes,
			DownloadURL: res.DownloadURL,
		}
		if !res.PublishedAt.IsZero() {
			item.PublishedAt = res.PublishedAt.Format(time.RFC3339)
		}
		resp = append(resp, item)
	}
	writeJSON(w, http.StatusOK, resp)
}

type addNZBFromURLRequest struct {
	DownloadURL string  `json:"downloadUrl"`
	LibraryID   *string `json:"libraryId"`
}

// handleAddNZBFromURL fetches the .nzb file from a search result's download
// URL server-side (indexers generally don't send permissive CORS headers,
// and the URL already embeds that indexer's own API key) and starts
// downloading it the same way an uploaded file would.
func (s *Server) handleAddNZBFromURL(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	var req addNZBFromURLRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DownloadURL == "" {
		writeError(w, http.StatusBadRequest, "downloadUrl is required")
		return
	}
	if req.LibraryID != nil {
		if _, err := s.store.GetLibrary(*req.LibraryID); err != nil {
			s.writeStoreErr(w, err, "loading library")
			return
		}
	}
	n, err := s.nzbSvc.AddNZBFromURL(r.Context(), req.DownloadURL, req.LibraryID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, toNZBDownloadResponse(n))
}

type nzbIndexerResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	BaseURL   string `json:"baseUrl"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"createdAt"`
}

func toNZBIndexerResponse(idx *store.NZBIndexer) nzbIndexerResponse {
	return nzbIndexerResponse{
		ID:        idx.ID,
		Name:      idx.Name,
		BaseURL:   idx.BaseURL,
		Enabled:   idx.Enabled,
		CreatedAt: idx.CreatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleListNZBIndexers(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc == nil {
		writeJSON(w, http.StatusOK, []nzbIndexerResponse{})
		return
	}
	indexers, err := s.nzbSvc.ListIndexers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing indexers")
		return
	}
	resp := make([]nzbIndexerResponse, 0, len(indexers))
	for _, idx := range indexers {
		resp = append(resp, toNZBIndexerResponse(idx))
	}
	writeJSON(w, http.StatusOK, resp)
}

type createNZBIndexerRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
}

func (s *Server) handleCreateNZBIndexer(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	var req createNZBIndexerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "name and baseUrl are required")
		return
	}
	idx, err := s.nzbSvc.AddIndexer(req.Name, req.BaseURL, req.APIKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "creating indexer")
		return
	}
	writeJSON(w, http.StatusCreated, toNZBIndexerResponse(idx))
}

type testNZBIndexerRequest struct {
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
}

// handleTestNZBIndexer checks a Newznab indexer's base URL/API key (via its
// capabilities document) using whatever's currently in the add-indexer
// form, without requiring it to be saved first.
func (s *Server) handleTestNZBIndexer(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	var req testNZBIndexerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "baseUrl is required")
		return
	}
	if err := nzb.TestIndexer(r.Context(), req.BaseURL, req.APIKey); err != nil {
		writeJSON(w, http.StatusOK, testResultResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, testResultResponse{OK: true})
}

func (s *Server) handleDeleteNZBIndexer(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := s.nzbSvc.RemoveIndexer(id); err != nil {
		s.writeStoreErr(w, err, "deleting indexer")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
