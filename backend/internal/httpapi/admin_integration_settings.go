package httpapi

import (
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// integrationSettingsResponse deliberately never echoes the stored secrets
// back to the client -- only whether each integration is configured, plus
// the OpenSubtitles username (not itself a secret, and useful for an admin
// to confirm which account is wired up).
type integrationSettingsResponse struct {
	TMDbConfigured           bool   `json:"tmdbConfigured"`
	OpenSubtitlesConfigured  bool   `json:"openSubtitlesConfigured"`
	OpenSubtitlesUsername    string `json:"openSubtitlesUsername,omitempty"`
	MusicMetadataEnabled     bool   `json:"musicMetadataEnabled"`
	AudiobookMetadataEnabled bool   `json:"audiobookMetadataEnabled"`
	UpdatedAt                string `json:"updatedAt"`
}

func toIntegrationSettingsResponse(is *store.IntegrationSettings) integrationSettingsResponse {
	return integrationSettingsResponse{
		TMDbConfigured:           is.TMDbAPIKey != "",
		OpenSubtitlesConfigured:  is.OpenSubtitlesAPIKey != "" && is.OpenSubtitlesUsername != "",
		OpenSubtitlesUsername:    is.OpenSubtitlesUsername,
		MusicMetadataEnabled:     is.MusicMetadataEnabled,
		AudiobookMetadataEnabled: is.AudiobookMetadataEnabled,
		UpdatedAt:                is.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleGetIntegrationSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetIntegrationSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading integration settings")
		return
	}
	writeJSON(w, http.StatusOK, toIntegrationSettingsResponse(settings))
}

// updateIntegrationSettingsRequest fields are pointers so a field omitted
// from the JSON body leaves the stored credential unchanged -- otherwise
// rotating just the TMDb key would blank out OpenSubtitles credentials the
// admin never intended to touch (the API never sends secrets back for the
// frontend to round-trip). Send an empty string explicitly to clear a field.
type updateIntegrationSettingsRequest struct {
	TMDbAPIKey               *string `json:"tmdbApiKey"`
	OpenSubtitlesAPIKey      *string `json:"openSubtitlesApiKey"`
	OpenSubtitlesUsername    *string `json:"openSubtitlesUsername"`
	OpenSubtitlesPassword    *string `json:"openSubtitlesPassword"`
	MusicMetadataEnabled     *bool   `json:"musicMetadataEnabled"`
	AudiobookMetadataEnabled *bool   `json:"audiobookMetadataEnabled"`
}

func (s *Server) handleUpdateIntegrationSettings(w http.ResponseWriter, r *http.Request) {
	var req updateIntegrationSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	settings, err := s.store.UpdateIntegrationSettings(store.UpdateIntegrationSettingsInput{
		TMDbAPIKey:               req.TMDbAPIKey,
		OpenSubtitlesAPIKey:      req.OpenSubtitlesAPIKey,
		OpenSubtitlesUsername:    req.OpenSubtitlesUsername,
		OpenSubtitlesPassword:    req.OpenSubtitlesPassword,
		MusicMetadataEnabled:     req.MusicMetadataEnabled,
		AudiobookMetadataEnabled: req.AudiobookMetadataEnabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "saving integration settings")
		return
	}
	writeJSON(w, http.StatusOK, toIntegrationSettingsResponse(settings))
}
