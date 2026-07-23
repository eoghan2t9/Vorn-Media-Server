package httpapi

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/auth"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// jellyfinTokenRe extracts Token="..." out of a MediaBrowser/Emby auth
// header: `MediaBrowser Client="...", Device="...", DeviceId="...", Token="..."`
var jellyfinTokenRe = regexp.MustCompile(`(?i)token="?([^",]+)"?`)

// jellyfinToken pulls the client's access token out of whichever transport a
// Jellyfin-compatible client used: a bare X-Emby-Token/X-MediaBrowser-Token
// header, an api_key query param (common on stream URLs), or the Token=
// field of an Authorization/X-Emby-Authorization "MediaBrowser ..." header.
func jellyfinToken(r *http.Request) string {
	if t := r.Header.Get("X-Emby-Token"); t != "" {
		return t
	}
	if t := r.Header.Get("X-MediaBrowser-Token"); t != "" {
		return t
	}
	if t := r.URL.Query().Get("api_key"); t != "" {
		return t
	}
	for _, h := range []string{"X-Emby-Authorization", "Authorization"} {
		if v := r.Header.Get(h); v != "" {
			if m := jellyfinTokenRe.FindStringSubmatch(v); m != nil {
				return m[1]
			}
		}
	}
	return ""
}

// authenticateJellyfin resolves a Jellyfin-style token into a Vorn user
// using the same sessions table handleLogin/withAuth do -- AuthenticateByName
// (see handleJfAuthenticateByName) just hands the opaque token back as
// AccessToken instead of setting a cookie.
func (s *Server) authenticateJellyfin(r *http.Request) (*store.User, error) {
	token := jellyfinToken(r)
	if token == "" {
		return nil, auth.ErrInvalidCredentials
	}
	sess, err := s.store.GetSession(auth.HashToken(token))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, auth.ErrInvalidCredentials
		}
		return nil, err
	}
	return s.store.GetUserByID(sess.UserID)
}

func (s *Server) withJellyfinAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := s.authenticateJellyfin(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next(w, r.WithContext(withUser(r.Context(), user)))
	}
}
