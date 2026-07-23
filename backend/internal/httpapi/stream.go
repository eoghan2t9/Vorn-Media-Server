package httpapi

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/transcode"
	"github.com/google/uuid"
)

type capabilitiesResponse struct {
	Backends []string `json:"backends"`
}

func (s *Server) handleTranscodeCapabilities(w http.ResponseWriter, r *http.Request) {
	if s.transcodeMgr == nil {
		writeJSON(w, http.StatusOK, capabilitiesResponse{Backends: nil})
		return
	}
	names := make([]string, 0)
	for _, b := range s.transcodeMgr.Capabilities() {
		names = append(names, b.Name)
	}
	writeJSON(w, http.StatusOK, capabilitiesResponse{Backends: names})
}

type playResponse struct {
	Mode        string `json:"mode"` // "direct" | "transcode"
	DirectURL   string `json:"directUrl,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
	PlaylistURL string `json:"playlistUrl,omitempty"`
}

// itemForPlayback loads a media item, checks the caller has access to its
// library, and returns it (or writes an error response and returns nil).
func (s *Server) itemForPlayback(w http.ResponseWriter, r *http.Request, id string) *store.MediaItem {
	user := userFromContext(r.Context())
	item, err := s.store.GetMediaItem(id)
	if err != nil {
		s.writeStoreErr(w, err, "loading item")
		return nil
	}
	if item.Path == nil || *item.Path == "" {
		writeError(w, http.StatusUnprocessableEntity, "item has no playable file")
		return nil
	}
	ok, err := s.canAccessLibrary(user, item.LibraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checking permissions")
		return nil
	}
	if !ok {
		writeError(w, http.StatusForbidden, "no access to this item")
		return nil
	}
	return item
}

func (s *Server) handlePlayItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item := s.itemForPlayback(w, r, id)
	if item == nil {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	info, err := transcode.Probe(ctx, *item.Path)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "probing media file")
		return
	}

	if transcode.Decide(info) == transcode.ModeDirect {
		writeJSON(w, http.StatusOK, playResponse{Mode: "direct", DirectURL: "/api/stream/direct/" + item.ID})
		return
	}

	if s.transcodeMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "transcoding is not available (ffmpeg not detected)")
		return
	}

	sessionID := uuid.NewString()
	sess, err := s.transcodeMgr.StartSession(context.Background(), sessionID, *item.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "starting transcode session")
		return
	}
	writeJSON(w, http.StatusAccepted, playResponse{
		Mode:        "transcode",
		SessionID:   sess.ID,
		PlaylistURL: "/api/stream/session/" + sess.ID + "/playlist.m3u8",
	})
}

func (s *Server) handleDirectStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item := s.itemForPlayback(w, r, id)
	if item == nil {
		return
	}
	http.ServeFile(w, r, *item.Path)
}

// handleSessionFile serves playlist/segment files out of a transcode
// session's output directory. filepath.Base strips any directory
// components from the requested name so a path-traversal attempt (e.g.
// "../../etc/passwd") can't escape the session directory.
func (s *Server) handleSessionFile(w http.ResponseWriter, r *http.Request) {
	if s.transcodeMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "transcoding is not available")
		return
	}
	sessionID := r.PathValue("sessionId")
	file := filepath.Base(r.PathValue("file"))

	sess, ok := s.transcodeMgr.Get(sessionID)
	if !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	fullPath := filepath.Join(sess.OutputDir, file)
	if !strings.HasPrefix(fullPath, sess.OutputDir) {
		writeError(w, http.StatusBadRequest, "invalid file")
		return
	}

	if strings.HasSuffix(file, ".m3u8") {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	} else if strings.HasSuffix(file, ".ts") {
		w.Header().Set("Content-Type", "video/mp2t")
	}
	http.ServeFile(w, r, fullPath)
}

func (s *Server) handleStopSession(w http.ResponseWriter, r *http.Request) {
	if s.transcodeMgr == nil {
		writeError(w, http.StatusServiceUnavailable, "transcoding is not available")
		return
	}
	sessionID := r.PathValue("sessionId")
	if err := s.transcodeMgr.Stop(sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, "stopping session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
