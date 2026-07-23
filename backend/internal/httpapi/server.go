package httpapi

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/debrid"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/metadata"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/nzb"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/torrent"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/transcode"
)

const sessionCookieName = "vorn_session"

// Deps bundles everything the HTTP layer needs. Fields other than Store are
// nil-able: each represents a subsystem that may be unconfigured (no TMDb
// key, no ffmpeg found, ...), and every handler that uses one must degrade
// gracefully (typically a 503) rather than assume it's present.
type Deps struct {
	Store        *store.Store
	Scanner      *scanner.Service
	Metadata     *metadata.Service
	TranscodeMgr *transcode.Manager
	Torrent      *torrent.Service
	NZB          *nzb.Service
	Debrid       *debrid.Service
	CORSOrigin   string
	DevMode      bool
}

type Server struct {
	store        *store.Store
	scanner      *scanner.Service
	metadataSvc  *metadata.Service
	transcodeMgr *transcode.Manager
	torrentSvc   *torrent.Service
	nzbSvc       *nzb.Service
	debridSvc    *debrid.Service
	devMode      bool
}

func NewServer(deps Deps) *Server {
	return &Server{
		store:        deps.Store,
		scanner:      deps.Scanner,
		metadataSvc:  deps.Metadata,
		transcodeMgr: deps.TranscodeMgr,
		torrentSvc:   deps.Torrent,
		nzbSvc:       deps.NZB,
		debridSvc:    deps.Debrid,
		devMode:      deps.DevMode,
	}
}

// NewRouter returns the root HTTP handler for the Vorn backend.
func NewRouter(deps Deps) http.Handler {
	s := NewServer(deps)
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", handleHealthz)

	mux.HandleFunc("GET /api/setup/status", s.handleSetupStatus)
	mux.HandleFunc("POST /api/setup/init", s.handleSetupInit)

	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/me", s.withAuth(s.handleMe))

	mux.HandleFunc("GET /api/users", s.withAdmin(s.handleListUsers))
	mux.HandleFunc("POST /api/users", s.withAdmin(s.handleCreateUser))
	mux.HandleFunc("PATCH /api/users/{id}", s.withAdmin(s.handleUpdateUser))
	mux.HandleFunc("DELETE /api/users/{id}", s.withAdmin(s.handleDeleteUser))
	mux.HandleFunc("PUT /api/users/{id}/permissions", s.withAdmin(s.handleSetUserPermissions))

	mux.HandleFunc("GET /api/libraries", s.withAuth(s.handleListLibraries))
	mux.HandleFunc("POST /api/libraries", s.withAdmin(s.handleCreateLibrary))
	mux.HandleFunc("GET /api/libraries/{id}", s.withAuth(s.handleGetLibrary))
	mux.HandleFunc("PATCH /api/libraries/{id}", s.withAdmin(s.handleUpdateLibrary))
	mux.HandleFunc("DELETE /api/libraries/{id}", s.withAdmin(s.handleDeleteLibrary))
	mux.HandleFunc("GET /api/libraries/{id}/items", s.withAuth(s.handleListLibraryItems))

	mux.HandleFunc("POST /api/libraries/{id}/scan", s.withAdmin(s.handleStartLibraryScan))
	mux.HandleFunc("GET /api/scan-jobs", s.withAdmin(s.handleListScanJobs))
	mux.HandleFunc("GET /api/scan-jobs/{id}", s.withAdmin(s.handleGetScanJob))
	mux.HandleFunc("POST /api/dev/synthetic-scan", s.withAdmin(s.handleSyntheticScan))

	mux.HandleFunc("GET /api/items/{id}", s.withAuth(s.handleGetItem))
	mux.HandleFunc("PUT /api/items/{id}/progress", s.withAuth(s.handleUpdateProgress))
	mux.HandleFunc("GET /api/items/{id}/progress", s.withAuth(s.handleGetProgress))
	mux.HandleFunc("GET /api/continue-watching", s.withAuth(s.handleContinueWatching))

	mux.HandleFunc("GET /api/admin/stats", s.withAdmin(s.handleServerStats))
	mux.HandleFunc("GET /api/admin/currently-watching", s.withAdmin(s.handleCurrentlyWatching))
	mux.HandleFunc("GET /api/search", s.withAuth(s.handleSearch))

	mux.HandleFunc("POST /api/libraries/{id}/sync-metadata", s.withAdmin(s.handleStartMetadataSync))
	mux.HandleFunc("GET /api/metadata-jobs/{id}", s.withAdmin(s.handleGetMetadataJob))
	mux.HandleFunc("PATCH /api/items/{id}/metadata", s.withAdmin(s.handleUpdateItemMetadata))

	mux.HandleFunc("GET /api/transcode/capabilities", s.withAuth(s.handleTranscodeCapabilities))
	mux.HandleFunc("POST /api/items/{id}/play", s.withAuth(s.handlePlayItem))
	mux.HandleFunc("GET /api/stream/direct/{id}", s.withAuth(s.handleDirectStream))
	mux.HandleFunc("GET /api/stream/session/{sessionId}/{file}", s.withAuth(s.handleSessionFile))
	mux.HandleFunc("DELETE /api/stream/session/{sessionId}", s.withAuth(s.handleStopSession))

	mux.HandleFunc("GET /api/torrents", s.withAdmin(s.handleListTorrents))
	mux.HandleFunc("POST /api/torrents", s.withAdmin(s.handleAddMagnet))
	mux.HandleFunc("POST /api/torrents/file", s.withAdmin(s.handleAddTorrentFile))
	mux.HandleFunc("DELETE /api/torrents/{id}", s.withAdmin(s.handleRemoveTorrent))
	mux.HandleFunc("GET /api/torrents/search", s.withAdmin(s.handleTorrentSearch))
	mux.HandleFunc("GET /api/torrent-indexers", s.withAdmin(s.handleListTorrentIndexers))
	mux.HandleFunc("POST /api/torrent-indexers", s.withAdmin(s.handleCreateTorrentIndexer))
	mux.HandleFunc("DELETE /api/torrent-indexers/{id}", s.withAdmin(s.handleDeleteTorrentIndexer))

	mux.HandleFunc("GET /api/nzb", s.withAdmin(s.handleListNZBDownloads))
	mux.HandleFunc("POST /api/nzb", s.withAdmin(s.handleAddNZB))
	mux.HandleFunc("DELETE /api/nzb/{id}", s.withAdmin(s.handleRemoveNZB))
	mux.HandleFunc("GET /api/usenet-servers", s.withAdmin(s.handleListUsenetServers))
	mux.HandleFunc("POST /api/usenet-servers", s.withAdmin(s.handleCreateUsenetServer))
	mux.HandleFunc("DELETE /api/usenet-servers/{id}", s.withAdmin(s.handleDeleteUsenetServer))

	mux.HandleFunc("GET /api/debrid-accounts", s.withAdmin(s.handleListDebridAccounts))
	mux.HandleFunc("POST /api/debrid-accounts", s.withAdmin(s.handleCreateDebridAccount))
	mux.HandleFunc("DELETE /api/debrid-accounts/{id}", s.withAdmin(s.handleDeleteDebridAccount))
	mux.HandleFunc("GET /api/debrid", s.withAdmin(s.handleListDebridItems))
	mux.HandleFunc("POST /api/debrid", s.withAdmin(s.handleAddDebridLink))
	mux.HandleFunc("DELETE /api/debrid/{id}", s.withAdmin(s.handleRemoveDebridItem))
	mux.HandleFunc("GET /api/debrid/{id}/files", s.withAdmin(s.handleListDebridFiles))

	return withCORS(mux, deps.CORSOrigin)
}

// withCORS allows the frontend dev server (or, in production, whatever
// origin the admin configures) to make credentialed (cookie-based) requests.
// Access-Control-Allow-Origin can't be "*" when credentials are allowed, so
// the configured origin is echoed back explicitly.
func withCORS(next http.Handler, origin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			log.Printf("httpapi: encoding response: %v", err)
		}
	}
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
