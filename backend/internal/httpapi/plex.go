package httpapi

import (
	"context"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/auth"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/plex"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/transcode"
)

// plexCompatVersion is reported at /identity; it does not imply Vorn
// implements every endpoint that Plex Media Server version actually shipped
// (see package plex's doc comment for what's actually covered).
const plexCompatVersion = "1.32.0.0"

func (s *Server) handlePlexIdentity(w http.ResponseWriter, r *http.Request) {
	claimed := true
	writeJSON(w, http.StatusOK, plex.Envelope(plex.MediaContainer{
		MachineIdentifier: s.serverID,
		Claimed:           &claimed,
		Version:           plexCompatVersion,
	}))
}

// plexSignInCredentials accepts every credential transport real Plex client
// libraries use against plex.tv's users/sign_in.json: HTTP Basic auth, a
// JSON body, or classic Rails-style nested form params (user[login]).
func plexSignInCredentials(r *http.Request) (username, password string) {
	if u, p, ok := r.BasicAuth(); ok {
		return u, p
	}
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		var req struct {
			Login    string `json:"login"`
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := decodeJSONLenient(r, &req); err == nil {
			username = req.Login
			if username == "" {
				username = req.Username
			}
			password = req.Password
			return
		}
	}
	if err := r.ParseForm(); err == nil {
		for _, key := range []string{"user[login]", "login", "username"} {
			if v := r.PostFormValue(key); v != "" {
				username = v
				break
			}
		}
		for _, key := range []string{"user[password]", "password"} {
			if v := r.PostFormValue(key); v != "" {
				password = v
				break
			}
		}
	}
	return
}

// handlePlexSignIn mimics plex.tv's users/sign_in.json well enough for
// token-oriented Plex tooling to authenticate directly against Vorn instead
// of plex.tv (see package plex's doc comment for why official Plex apps
// can't use this).
func (s *Server) handlePlexSignIn(w http.ResponseWriter, r *http.Request) {
	username, password := plexSignInCredentials(r)
	if username == "" {
		writeError(w, http.StatusBadRequest, "missing credentials")
		return
	}

	user, err := s.store.GetUserByUsername(username)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err := auth.VerifyPassword(user.PasswordHash, password); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	token, tokenHash, err := auth.NewSessionToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "creating session")
		return
	}
	if err := s.store.CreateSession(tokenHash, user.ID, time.Now().Add(auth.SessionTTL)); err != nil {
		writeError(w, http.StatusInternalServerError, "creating session")
		return
	}

	writeJSON(w, http.StatusCreated, plex.SignInResponse{
		User: plex.SignInUser{
			UUID:      user.ID,
			Username:  user.Username,
			Title:     user.Username,
			AuthToken: token,
		},
	})
}

func (s *Server) toPlexMetadata(user *store.User, m *store.MediaItem, durationSeconds float64) plex.Metadata {
	poster, backdrop, _ := s.store.GetMediaItemImageURLs(m.ID)
	playback, _ := s.store.GetPlaybackState(user.ID, m.ID)
	return plex.MetadataFromItem(m, durationSeconds, poster, backdrop, playback)
}

func (s *Server) toPlexMetadataList(user *store.User, items []*store.MediaItem) []plex.Metadata {
	out := make([]plex.Metadata, 0, len(items))
	for _, m := range items {
		// Duration is left at 0 for listings: probing every file with
		// ffprobe just to render a browse screen would be far too slow.
		// It's filled in properly by handlePlexMetadataItem's detail view.
		out = append(out, s.toPlexMetadata(user, m, 0))
	}
	return out
}

func (s *Server) handlePlexSections(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	libs, err := s.store.ListLibraries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing libraries")
		return
	}

	dirs := make([]plex.Directory, 0, len(libs))
	for _, l := range libs {
		ok, err := s.canAccessLibrary(user, l.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "checking permissions")
			return
		}
		if ok {
			dirs = append(dirs, plex.DirectoryFromLibrary(l))
		}
	}
	writeJSON(w, http.StatusOK, plex.Envelope(plex.MediaContainer{Size: len(dirs), Directory: dirs}))
}

func (s *Server) handlePlexSectionItems(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	lib, err := s.store.GetLibrary(r.PathValue("sectionId"))
	if err != nil {
		s.writeStoreErr(w, err, "loading library")
		return
	}
	ok, err := s.canAccessLibrary(user, lib.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checking permissions")
		return
	}
	if !ok {
		writeError(w, http.StatusForbidden, "no access to this library")
		return
	}

	items, err := s.store.ListMediaItems(lib.ID, store.ListItemsOptions{})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing items")
		return
	}
	metadata := s.toPlexMetadataList(user, items)
	writeJSON(w, http.StatusOK, plex.Envelope(plex.MediaContainer{Size: len(metadata), Metadata: metadata}))
}

func (s *Server) handlePlexMetadataChildren(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	item, err := s.store.GetMediaItem(r.PathValue("ratingKey"))
	if err != nil {
		s.writeStoreErr(w, err, "loading item")
		return
	}
	ok, err := s.canAccessLibrary(user, item.LibraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checking permissions")
		return
	}
	if !ok {
		writeError(w, http.StatusForbidden, "no access to this item")
		return
	}

	children, err := s.store.ListChildren(item.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading children")
		return
	}
	metadata := s.toPlexMetadataList(user, children)
	writeJSON(w, http.StatusOK, plex.Envelope(plex.MediaContainer{Size: len(metadata), Metadata: metadata}))
}

// handlePlexMetadataItem is the one endpoint that probes the real file (via
// ffprobe) to populate the Media/Part hierarchy a Plex client needs to
// actually play something -- series/season items have no file (item.Path is
// nil) and are returned without a Media entry, same as a real Plex folder.
func (s *Server) handlePlexMetadataItem(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	item, err := s.store.GetMediaItem(r.PathValue("ratingKey"))
	if err != nil {
		s.writeStoreErr(w, err, "loading item")
		return
	}
	ok, err := s.canAccessLibrary(user, item.LibraryID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "checking permissions")
		return
	}
	if !ok {
		writeError(w, http.StatusForbidden, "no access to this item")
		return
	}

	durationSeconds := 0.0
	var media []plex.Media
	if item.Path != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		info, probeErr := transcode.Probe(ctx, *item.Path)
		cancel()
		if probeErr == nil {
			durationSeconds = info.DurationSeconds
			container := strings.TrimPrefix(strings.ToLower(filepath.Ext(*item.Path)), ".")
			partKey := "/library/parts/" + item.ID + "/file." + container
			media = []plex.Media{plex.MediaFromItem(item, durationSeconds, container, info.VideoCodec, info.AudioCodec, info.Width, info.Height, partKey)}
		}
	}

	md := s.toPlexMetadata(user, item, durationSeconds)
	md.Media = media
	writeJSON(w, http.StatusOK, plex.Envelope(plex.MediaContainer{Size: 1, Metadata: []plex.Metadata{md}}))
}

// handlePlexPartFile matches /library/parts/{id}/{filename} and streams the
// same way handleDirectStream does (local file, or a redirect for a remote
// debrid URL) -- the filename Plex clients append is cosmetic, only the id
// path segment matters.
func (s *Server) handlePlexPartFile(w http.ResponseWriter, r *http.Request) {
	s.handleDirectStream(w, r)
}

// handlePlexTimeline backs Plex's /:/timeline progress-reporting endpoint,
// which clients poll periodically during playback with ratingKey/time/
// duration query params (time and duration in milliseconds).
func (s *Server) handlePlexTimeline(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	q := r.URL.Query()
	ratingKey := q.Get("ratingKey")
	if ratingKey == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	timeMs, _ := strconv.ParseInt(q.Get("time"), 10, 64)
	durationMs, _ := strconv.ParseInt(q.Get("duration"), 10, 64)
	position := plex.MillisToSeconds(timeMs)
	duration := plex.MillisToSeconds(durationMs)
	if duration == 0 {
		if existing, err := s.store.GetPlaybackState(user.ID, ratingKey); err == nil {
			duration = existing.DurationSeconds
		}
	}
	_ = s.store.UpsertPlaybackState(user.ID, ratingKey, position, duration)
	w.WriteHeader(http.StatusOK)
}
