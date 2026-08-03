package httpapi

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/torrent"
)

const torrentServiceUnavailable = "torrent acquisition is not configured (set VORN_TORRENT_ENABLED=true)"

type magnetResponse struct {
	MagnetURI string `json:"magnetUri"`
}

// handleMagnetFromFile accepts a raw .torrent file body and returns the
// magnet URI extracted from it -- a pure conversion, no side effects, so
// admins can upload a .torrent file and then resolve the resulting magnet
// through a debrid provider (POST /api/debrid/links) exactly like a pasted
// magnet link. Vorn never downloads the torrent itself.
func (s *Server) handleMagnetFromFile(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil || len(data) == 0 {
		writeError(w, http.StatusBadRequest, "missing or unreadable torrent file body")
		return
	}
	magnetURI, err := torrent.MagnetFromTorrentBytes(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, magnetResponse{MagnetURI: magnetURI})
}

type magnetFromURLRequest struct {
	DownloadURL string `json:"downloadUrl"`
}

// handleMagnetFromURL is handleMagnetFromFile's counterpart for a search
// result whose DownloadURL points at a .torrent file rather than being a
// magnet URI already -- fetches it server-side (indexers generally don't
// send permissive CORS headers) and converts it the same way.
func (s *Server) handleMagnetFromURL(w http.ResponseWriter, r *http.Request) {
	var req magnetFromURLRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.DownloadURL == "" {
		writeError(w, http.StatusBadRequest, "downloadUrl is required")
		return
	}
	httpReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, req.DownloadURL, nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid downloadUrl")
		return
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, "fetching torrent file")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("fetching torrent file returned status %d", resp.StatusCode))
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil || len(data) == 0 {
		writeError(w, http.StatusBadGateway, "empty or unreadable torrent file")
		return
	}
	magnetURI, err := torrent.MagnetFromTorrentBytes(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, magnetResponse{MagnetURI: magnetURI})
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
	if s.torrentSvc.Load() == nil {
		writeError(w, http.StatusServiceUnavailable, torrentServiceUnavailable)
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	// See handleNZBSearch for the full reasoning: a bare IMDb ID or explicit
	// "tvdb:12345" query is useless as free text, so it's resolved to a
	// plain title via TMDb (for indexers with no id-search support at all,
	// confirmed against a live EZTV instance) and searched both ways,
	// merging whatever either finds.
	var imdbID, tvdbID string
	if imdbIDPattern.MatchString(q) {
		imdbID = q
	} else if m := tvdbIDPattern.FindStringSubmatch(q); m != nil {
		tvdbID = m[1]
	}

	var results []torrent.SearchResult
	if imdbID != "" || tvdbID != "" {
		if title := resolveIDToTitle(r.Context(), s.tmdb.Load(), imdbID, tvdbID); title != "" {
			if titleResults, err := s.torrentSvc.Load().Search(r.Context(), title); err != nil {
				log.Printf("httpapi: title-based torrent search for %q: %v", title, err)
			} else {
				results = append(results, titleResults...)
			}
		}
		if idResults, err := s.torrentSvc.Load().SearchByIMDb(r.Context(), imdbID, tvdbID, 0, 0); err != nil {
			log.Printf("httpapi: id-based torrent search for %q: %v", q, err)
		} else {
			results = append(results, idResults...)
		}
	} else {
		res, err := s.torrentSvc.Load().Search(r.Context(), q)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "searching indexers")
			return
		}
		results = res
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
	ID                 string `json:"id"`
	Name               string `json:"name"`
	BaseURL            string `json:"baseUrl"`
	Provider           string `json:"provider"`
	Enabled            bool   `json:"enabled"`
	CreatedAt          string `json:"createdAt"`
	SupportsImdbSearch bool   `json:"supportsImdbSearch"`
	SupportsTvdbSearch bool   `json:"supportsTvdbSearch"`
	DisabledReason     string `json:"disabledReason,omitempty"`
}

func toTorrentIndexerResponse(idx *store.TorrentIndexer) torrentIndexerResponse {
	return torrentIndexerResponse{
		ID:                 idx.ID,
		Name:               idx.Name,
		BaseURL:            idx.BaseURL,
		Provider:           idx.Provider,
		Enabled:            idx.Enabled,
		CreatedAt:          idx.CreatedAt.Format(time.RFC3339),
		SupportsImdbSearch: idx.SupportsImdbSearch,
		SupportsTvdbSearch: idx.SupportsTvdbSearch,
		DisabledReason:     idx.DisabledReason,
	}
}

func (s *Server) handleListTorrentIndexers(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc.Load() == nil {
		writeJSON(w, http.StatusOK, []torrentIndexerResponse{})
		return
	}
	indexers, err := s.torrentSvc.Load().ListIndexers()
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
	Name     string `json:"name"`
	BaseURL  string `json:"baseUrl"`
	APIKey   string `json:"apiKey"`
	Provider string `json:"provider"` // "torznab" (default) | "torbox"
}

func (s *Server) handleCreateTorrentIndexer(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc.Load() == nil {
		writeError(w, http.StatusServiceUnavailable, torrentServiceUnavailable)
		return
	}
	var req createIndexerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// A torbox-provider indexer has no base URL of its own (the endpoint is
	// fixed internally, see torrent.torBoxSearchBaseURL) -- only Torznab
	// indexers need one supplied.
	if req.Name == "" || (req.Provider != "torbox" && req.BaseURL == "") {
		writeError(w, http.StatusBadRequest, "name and baseUrl are required")
		return
	}
	idx, err := s.torrentSvc.Load().AddIndexer(r.Context(), req.Name, req.BaseURL, req.APIKey, req.Provider)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toTorrentIndexerResponse(idx))
}

type testIndexerRequest struct {
	BaseURL  string `json:"baseUrl"`
	APIKey   string `json:"apiKey"`
	Provider string `json:"provider"`
}

// handleTestTorrentIndexer checks an indexer's base URL/API key (Torznab's
// capabilities document, or a validation query against TorBox's own
// torrent-search API) using whatever's currently in the add-indexer form,
// without requiring it to be saved first.
func (s *Server) handleTestTorrentIndexer(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc.Load() == nil {
		writeError(w, http.StatusServiceUnavailable, torrentServiceUnavailable)
		return
	}
	var req testIndexerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Provider == "torbox" {
		if err := torrent.TestTorBoxIndexer(r.Context(), req.APIKey); err != nil {
			writeJSON(w, http.StatusOK, testResultResponse{OK: false, Error: err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, testResultResponse{OK: true})
		return
	}
	if req.BaseURL == "" {
		writeError(w, http.StatusBadRequest, "baseUrl is required")
		return
	}
	caps, err := torrent.TestIndexer(r.Context(), req.BaseURL, req.APIKey)
	if err != nil {
		writeJSON(w, http.StatusOK, testResultResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, testResultResponse{OK: true, SupportsImdbSearch: caps.SupportsImdb, SupportsTvdbSearch: caps.SupportsTvdb})
}

// updateIndexerRequest fields are pointers so an omitted field leaves it
// unchanged -- in particular, an admin editing name/baseUrl shouldn't have
// to resend the API key, and handleListTorrentIndexers never echoes it back
// for them to resend in the first place. An explicit empty string clears
// the API key, distinct from omitting the field.
type updateIndexerRequest struct {
	Name     *string `json:"name"`
	BaseURL  *string `json:"baseUrl"`
	APIKey   *string `json:"apiKey"`
	Provider *string `json:"provider"`
	Enabled  *bool   `json:"enabled"`
}

func (s *Server) handleUpdateTorrentIndexer(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc.Load() == nil {
		writeError(w, http.StatusServiceUnavailable, torrentServiceUnavailable)
		return
	}
	var req updateIndexerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id := r.PathValue("id")
	idx, err := s.torrentSvc.Load().UpdateIndexer(id, store.UpdateTorrentIndexerInput{
		Name:     req.Name,
		BaseURL:  req.BaseURL,
		APIKey:   req.APIKey,
		Provider: req.Provider,
		Enabled:  req.Enabled,
	})
	if err != nil {
		s.writeStoreErr(w, err, "updating indexer")
		return
	}
	writeJSON(w, http.StatusOK, toTorrentIndexerResponse(idx))
}

func (s *Server) handleDeleteTorrentIndexer(w http.ResponseWriter, r *http.Request) {
	if s.torrentSvc.Load() == nil {
		writeError(w, http.StatusServiceUnavailable, torrentServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := s.torrentSvc.Load().RemoveIndexer(id); err != nil {
		s.writeStoreErr(w, err, "deleting indexer")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
