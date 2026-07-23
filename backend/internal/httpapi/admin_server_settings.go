package httpapi

import (
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

type serverSettingsResponse struct {
	CustomDomain    string `json:"customDomain"`
	ACMEEmail       string `json:"acmeEmail"`
	SSLEnabled      bool   `json:"sslEnabled"`
	TrustCloudflare bool   `json:"trustCloudflare"`
	UpdatedAt       string `json:"updatedAt"`
}

func toServerSettingsResponse(ss *store.ServerSettings) serverSettingsResponse {
	return serverSettingsResponse{
		CustomDomain:    ss.CustomDomain,
		ACMEEmail:       ss.ACMEEmail,
		SSLEnabled:      ss.SSLEnabled,
		TrustCloudflare: ss.TrustCloudflare,
		UpdatedAt:       ss.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *Server) handleGetServerSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetServerSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading server settings")
		return
	}
	writeJSON(w, http.StatusOK, toServerSettingsResponse(settings))
}

type updateServerSettingsRequest struct {
	CustomDomain    string `json:"customDomain"`
	ACMEEmail       string `json:"acmeEmail"`
	SSLEnabled      bool   `json:"sslEnabled"`
	TrustCloudflare bool   `json:"trustCloudflare"`
}

// handleUpdateServerSettings persists custom-domain/SSL and Cloudflare-trust
// settings. Note that a custom domain/SSL change only takes effect on the
// next restart (certmagic's ACME setup runs once at daemon startup); this
// endpoint saves the setting for that next restart to pick up, it doesn't
// hot-reload TLS. TrustCloudflare, by contrast, takes effect immediately --
// the access-log middleware reads Server.trustCloudflare on every request.
func (s *Server) handleUpdateServerSettings(w http.ResponseWriter, r *http.Request) {
	var req updateServerSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SSLEnabled && req.CustomDomain == "" {
		writeError(w, http.StatusBadRequest, "customDomain is required when sslEnabled is true")
		return
	}

	settings, err := s.store.UpdateServerSettings(store.UpdateServerSettingsInput{
		CustomDomain:    req.CustomDomain,
		ACMEEmail:       req.ACMEEmail,
		SSLEnabled:      req.SSLEnabled,
		TrustCloudflare: req.TrustCloudflare,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "saving server settings")
		return
	}

	s.trustCloudflare.Store(settings.TrustCloudflare)
	writeJSON(w, http.StatusOK, toServerSettingsResponse(settings))
}
