package httpapi

import (
	"net/http"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

type libraryResponse struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Folders []string `json:"folders"`
}

func toLibraryResponse(l *store.Library) libraryResponse {
	return libraryResponse{ID: l.ID, Name: l.Name, Type: l.Type, Folders: l.Folders}
}

// canAccessLibrary reports whether user may see libraryID: admins see
// everything, everyone else needs an explicit grant.
func (s *Server) canAccessLibrary(user *store.User, libraryID string) (bool, error) {
	if user.IsAdmin {
		return true, nil
	}
	ids, err := s.store.GetUserLibraryPermissions(user.ID)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		if id == libraryID {
			return true, nil
		}
	}
	return false, nil
}

// handleListLibraries returns every library for admins, or only the
// libraries a non-admin user has been granted access to.
func (s *Server) handleListLibraries(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())

	libs, err := s.store.ListLibraries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing libraries")
		return
	}

	var allowed map[string]bool
	if !user.IsAdmin {
		ids, err := s.store.GetUserLibraryPermissions(user.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "loading permissions")
			return
		}
		allowed = make(map[string]bool, len(ids))
		for _, id := range ids {
			allowed[id] = true
		}
	}

	resp := make([]libraryResponse, 0, len(libs))
	for _, l := range libs {
		if !user.IsAdmin && !allowed[l.ID] {
			continue
		}
		resp = append(resp, toLibraryResponse(l))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetLibrary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	user := userFromContext(r.Context())

	ok, err := s.canAccessLibrary(user, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checking permissions")
		return
	}
	if !ok {
		writeError(w, http.StatusForbidden, "no access to this library")
		return
	}

	lib, err := s.store.GetLibrary(id)
	if err != nil {
		s.writeStoreErr(w, err, "loading library")
		return
	}
	writeJSON(w, http.StatusOK, toLibraryResponse(lib))
}

type createLibraryRequest struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Folders []string `json:"folders"`
}

func (s *Server) handleCreateLibrary(w http.ResponseWriter, r *http.Request) {
	var req createLibraryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || (req.Type != "movie" && req.Type != "series") || len(req.Folders) == 0 {
		writeError(w, http.StatusBadRequest, "name, type ('movie' or 'series'), and at least one folder are required")
		return
	}

	lib, err := s.store.CreateLibrary(req.Name, req.Type, req.Folders)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "creating library")
		return
	}
	writeJSON(w, http.StatusCreated, toLibraryResponse(lib))
}

type updateLibraryRequest struct {
	Name    string   `json:"name,omitempty"`
	Folders []string `json:"folders,omitempty"`
}

func (s *Server) handleUpdateLibrary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req updateLibraryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.store.UpdateLibrary(id, req.Name, req.Folders); err != nil {
		s.writeStoreErr(w, err, "updating library")
		return
	}
	lib, err := s.store.GetLibrary(id)
	if err != nil {
		s.writeStoreErr(w, err, "loading updated library")
		return
	}
	writeJSON(w, http.StatusOK, toLibraryResponse(lib))
}

func (s *Server) handleDeleteLibrary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteLibrary(id); err != nil {
		s.writeStoreErr(w, err, "deleting library")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
