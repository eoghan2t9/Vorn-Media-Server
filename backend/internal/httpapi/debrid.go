package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const debridServiceUnavailable = "debrid acquisition is not configured"

type debridAccountResponse struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"createdAt"`
}

func toDebridAccountResponse(a *store.DebridAccount) debridAccountResponse {
	return debridAccountResponse{
		ID:        a.ID,
		Provider:  a.Provider,
		Enabled:   a.Enabled,
		CreatedAt: a.CreatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleListDebridAccounts(w http.ResponseWriter, r *http.Request) {
	if s.debridSvc == nil {
		writeJSON(w, http.StatusOK, []debridAccountResponse{})
		return
	}
	accounts, err := s.debridSvc.ListAccounts()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing debrid accounts")
		return
	}
	resp := make([]debridAccountResponse, 0, len(accounts))
	for _, a := range accounts {
		resp = append(resp, toDebridAccountResponse(a))
	}
	writeJSON(w, http.StatusOK, resp)
}

type createDebridAccountRequest struct {
	Provider string `json:"provider"`
	APIKey   string `json:"apiKey"`
}

func (s *Server) handleCreateDebridAccount(w http.ResponseWriter, r *http.Request) {
	if s.debridSvc == nil {
		writeError(w, http.StatusServiceUnavailable, debridServiceUnavailable)
		return
	}
	var req createDebridAccountRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Provider == "" || req.APIKey == "" {
		writeError(w, http.StatusBadRequest, "provider and apiKey are required")
		return
	}
	account, err := s.debridSvc.AddAccount(req.Provider, req.APIKey)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toDebridAccountResponse(account))
}

type testDebridAccountRequest struct {
	Provider string `json:"provider"`
	APIKey   string `json:"apiKey"`
}

type debridTestResultResponse struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	Username string `json:"username,omitempty"`
	Premium  bool   `json:"premium,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

// handleTestDebridAccount verifies a provider/apiKey pair by fetching that
// provider's account info, without requiring the account to be saved
// first -- a bad key otherwise wouldn't surface until the first real
// resolve attempt fails.
func (s *Server) handleTestDebridAccount(w http.ResponseWriter, r *http.Request) {
	if s.debridSvc == nil {
		writeError(w, http.StatusServiceUnavailable, debridServiceUnavailable)
		return
	}
	var req testDebridAccountRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Provider == "" || req.APIKey == "" {
		writeError(w, http.StatusBadRequest, "provider and apiKey are required")
		return
	}
	info, err := s.debridSvc.TestAccount(r.Context(), req.Provider, req.APIKey)
	if err != nil {
		writeJSON(w, http.StatusOK, debridTestResultResponse{OK: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, debridTestResultResponse{OK: true, Username: info.Username, Premium: info.Premium, Detail: info.Detail})
}

func (s *Server) handleDeleteDebridAccount(w http.ResponseWriter, r *http.Request) {
	if s.debridSvc == nil {
		writeError(w, http.StatusServiceUnavailable, debridServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := s.debridSvc.RemoveAccount(id); err != nil {
		s.writeStoreErr(w, err, "deleting debrid account")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type debridItemResponse struct {
	ID        string  `json:"id"`
	LibraryID *string `json:"libraryId,omitempty"`
	AccountID string  `json:"accountId"`
	SourceRef string  `json:"sourceRef"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	Error     string  `json:"error,omitempty"`
	Promoted  bool    `json:"promoted"`
	AddedAt   string  `json:"addedAt"`
}

func toDebridItemResponse(item *store.DebridItem) debridItemResponse {
	return debridItemResponse{
		ID:        item.ID,
		LibraryID: item.LibraryID,
		AccountID: item.AccountID,
		SourceRef: item.SourceRef,
		Name:      item.Name,
		Status:    item.Status,
		Error:     item.Error,
		Promoted:  item.Promoted,
		AddedAt:   item.AddedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleListDebridItems(w http.ResponseWriter, r *http.Request) {
	if s.debridSvc == nil {
		writeJSON(w, http.StatusOK, []debridItemResponse{})
		return
	}
	items, err := s.debridSvc.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing debrid items")
		return
	}
	resp := make([]debridItemResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, toDebridItemResponse(item))
	}
	writeJSON(w, http.StatusOK, resp)
}

type addDebridLinkRequest struct {
	AccountID string  `json:"accountId"`
	SourceRef string  `json:"sourceRef"`
	Name      string  `json:"name"`
	LibraryID *string `json:"libraryId"`
}

func (s *Server) handleAddDebridLink(w http.ResponseWriter, r *http.Request) {
	if s.debridSvc == nil {
		writeError(w, http.StatusServiceUnavailable, debridServiceUnavailable)
		return
	}
	var req addDebridLinkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AccountID == "" || req.SourceRef == "" {
		writeError(w, http.StatusBadRequest, "accountId and sourceRef are required")
		return
	}
	if req.LibraryID != nil {
		if _, err := s.store.GetLibrary(*req.LibraryID); err != nil {
			s.writeStoreErr(w, err, "loading library")
			return
		}
	}

	item, err := s.debridSvc.AddLink(req.AccountID, req.SourceRef, req.Name, req.LibraryID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "debrid account not found")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, toDebridItemResponse(item))
}

func (s *Server) handleRemoveDebridItem(w http.ResponseWriter, r *http.Request) {
	if s.debridSvc == nil {
		writeError(w, http.StatusServiceUnavailable, debridServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if err := s.debridSvc.Remove(id); err != nil {
		s.writeStoreErr(w, err, "removing debrid item")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type debridFileResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	StreamURL string `json:"streamUrl"`
}

func (s *Server) handleListDebridFiles(w http.ResponseWriter, r *http.Request) {
	if s.debridSvc == nil {
		writeJSON(w, http.StatusOK, []debridFileResponse{})
		return
	}
	id := r.PathValue("id")
	files, err := s.debridSvc.ListFiles(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing debrid files")
		return
	}
	resp := make([]debridFileResponse, 0, len(files))
	for _, f := range files {
		resp = append(resp, debridFileResponse{ID: f.ID, Name: f.Name, SizeBytes: f.SizeBytes, StreamURL: f.StreamURL})
	}
	writeJSON(w, http.StatusOK, resp)
}
