package httpapi

import (
	"errors"
	"net/http"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/auth"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// plexToken pulls the client's token out of whichever transport a
// Plex-protocol client used: the X-Plex-Token header (most common) or the
// X-Plex-Token query parameter (used on stream/image URLs handed to players
// that don't attach custom headers).
func plexToken(r *http.Request) string {
	if t := r.Header.Get("X-Plex-Token"); t != "" {
		return t
	}
	return r.URL.Query().Get("X-Plex-Token")
}

// authenticatePlex resolves a Plex-style token into a Vorn user using the
// same sessions table every other client-API surface does -- see
// handlePlexSignIn for how the token is minted.
func (s *Server) authenticatePlex(r *http.Request) (*store.User, error) {
	token := plexToken(r)
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

func (s *Server) withPlexAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := s.authenticatePlex(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next(w, r.WithContext(withUser(r.Context(), user)))
	}
}
