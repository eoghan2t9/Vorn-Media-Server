package httpapi

import "net/http"

type libraryResponse struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Folders []string `json:"folders"`
}

// handleListLibraries returns every library for admins, or only the
// libraries a non-admin user has been granted access to. Full library
// management (create/edit/rescan) lands in Phase 3; this endpoint exists now
// so user permission assignment has something to reference.
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
		resp = append(resp, libraryResponse{ID: l.ID, Name: l.Name, Type: l.Type, Folders: l.Folders})
	}
	writeJSON(w, http.StatusOK, resp)
}
