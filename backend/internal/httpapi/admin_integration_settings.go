package httpapi

import (
	"log"
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
	FanartConfigured         bool   `json:"fanartConfigured"`
	OMDbConfigured           bool   `json:"omdbConfigured"`
	TVDbConfigured           bool   `json:"tvdbConfigured"`
	// TorrentEnabled/NZBEnabled are the *effective* value -- the DB toggle if
	// an admin has ever set it, else whatever VORN_TORRENT_ENABLED/
	// VORN_NZB_ENABLED resolved to at boot (see Server.reconfigure).
	TorrentEnabled bool   `json:"torrentEnabled"`
	NZBEnabled     bool   `json:"nzbEnabled"`
	UpdatedAt      string `json:"updatedAt"`
}

func (s *Server) toIntegrationSettingsResponse(is *store.IntegrationSettings) integrationSettingsResponse {
	torrentEnabled := s.baseCfg.TorrentEnabled
	if is.TorrentEnabled != nil {
		torrentEnabled = *is.TorrentEnabled
	}
	nzbEnabled := s.baseCfg.NZBEnabled
	if is.NZBEnabled != nil {
		nzbEnabled = *is.NZBEnabled
	}
	return integrationSettingsResponse{
		TMDbConfigured:           is.TMDbAPIKey != "",
		OpenSubtitlesConfigured:  is.OpenSubtitlesAPIKey != "" && is.OpenSubtitlesUsername != "",
		OpenSubtitlesUsername:    is.OpenSubtitlesUsername,
		MusicMetadataEnabled:     is.MusicMetadataEnabled,
		AudiobookMetadataEnabled: is.AudiobookMetadataEnabled,
		FanartConfigured:         is.FanartAPIKey != "",
		OMDbConfigured:           is.OMDbAPIKey != "",
		TVDbConfigured:           is.TVDbAPIKey != "",
		TorrentEnabled:           torrentEnabled,
		NZBEnabled:               nzbEnabled,
		UpdatedAt:                is.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleGetIntegrationSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetIntegrationSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading integration settings")
		return
	}
	writeJSON(w, http.StatusOK, s.toIntegrationSettingsResponse(settings))
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
	FanartAPIKey             *string `json:"fanartApiKey"`
	OMDbAPIKey               *string `json:"omdbApiKey"`
	TVDbAPIKey               *string `json:"tvdbApiKey"`
	TVDbPin                  *string `json:"tvdbPin"`
	TorrentEnabled           *bool   `json:"torrentEnabled"`
	NZBEnabled               *bool   `json:"nzbEnabled"`
}

// handleUpdateIntegrationSettings saves whatever credentials/toggles the
// admin sent, then calls reconfigure so the change takes effect immediately
// -- a new TMDb key is used by the very next metadata sync, and a torrent/
// NZB toggle starts or fully stops that subsystem right away, no restart.
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
		FanartAPIKey:             req.FanartAPIKey,
		OMDbAPIKey:               req.OMDbAPIKey,
		TVDbAPIKey:               req.TVDbAPIKey,
		TVDbPin:                  req.TVDbPin,
		TorrentEnabled:           req.TorrentEnabled,
		NZBEnabled:               req.NZBEnabled,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "saving integration settings")
		return
	}
	if err := s.reconfigure(); err != nil {
		log.Printf("httpapi: reconfiguring after integration settings save: %v", err)
	}
	writeJSON(w, http.StatusOK, s.toIntegrationSettingsResponse(settings))
}
