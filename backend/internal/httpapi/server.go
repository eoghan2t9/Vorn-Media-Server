package httpapi

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/metadata"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/scanner"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const sessionCookieName = "vorn_session"

type Server struct {
	store       *store.Store
	scanner     *scanner.Service
	metadataSvc *metadata.Service // nil if no TMDb API key is configured
	devMode     bool
}

func NewServer(st *store.Store, sc *scanner.Service, metadataSvc *metadata.Service, devMode bool) *Server {
	return &Server{store: st, scanner: sc, metadataSvc: metadataSvc, devMode: devMode}
}

// NewRouter returns the root HTTP handler for the Vorn backend.
func NewRouter(st *store.Store, sc *scanner.Service, metadataSvc *metadata.Service, corsOrigin string, devMode bool) http.Handler {
	s := NewServer(st, sc, metadataSvc, devMode)
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
	mux.HandleFunc("GET /api/continue-watching", s.withAuth(s.handleContinueWatching))

	mux.HandleFunc("GET /api/admin/stats", s.withAdmin(s.handleServerStats))
	mux.HandleFunc("GET /api/search", s.withAuth(s.handleSearch))

	mux.HandleFunc("POST /api/libraries/{id}/sync-metadata", s.withAdmin(s.handleStartMetadataSync))
	mux.HandleFunc("GET /api/metadata-jobs/{id}", s.withAdmin(s.handleGetMetadataJob))
	mux.HandleFunc("PATCH /api/items/{id}/metadata", s.withAdmin(s.handleUpdateItemMetadata))

	return withCORS(mux, corsOrigin)
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
