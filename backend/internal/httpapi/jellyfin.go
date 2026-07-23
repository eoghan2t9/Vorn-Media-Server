package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/auth"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/jellyfin"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/transcode"
	"github.com/google/uuid"
)

// jellyfinCompatVersion/embyCompatVersion are reported so clients that gate
// features on server version see something recent; they do not imply Vorn
// implements every endpoint that version actually shipped (see package
// jellyfin's doc comment for what's actually covered). Jellyfin deliberately
// uses a "10.x" scheme so it can never collide with Emby's "4.x" one, so a
// client speaking to Vorn under the /emby prefix needs the latter or it may
// misinterpret the server's capabilities.
const (
	jellyfinCompatVersion = "10.9.0"
	embyCompatVersion     = "4.8.8.0"
)

// decodeJSONLenient decodes a JSON body without decodeJSON's
// DisallowUnknownFields: Jellyfin clients send many fields Vorn doesn't
// model (PlaySessionId, IsPaused, AudioStreamIndex, ...), so strict decoding
// would reject every real request.
func decodeJSONLenient(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func (s *Server) handleJfPublicSystemInfo(w http.ResponseWriter, r *http.Request) {
	version, productName := jellyfinCompatVersion, "Jellyfin Server"
	if strings.HasPrefix(r.URL.Path, "/emby/") {
		version, productName = embyCompatVersion, "Emby Server"
	}
	writeJSON(w, http.StatusOK, jellyfin.PublicSystemInfo{
		ServerName:             "Vorn",
		Version:                version,
		ProductName:            productName,
		OperatingSystem:        "Linux",
		Id:                     s.serverID,
		StartupWizardCompleted: true,
	})
}

type jfAuthRequest struct {
	Username string `json:"Username"`
	Pw       string `json:"Pw"`
}

func (s *Server) handleJfAuthenticateByName(w http.ResponseWriter, r *http.Request) {
	var req jfAuthRequest
	if err := decodeJSONLenient(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := s.store.GetUserByUsername(req.Username)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err := auth.VerifyPassword(user.PasswordHash, req.Pw); err != nil {
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

	writeJSON(w, http.StatusOK, jellyfin.AuthenticationResult{
		User: jellyfin.UserDto{
			Id:                    user.ID,
			Name:                  user.Username,
			ServerId:              s.serverID,
			HasPassword:           true,
			HasConfiguredPassword: true,
			Policy:                jellyfin.UserPolicy{IsAdministrator: user.IsAdmin, EnableAllFolders: true},
		},
		SessionInfo: jellyfin.SessionInfo{Id: tokenHash[:16], UserId: user.ID, UserName: user.Username},
		AccessToken: token,
		ServerId:    s.serverID,
	})
}

// toJfItem converts a media item into a BaseItemDto, filling in the poster/
// playback-state extras that live outside the MediaItem struct itself.
func (s *Server) toJfItem(user *store.User, m *store.MediaItem) jellyfin.BaseItemDto {
	poster, backdrop, _ := s.store.GetMediaItemImageURLs(m.ID)
	playback, _ := s.store.GetPlaybackState(user.ID, m.ID)
	return jellyfin.MediaItem(m, s.serverID, poster, backdrop, playback)
}

func (s *Server) toJfQueryResult(user *store.User, items []*store.MediaItem) jellyfin.QueryResult[jellyfin.BaseItemDto] {
	out := make([]jellyfin.BaseItemDto, 0, len(items))
	for _, m := range items {
		out = append(out, s.toJfItem(user, m))
	}
	return jellyfin.QueryResult[jellyfin.BaseItemDto]{Items: out, TotalRecordCount: len(out)}
}

func (s *Server) handleJfUserViews(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	libs, err := s.store.ListLibraries()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing libraries")
		return
	}

	items := make([]jellyfin.BaseItemDto, 0, len(libs))
	for _, l := range libs {
		ok, err := s.canAccessLibrary(user, l.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "checking permissions")
			return
		}
		if ok {
			items = append(items, jellyfin.LibraryItem(l, s.serverID))
		}
	}
	writeJSON(w, http.StatusOK, jellyfin.QueryResult[jellyfin.BaseItemDto]{Items: items, TotalRecordCount: len(items)})
}

// handleJfItems serves both /Items and /Users/{userId}/Items: real Jellyfin
// clients call it with ParentId set to a library (top-level movies/series)
// or a media item (series -> seasons, season -> episodes). No ParentId at
// all is treated the same as /Views, since some clients call it that way.
func (s *Server) handleJfItems(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	parentID := r.URL.Query().Get("ParentId")
	if parentID == "" {
		s.handleJfUserViews(w, r)
		return
	}

	if lib, err := s.store.GetLibrary(parentID); err == nil {
		ok, err := s.canAccessLibrary(user, lib.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "checking permissions")
			return
		}
		if !ok {
			writeError(w, http.StatusForbidden, "no access to this library")
			return
		}
		mediaItems, err := s.store.ListMediaItems(lib.ID, store.ListItemsOptions{})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "listing items")
			return
		}
		writeJSON(w, http.StatusOK, s.toJfQueryResult(user, mediaItems))
		return
	}

	item, err := s.store.GetMediaItem(parentID)
	if err != nil {
		s.writeStoreErr(w, err, "loading parent item")
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
	writeJSON(w, http.StatusOK, s.toJfQueryResult(user, children))
}

func (s *Server) handleJfItem(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	item, err := s.store.GetMediaItem(r.PathValue("id"))
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
	writeJSON(w, http.StatusOK, s.toJfItem(user, item))
}

// handleJfItemImage is intentionally unauthenticated: it just 302s to a
// public TMDb-hosted image URL, so there is nothing sensitive to gate and
// gating it would break clients that fetch images without attaching a token.
func (s *Server) handleJfItemImage(w http.ResponseWriter, r *http.Request) {
	poster, backdrop, err := s.store.GetMediaItemImageURLs(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "item not found")
		return
	}
	url := poster
	if strings.EqualFold(r.PathValue("type"), "backdrop") {
		url = backdrop
	}
	if url == "" {
		writeError(w, http.StatusNotFound, "no image available")
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

// handleJfPlaybackInfo always offers a direct-play/direct-stream source
// pointed at Vorn's own video stream endpoint; see the jellyfin package doc
// comment for why Jellyfin's own transcode-session protocol isn't
// implemented here.
func (s *Server) handleJfPlaybackInfo(w http.ResponseWriter, r *http.Request) {
	item := s.itemForPlayback(w, r, r.PathValue("id"))
	if item == nil {
		return
	}

	source := jellyfin.MediaSourceInfo{
		Id:                   item.ID,
		Protocol:             "File",
		IsRemote:             isRemoteURL(*item.Path),
		SupportsDirectPlay:   true,
		SupportsDirectStream: true,
		SupportsTranscoding:  s.transcodeMgr != nil,
		DirectStreamUrl:      "/Videos/" + item.ID + "/stream",
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if info, err := transcode.Probe(ctx, *item.Path); err == nil {
		ticks := jellyfin.SecondsToTicks(info.DurationSeconds)
		source.RunTimeTicks = &ticks
		source.MediaStreams = []jellyfin.MediaStream{
			{Type: "Video", Codec: info.VideoCodec, Index: 0, Width: info.Width, Height: info.Height, IsDefault: true},
			{Type: "Audio", Codec: info.AudioCodec, Index: 1, IsDefault: true},
		}
	}

	writeJSON(w, http.StatusOK, jellyfin.PlaybackInfoResponse{
		MediaSources:  []jellyfin.MediaSourceInfo{source},
		PlaySessionId: uuid.NewString(),
	})
}

// handleJfVideoStream matches /Videos/{id}/{filename} -- Jellyfin clients
// request things like "stream", "stream.mp4", or "stream.mkv", none of
// which net/http's ServeMux can express as a single pattern alongside a
// literal "stream" prefix, so any filename is accepted and only the id is
// used. Streaming itself reuses handleDirectStream's exact logic (local
// ServeFile, or a redirect for a remote debrid URL).
func (s *Server) handleJfVideoStream(w http.ResponseWriter, r *http.Request) {
	s.handleDirectStream(w, r)
}

type jfPlaybackProgressRequest struct {
	ItemId        string `json:"ItemId"`
	PositionTicks int64  `json:"PositionTicks"`
}

// jfUpdateProgress backs Sessions/Playing, .../Progress, and .../Stopped:
// Vorn's playback_state only tracks a position/duration pair, so all three
// Jellyfin events collapse into the same upsert. PositionTicks is the only
// field of the request Vorn's own progress tracking needs.
func (s *Server) jfUpdateProgress(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())

	var req jfPlaybackProgressRequest
	if err := decodeJSONLenient(r, &req); err != nil || req.ItemId == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	position := jellyfin.TicksToSeconds(req.PositionTicks)
	duration := 0.0
	if existing, err := s.store.GetPlaybackState(user.ID, req.ItemId); err == nil {
		duration = existing.DurationSeconds
	}
	if duration == 0 {
		if item, err := s.store.GetMediaItem(req.ItemId); err == nil && item.Path != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			if info, err := transcode.Probe(ctx, *item.Path); err == nil {
				duration = info.DurationSeconds
			}
			cancel()
		}
	}

	_ = s.store.UpsertPlaybackState(user.ID, req.ItemId, position, duration)
	w.WriteHeader(http.StatusNoContent)
}
