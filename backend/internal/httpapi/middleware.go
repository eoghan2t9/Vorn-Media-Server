package httpapi

import (
	"errors"
	"net/http"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/auth"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// authenticate resolves the session cookie on r, if any, into a user.
func (s *Server) authenticate(r *http.Request) (*store.User, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil, auth.ErrInvalidCredentials
	}
	sess, err := s.store.GetSession(auth.HashToken(cookie.Value))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, auth.ErrInvalidCredentials
		}
		return nil, err
	}
	return s.store.GetUserByID(sess.UserID)
}

// withAuth requires a valid session and injects the user into the request context.
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := s.authenticate(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next(w, r.WithContext(withUser(r.Context(), user)))
	}
}

// withAdmin requires a valid session belonging to an admin user.
func (s *Server) withAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.withAuth(func(w http.ResponseWriter, r *http.Request) {
		user := userFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			writeError(w, http.StatusForbidden, "admin access required")
			return
		}
		next(w, r)
	})
}
