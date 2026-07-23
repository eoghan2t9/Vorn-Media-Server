package httpapi

import (
	"net/http"
	"path/filepath"
)

// handleArtwork serves embedded cover art the scanner extracted from audio
// files (see scanner.extractAndCacheArtwork) -- filepath.Base strips any
// directory components from the requested name, the same path-traversal
// guard handleSessionFile uses, so a crafted key can't escape the cache dir.
// Gated by plain auth (like every other item-adjacent asset): the cache key
// is an opaque hash, not tied to a specific library's permission check, but
// cover art isn't sensitive content and this is already more restrictive
// than TMDb's unauthenticated public CDN.
func (s *Server) handleArtwork(w http.ResponseWriter, r *http.Request) {
	if s.scanner == nil {
		writeError(w, http.StatusNotFound, "artwork not found")
		return
	}
	key := filepath.Base(r.PathValue("key"))
	http.ServeFile(w, r, filepath.Join(s.scanner.ArtworkCacheDir(), key))
}
