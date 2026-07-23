package httpapi

import (
	"errors"
	"net/http"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/auth"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing users")
		return
	}
	resp := make([]userResponse, 0, len(users))
	for _, u := range users {
		resp = append(resp, toUserResponse(u))
	}
	writeJSON(w, http.StatusOK, resp)
}

type createUserRequest struct {
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	IsAdmin    bool     `json:"isAdmin"`
	LibraryIDs []string `json:"libraryIds"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Username) < 3 || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "username must be >=3 chars and password >=8 chars")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hashing password")
		return
	}
	user, err := s.store.CreateUser(req.Username, hash, req.IsAdmin)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "username already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "creating user")
		return
	}

	if len(req.LibraryIDs) > 0 {
		if err := s.store.SetUserLibraryPermissions(user.ID, req.LibraryIDs); err != nil {
			writeError(w, http.StatusInternalServerError, "setting library permissions")
			return
		}
	}

	writeJSON(w, http.StatusCreated, toUserResponse(user))
}

type updateUserRequest struct {
	Password *string `json:"password,omitempty"`
	IsAdmin  *bool   `json:"isAdmin,omitempty"`
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req updateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Password != nil {
		if len(*req.Password) < 8 {
			writeError(w, http.StatusBadRequest, "password must be >=8 chars")
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "hashing password")
			return
		}
		if err := s.store.UpdateUserPassword(id, hash); err != nil {
			s.writeStoreErr(w, err, "updating user")
			return
		}
	}

	if req.IsAdmin != nil {
		if err := s.store.UpdateUserAdmin(id, *req.IsAdmin); err != nil {
			s.writeStoreErr(w, err, "updating user")
			return
		}
	}

	user, err := s.store.GetUserByID(id)
	if err != nil {
		s.writeStoreErr(w, err, "fetching updated user")
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if requester := userFromContext(r.Context()); requester != nil && requester.ID == id {
		writeError(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}
	if err := s.store.DeleteUser(id); err != nil {
		s.writeStoreErr(w, err, "deleting user")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type setPermissionsRequest struct {
	LibraryIDs []string `json:"libraryIds"`
}

func (s *Server) handleSetUserPermissions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req setPermissionsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.store.SetUserLibraryPermissions(id, req.LibraryIDs); err != nil {
		s.writeStoreErr(w, err, "setting library permissions")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeStoreErr(w http.ResponseWriter, err error, msg string) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusInternalServerError, msg)
}
