package store

// notificationSettingsKey is this feature's key in the generic
// server_settings key/value table (see GetSetting/SetSetting in
// settings.go) -- a singleton config blob, same as BackupSettings.
const notificationSettingsKey = "notifications"

// NotificationSettings controls the webhook Vorn posts acquisition events
// to (see backend/internal/notify). Disabled with no URL set is the zero
// value, so a fresh install doesn't try posting anywhere until an admin
// explicitly configures one.
type NotificationSettings struct {
	Enabled    bool
	WebhookURL string
}

type notificationSettingsValue struct {
	Enabled    bool   `json:"enabled"`
	WebhookURL string `json:"webhookUrl"`
}

// GetNotificationSettings returns the current settings, defaulting to
// disabled with no URL if never configured.
func (s *Store) GetNotificationSettings() (*NotificationSettings, error) {
	var v notificationSettingsValue
	found, err := s.GetSetting(notificationSettingsKey, &v)
	if err != nil {
		return nil, err
	}
	if !found {
		return &NotificationSettings{}, nil
	}
	return &NotificationSettings{Enabled: v.Enabled, WebhookURL: v.WebhookURL}, nil
}

func (s *Store) SetNotificationSettings(in NotificationSettings) error {
	return s.SetSetting(notificationSettingsKey, notificationSettingsValue{Enabled: in.Enabled, WebhookURL: in.WebhookURL})
}
