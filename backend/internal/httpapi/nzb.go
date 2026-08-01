package httpapi

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/metadata"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/nzb"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const nzbServiceUnavailable = "NZB acquisition is not configured (set VORN_NZB_ENABLED=true)"

// imdbIDPattern recognizes a bare IMDb ID (e.g. "tt0295701") typed into a
// manual search box -- shared by handleNZBSearch and handleTorrentSearch,
// both of which otherwise only ever send it as a literal free-text query
// (which finds nothing, since no real release title contains the ID
// itself as text) rather than routing it through the ID-based search
// functions acquisition already uses internally.
var imdbIDPattern = regexp.MustCompile(`^tt\d+$`)

// tvdbIDPattern recognizes an explicit "tvdb:12345" query -- unlike an IMDb
// ID, a bare TheTVDB id is just a plain number, indistinguishable from a
// normal (if unusual) free-text search term, so it needs an explicit
// prefix rather than being auto-detected the way imdbIDPattern is.
var tvdbIDPattern = regexp.MustCompile(`(?i)^tvdb:(\d+)$`)

// resolveIDToTitle looks up a plain title for imdbID/tvdbID (exactly one
// expected non-empty) via TMDb, for indexers that don't support id-based
// search at all -- confirmed against a live EZTV instance, whose tv-search
// only accepts q/season/ep, no imdbid/tvdbid, so an id-only query there
// finds nothing no matter how it's sent. "" (both no error and no title)
// is a normal outcome (TMDb has no configured client, or no match) --
// callers just skip the extra free-text search in that case.
func resolveIDToTitle(ctx context.Context, tmdbClient *metadata.TMDbClient, imdbID, tvdbID string) string {
	if tmdbClient == nil {
		return ""
	}
	source, id := "imdb_id", imdbID
	if tvdbID != "" {
		source, id = "tvdb_id", tvdbID
	}
	title, err := tmdbClient.FindByExternalID(ctx, id, source)
	if err != nil {
		log.Printf("httpapi: resolving %s %s to a title: %v", source, id, err)
		return ""
	}
	return title
}

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
	if s.nzbSvc.Load() == nil {
		writeJSON(w, http.StatusOK, []nzbDownloadResponse{})
		return
	}
	downloads, err := s.nzbSvc.Load().List()
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
	if s.nzbSvc.Load() == nil {
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

	n, err := s.nzbSvc.Load().AddNZB(context.Background(), data, libraryID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, toNZBDownloadResponse(n))
}

func (s *Server) handleRemoveNZB(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc.Load() == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := s.nzbSvc.Load().Remove(id); err != nil {
		s.writeStoreErr(w, err, "removing nzb download")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type usenetServerResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"createdAt"`
}

func toUsenetServerResponse(u *store.UsenetServer) usenetServerResponse {
	return usenetServerResponse{
		ID:        u.ID,
		Name:      u.Name,
		Enabled:   u.Enabled,
		CreatedAt: u.CreatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleListUsenetServers(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc.Load() == nil {
		writeJSON(w, http.StatusOK, []usenetServerResponse{})
		return
	}
	servers, err := s.nzbSvc.Load().ListServers()
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
	Name   string `json:"name"`
	APIKey string `json:"apiKey"`
}

func (s *Server) handleCreateUsenetServer(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc.Load() == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	var req createUsenetServerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.APIKey == "" {
		writeError(w, http.StatusBadRequest, "apiKey is required")
		return
	}
	u, err := s.nzbSvc.Load().AddServer(store.UsenetServer{
		Name:   req.Name,
		APIKey: req.APIKey,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "creating usenet server")
		return
	}
	writeJSON(w, http.StatusCreated, toUsenetServerResponse(u))
}

type testUsenetServerRequest struct {
	APIKey string `json:"apiKey"`
}

type testResultResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// handleTestUsenetServer verifies a TorBox API key using whatever's
// currently in the add-server form, without requiring it to be saved first
// -- a bad key otherwise wouldn't surface until the first real download
// attempt fails.
func (s *Server) handleTestUsenetServer(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc.Load() == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	var req testUsenetServerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.APIKey == "" {
		writeError(w, http.StatusBadRequest, "apiKey is required")
		return
	}
	if err := s.nzbSvc.Load().TestTorBoxAccount(r.Context(), req.APIKey); err != nil {
		writeJSON(w, http.StatusOK, testResultResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, testResultResponse{OK: true})
}

// updateUsenetServerRequest fields are pointers so an omitted field leaves it
// unchanged -- see updateIndexerRequest in torrents.go for the same
// reasoning applied to apiKey here. An explicit empty string clears the
// apiKey, distinct from omitting the field.
type updateUsenetServerRequest struct {
	Name    *string `json:"name"`
	APIKey  *string `json:"apiKey"`
	Enabled *bool   `json:"enabled"`
}

func (s *Server) handleUpdateUsenetServer(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc.Load() == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	var req updateUsenetServerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id := r.PathValue("id")
	u, err := s.nzbSvc.Load().UpdateServer(id, store.UpdateUsenetServerInput{
		Name:    req.Name,
		APIKey:  req.APIKey,
		Enabled: req.Enabled,
	})
	if err != nil {
		s.writeStoreErr(w, err, "updating usenet server")
		return
	}
	writeJSON(w, http.StatusOK, toUsenetServerResponse(u))
}

func (s *Server) handleDeleteUsenetServer(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc.Load() == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := s.nzbSvc.Load().RemoveServer(id); err != nil {
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
	if s.nzbSvc.Load() == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	// A bare IMDb ID (e.g. "tt0295701") or an explicit "tvdb:12345" query
	// would otherwise only ever be sent as a literal free-text query, which
	// finds nothing on real indexers -- confirmed live against NZBGeek:
	// t=search&q=tt0295701 returns zero results, while
	// t=movie&imdbid=0295701 returns real releases. So for either shape,
	// skip searching the raw id text and instead: (1) run the same
	// id-based search acquisition uses internally, and (2) resolve the id
	// to a plain title via TMDb and search *that* as free text too --
	// needed because some real indexers (confirmed against a live EZTV
	// instance) don't support id-based tv-search at all, only q/season/ep.
	var imdbID, tvdbID string
	if imdbIDPattern.MatchString(q) {
		imdbID = q
	} else if m := tvdbIDPattern.FindStringSubmatch(q); m != nil {
		tvdbID = m[1]
	}

	var results []nzb.SearchResult
	if imdbID != "" || tvdbID != "" {
		if title := resolveIDToTitle(r.Context(), s.tmdb.Load(), imdbID, tvdbID); title != "" {
			if titleResults, err := s.nzbSvc.Load().Search(r.Context(), title); err != nil {
				log.Printf("httpapi: title-based NZB search for %q: %v", title, err)
			} else {
				results = append(results, titleResults...)
			}
		}
		if idResults, err := s.nzbSvc.Load().SearchByIMDb(r.Context(), imdbID, tvdbID, 0, 0); err != nil {
			log.Printf("httpapi: id-based NZB search for %q: %v", q, err)
		} else {
			results = append(results, idResults...)
		}
	} else {
		res, err := s.nzbSvc.Load().Search(r.Context(), q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "searching indexers")
			return
		}
		results = res
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
	if s.nzbSvc.Load() == nil {
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
	n, err := s.nzbSvc.Load().AddNZBFromURL(r.Context(), req.DownloadURL, req.LibraryID)
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
	if s.nzbSvc.Load() == nil {
		writeJSON(w, http.StatusOK, []nzbIndexerResponse{})
		return
	}
	indexers, err := s.nzbSvc.Load().ListIndexers()
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
	if s.nzbSvc.Load() == nil {
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
	idx, err := s.nzbSvc.Load().AddIndexer(req.Name, req.BaseURL, req.APIKey)
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
	if s.nzbSvc.Load() == nil {
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

func (s *Server) handleUpdateNZBIndexer(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc.Load() == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	var req updateIndexerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id := r.PathValue("id")
	idx, err := s.nzbSvc.Load().UpdateIndexer(id, store.UpdateNZBIndexerInput{
		Name:    req.Name,
		BaseURL: req.BaseURL,
		APIKey:  req.APIKey,
		Enabled: req.Enabled,
	})
	if err != nil {
		s.writeStoreErr(w, err, "updating indexer")
		return
	}
	writeJSON(w, http.StatusOK, toNZBIndexerResponse(idx))
}

// handleNZBEvents streams Server-Sent Events whenever a download completes
// via the background sync. Clients (the Admin NZB page) use this instead
// of polling the list endpoint every 2 seconds.
func (s *Server) handleNZBEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	ns := s.nzbSvc.Load()
	if ns == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, unsub := ns.Subscribe()
	defer unsub()

	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: update\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleDeleteNZBIndexer(w http.ResponseWriter, r *http.Request) {
	if s.nzbSvc.Load() == nil {
		writeError(w, http.StatusServiceUnavailable, nzbServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := s.nzbSvc.Load().RemoveIndexer(id); err != nil {
		s.writeStoreErr(w, err, "deleting indexer")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
