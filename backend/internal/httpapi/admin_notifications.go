package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

type notificationSettingsResponse struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhookUrl"`
}

func (s *Server) handleGetNotificationSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetNotificationSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading notification settings")
		return
	}
	writeJSON(w, http.StatusOK, notificationSettingsResponse{Enabled: settings.Enabled, WebhookURL: settings.WebhookURL})
}

type updateNotificationSettingsRequest struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhookUrl"`
}

func (s *Server) handleUpdateNotificationSettings(w http.ResponseWriter, r *http.Request) {
	var req updateNotificationSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Enabled && req.WebhookURL == "" {
		writeError(w, http.StatusBadRequest, "webhookUrl is required to enable notifications")
		return
	}
	if err := s.store.SetNotificationSettings(store.NotificationSettings{Enabled: req.Enabled, WebhookURL: req.WebhookURL}); err != nil {
		writeError(w, http.StatusInternalServerError, "saving notification settings")
		return
	}
	s.handleGetNotificationSettings(w, r)
}

// handleTestNotification sends a "test" event through the currently
// configured webhook, without requiring settings to be saved as enabled
// first -- lets an admin verify a URL actually works before flipping it on.
func (s *Server) handleTestNotification(w http.ResponseWriter, r *http.Request) {
	if s.notify == nil {
		writeError(w, http.StatusServiceUnavailable, "notifications are not configured")
		return
	}
	var req updateNotificationSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WebhookURL == "" {
		writeError(w, http.StatusBadRequest, "webhookUrl is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	s.notify.SendTo(ctx, req.WebhookURL, "test", map[string]any{"message": "This is a test notification from Vorn."})
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}
