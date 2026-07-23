package httpapi

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// defaultBrowseRoot is where the directory browser starts when no path is
// given. /media is where the docker-compose deployment mounts the library
// volume (see deploy/docker-compose.yml); bare-metal installs won't have it,
// so fall back to the filesystem root.
func defaultBrowseRoot() string {
	if info, err := os.Stat("/media"); err == nil && info.IsDir() {
		return "/media"
	}
	return "/"
}

type browseEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type browseResponse struct {
	Path        string        `json:"path"`
	Parent      string        `json:"parent,omitempty"`
	Directories []browseEntry `json:"directories"`
}

// handleBrowseFilesystem lists subdirectories of a server-side path, for the
// admin "browse for a folder" picker used when mapping library locations --
// this only ever needs directories, not files, so files in the listing are
// silently skipped rather than surfaced as unselectable clutter. It's
// admin-gated (see the route registration) the same way Radarr/Sonarr/etc.
// expose an unrestricted server-side folder browser to admins only.
func (s *Server) handleBrowseFilesystem(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = defaultBrowseRoot()
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		writeError(w, http.StatusBadRequest, "path must be absolute")
		return
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "cannot read directory: "+err.Error())
		return
	}

	dirs := make([]browseEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dirs = append(dirs, browseEntry{Name: e.Name(), Path: filepath.Join(path, e.Name())})
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name) < strings.ToLower(dirs[j].Name) })

	resp := browseResponse{Path: path, Directories: dirs}
	if parent := filepath.Dir(path); parent != path {
		resp.Parent = parent
	}
	writeJSON(w, http.StatusOK, resp)
}
