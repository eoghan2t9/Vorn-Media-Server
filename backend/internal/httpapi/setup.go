package httpapi

import (
	"net/http"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/auth"
)

type setupStatusResponse struct {
	Completed bool `json:"completed"`
}

func (s *Server) isSetupComplete() (bool, error) {
	n, err := s.store.CountUsers()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	completed, err := s.isSetupComplete()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checking setup status")
		return
	}
	writeJSON(w, http.StatusOK, setupStatusResponse{Completed: completed})
}

type setupInitRequest struct {
	AdminUsername string `json:"adminUsername"`
	AdminPassword string `json:"adminPassword"`
	LibraryName   string `json:"libraryName"`
	LibraryType   string `json:"libraryType"`
	LibraryPath   string `json:"libraryPath"`
}

type setupInitResponse struct {
	AdminUsername string `json:"adminUsername"`
	LibraryID     string `json:"libraryId,omitempty"`
}

// handleSetupInit is the first-launch wizard: it may only run once, before any
// user exists. It creates the initial admin account and (optionally) the
// first library. GPU capability probing is surfaced separately by the
// transcoder subsystem once it exists (see Phase 5); the wizard does not
// fabricate hardware capabilities in the meantime.
func (s *Server) handleSetupInit(w http.ResponseWriter, r *http.Request) {
	completed, err := s.isSetupComplete()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checking setup status")
		return
	}
	if completed {
		writeError(w, http.StatusConflict, "setup has already been completed")
		return
	}

	var req setupInitRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.AdminUsername) < 3 || len(req.AdminPassword) < 8 {
		writeError(w, http.StatusBadRequest, "username must be >=3 chars and password >=8 chars")
		return
	}

	hash, err := auth.HashPassword(req.AdminPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hashing password")
		return
	}
	admin, err := s.store.CreateUser(req.AdminUsername, hash, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "creating admin user")
		return
	}

	resp := setupInitResponse{AdminUsername: admin.Username}

	if req.LibraryName != "" && req.LibraryPath != "" {
		libType := req.LibraryType
		if libType == "" {
			libType = "movie"
		}
		lib, err := s.store.CreateLibrary(req.LibraryName, libType, []string{req.LibraryPath})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "creating initial library")
			return
		}
		resp.LibraryID = lib.ID
	}

	writeJSON(w, http.StatusCreated, resp)
}
