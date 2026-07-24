package httpapi

import (
	"log"
	"net/http"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

type libraryResponse struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Type                 string   `json:"type"`
	Is4K                 bool     `json:"is4K"`
	DefaultRequestTarget bool     `json:"defaultRequestTarget"`
	Folders              []string `json:"folders"`
}

func toLibraryResponse(l *store.Library) libraryResponse {
	return libraryResponse{
		ID: l.ID, Name: l.Name, Type: l.Type, Is4K: l.Is4K,
		DefaultRequestTarget: l.DefaultRequestTarget, Folders: l.Folders,
	}
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

// validLibraryTypes are the library "type" values the scanner/metadata/
// player pipeline knows about. Music and audiobook libraries can be created
// today but only get as far as folder scanning finding zero items -- the
// scanner only recognizes video file extensions (see scanner.videoExtensions)
// and there's no music/audiobook metadata provider or non-video player yet.
var validLibraryTypes = map[string]bool{
	"movie":     true,
	"series":    true,
	"music":     true,
	"audiobook": true,
}

type createLibraryRequest struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Is4K    bool     `json:"is4K"`
	Folders []string `json:"folders"`
}

func (s *Server) handleCreateLibrary(w http.ResponseWriter, r *http.Request) {
	var req createLibraryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || !validLibraryTypes[req.Type] {
		writeError(w, http.StatusBadRequest, "name and a valid type (movie, series, music, or audiobook) are required")
		return
	}
	// Movie/series libraries can go folder-less -- they can be populated
	// entirely by debrid acquisition (see internal/acquisition and
	// internal/debrid), which writes each item's path as the provider's
	// stream URL and never touches the library's folder or the scanner.
	// Music/audiobook libraries have no such non-file acquisition path, so a
	// folder to scan is still mandatory for them.
	if (req.Type == "music" || req.Type == "audiobook") && len(req.Folders) == 0 {
		writeError(w, http.StatusBadRequest, "a folder is required for music/audiobook libraries")
		return
	}

	lib, err := s.store.CreateLibrary(req.Name, req.Type, req.Folders, req.Is4K)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "creating library")
		return
	}

	// Seed the quality profile to 2160p-only so the "4K" label is actually
	// true of what acquisition fetches into it, not just cosmetic -- only
	// on creation (a fresh library has no profile yet, so there's nothing
	// to clobber); toggling Is4K later via update deliberately leaves an
	// already-tuned profile alone.
	if req.Is4K && (req.Type == "movie" || req.Type == "series") {
		if _, err := s.store.UpsertQualityProfile(store.QualityProfile{
			LibraryID: lib.ID, MinResolution: "2160p", MaxResolution: "2160p", MinSeeders: 1,
		}); err != nil {
			log.Printf("libraries: seeding 4K quality profile for %s: %v", lib.ID, err)
		}
	}

	writeJSON(w, http.StatusCreated, toLibraryResponse(lib))
}

type updateLibraryRequest struct {
	Name                 string   `json:"name,omitempty"`
	Is4K                 *bool    `json:"is4K,omitempty"`
	DefaultRequestTarget *bool    `json:"defaultRequestTarget,omitempty"`
	Folders              []string `json:"folders,omitempty"`
}

func (s *Server) handleUpdateLibrary(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req updateLibraryRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.store.UpdateLibrary(id, req.Name, req.Folders, req.Is4K); err != nil {
		s.writeStoreErr(w, err, "updating library")
		return
	}
	// Group-exclusive (at most one default per type+is_4k), so it goes
	// through its own store method rather than the generic column setter.
	if req.DefaultRequestTarget != nil {
		if err := s.store.SetLibraryDefaultRequestTarget(id, *req.DefaultRequestTarget); err != nil {
			s.writeStoreErr(w, err, "updating default request target")
			return
		}
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
