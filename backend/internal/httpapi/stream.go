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
	Mode              string `json:"mode"` // "direct" | "transcode" | "acquiring"
	DirectURL         string `json:"directUrl,omitempty"`
	SessionID         string `json:"sessionId,omitempty"`
	PlaylistURL       string `json:"playlistUrl,omitempty"`
	AcquisitionStatus string `json:"acquisitionStatus,omitempty"` // "searching" | "acquiring" | "error", only set when Mode == "acquiring"
	AcquisitionError  string `json:"acquisitionError,omitempty"`
}

// loadItemForPlayback loads a media item and checks the caller has access
// to its library (or writes an error response and returns nil). Whether a
// playable file/stream actually exists yet is left to the caller: a
// placeholder item legitimately has no path, and handlePlayItem needs to
// treat that as "kick off acquisition" rather than an error the way
// handleDirectStream still must.
func (s *Server) loadItemForPlayback(w http.ResponseWriter, r *http.Request, id string) *store.MediaItem {
	user := userFromContext(r.Context())
	item, err := s.store.GetMediaItem(id)
	if err != nil {
		s.writeStoreErr(w, err, "loading item")
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

// itemForPlayback is loadItemForPlayback plus the strict "must already have
// a file/stream" guard every caller except handlePlayItem still needs
// (client-API-compat playback info, subtitle extraction, direct file
// serving -- none of those know how to wait on an in-progress acquisition).
func (s *Server) itemForPlayback(w http.ResponseWriter, r *http.Request, id string) *store.MediaItem {
	item := s.loadItemForPlayback(w, r, id)
	if item == nil {
		return nil
	}
	if item.Path == nil || *item.Path == "" {
		writeError(w, http.StatusUnprocessableEntity, "item has no playable file")
		return nil
	}
	return item
}

func (s *Server) handlePlayItem(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item := s.loadItemForPlayback(w, r, id)
	if item == nil {
		return
	}

	if item.Path == nil || *item.Path == "" {
		s.handleAcquiringPlay(w, r, item)
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

// handleAcquiringPlay is handlePlayItem's branch for an item with no
// path/stream yet: a freshly-opened placeholder starts acquisition, one
// already in flight just reports its current status, and a previously
// failed attempt is reported so the frontend can offer Retry (which lands
// back here and re-tries since Acquire treats 'error' the same as
// 'placeholder').
func (s *Server) handleAcquiringPlay(w http.ResponseWriter, r *http.Request, item *store.MediaItem) {
	switch item.AcquisitionStatus {
	case "searching", "acquiring":
		writeJSON(w, http.StatusAccepted, playResponse{Mode: "acquiring", AcquisitionStatus: item.AcquisitionStatus})
	case "placeholder", "error":
		if s.acquisition == nil {
			writeError(w, http.StatusServiceUnavailable, "on-demand acquisition is not configured")
			return
		}
		if err := s.acquisition.Acquire(r.Context(), item.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "starting acquisition")
			return
		}
		writeJSON(w, http.StatusAccepted, playResponse{Mode: "acquiring", AcquisitionStatus: "searching"})
	default:
		// "owned" with no path is an inconsistent state that shouldn't
		// happen -- surface the original hard failure rather than silently
		// starting acquisition on an item that was never a placeholder.
		writeError(w, http.StatusUnprocessableEntity, "item has no playable file")
	}
}

func (s *Server) handleDirectStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	item := s.itemForPlayback(w, r, id)
	if item == nil {
		return
	}
	// Debrid-backed items store a provider CDN URL as their path rather than
	// a local file: redirect the player straight to it instead of trying to
	// serve it off local disk (there is none -- that's the point of
	// direct-stream-from-debrid).
	if isRemoteURL(*item.Path) {
		http.Redirect(w, r, *item.Path, http.StatusFound)
		return
	}
	http.ServeFile(w, r, *item.Path)
}

func isRemoteURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
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
