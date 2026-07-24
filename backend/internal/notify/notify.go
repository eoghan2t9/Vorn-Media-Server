// Package notify posts acquisition-lifecycle events (acquired, failed,
// upgraded) to an admin-configured webhook URL -- the only outbound
// notification channel Vorn has (no email/SMTP integration exists).
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

const sendTimeout = 10 * time.Second

// Service posts a JSON payload to whatever webhook URL is currently
// configured (re-read fresh on every call -- this is event-driven, not a
// hot path, so there's no reason to cache it and risk acting on a stale
// admin setting).
type Service struct {
	store  *store.Store
	client *http.Client
}

func NewService(st *store.Store) *Service {
	return &Service{store: st, client: &http.Client{Timeout: sendTimeout}}
}

// Send posts {event, timestamp, ...payload} to the configured webhook URL.
// A send failure (disabled, unset, network error, non-2xx response) is
// logged and swallowed -- a notification is never allowed to fail the
// acquisition it's reporting on.
func (s *Service) Send(ctx context.Context, event string, payload map[string]any) {
	settings, err := s.store.GetNotificationSettings()
	if err != nil {
		log.Printf("notify: loading settings: %v", err)
		return
	}
	if !settings.Enabled || settings.WebhookURL == "" {
		return
	}
	s.SendTo(ctx, settings.WebhookURL, event, payload)
}

// SendTo posts directly to webhookURL, bypassing stored settings entirely
// -- used by the admin "send a test notification" action so it can try a
// URL that hasn't been saved (or saved-and-enabled) yet.
func (s *Service) SendTo(ctx context.Context, webhookURL, event string, payload map[string]any) {
	body := map[string]any{"event": event, "timestamp": time.Now().UTC().Format(time.RFC3339)}
	for k, v := range payload {
		body[k] = v
	}
	raw, err := json.Marshal(body)
	if err != nil {
		log.Printf("notify: encoding payload: %v", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(raw))
	if err != nil {
		log.Printf("notify: building request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		log.Printf("notify: posting %q to webhook: %v", event, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("notify: webhook returned status %d for event %q", resp.StatusCode, event)
	}
}
