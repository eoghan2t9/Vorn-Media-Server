package store

// notificationSettingsKey is this feature's key in the generic
// server_settings key/value table (see GetSetting/SetSetting in
// settings.go) -- a singleton config blob, same as BackupSettings.
const notificationSettingsKey = "notifications"

// NotificationSettings controls the webhook Vorn posts acquisition events
// to (see backend/internal/notify). Disabled with no URL set is the zero
// value, so a fresh install doesn't try posting anywhere until an admin
// explicitly configures one. Individual event types can be toggled on/off
// independently; when a type's toggle is false, that event is silently
// skipped even when the webhook is otherwise enabled.
type NotificationSettings struct {
	Enabled           bool
	WebhookURL        string
	NotifyOnAcquired  bool
	NotifyOnFailed    bool
	NotifyOnUpgraded  bool
}

type notificationSettingsValue struct {
	Enabled          bool   `json:"enabled"`
	WebhookURL       string `json:"webhookUrl"`
	NotifyOnAcquired bool   `json:"notifyOnAcquired"`
	NotifyOnFailed   bool   `json:"notifyOnFailed"`
	NotifyOnUpgraded bool   `json:"notifyOnUpgraded"`
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
		// Fresh install: nothing configured, all toggles default on so
		// enabling the webhook and setting a URL is all an admin needs to
		// do to get every event type flowing.
		return &NotificationSettings{NotifyOnAcquired: true, NotifyOnFailed: true, NotifyOnUpgraded: true}, nil
	}
	// Existing installs upgrading from a version before per-event toggles
	// existed won't have these fields in their stored JSON blob -- default
	// them to true so notifications don't silently stop.
	if v.NotifyOnAcquired == (notificationSettingsValue{}).NotifyOnAcquired &&
		v.NotifyOnFailed == (notificationSettingsValue{}).NotifyOnFailed &&
		v.NotifyOnUpgraded == (notificationSettingsValue{}).NotifyOnUpgraded {
		// All three are the zero value, which means either the admin
		// explicitly unchecked all three (unlikely), or this settings blob
		// predates the per-event toggles -- treat as all-on.
		v.NotifyOnAcquired = true
		v.NotifyOnFailed = true
		v.NotifyOnUpgraded = true
	}
	return &NotificationSettings{Enabled: v.Enabled, WebhookURL: v.WebhookURL, NotifyOnAcquired: v.NotifyOnAcquired, NotifyOnFailed: v.NotifyOnFailed, NotifyOnUpgraded: v.NotifyOnUpgraded}, nil
}

func (s *Store) SetNotificationSettings(in NotificationSettings) error {
	return s.SetSetting(notificationSettingsKey, notificationSettingsValue{Enabled: in.Enabled, WebhookURL: in.WebhookURL, NotifyOnAcquired: in.NotifyOnAcquired, NotifyOnFailed: in.NotifyOnFailed, NotifyOnUpgraded: in.NotifyOnUpgraded})
}
