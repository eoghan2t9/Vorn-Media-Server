package store

import "time"

// integrationSettingsKey is this feature's key in the generic server_settings
// key/value table (see GetSetting/SetSetting in settings.go) -- no dedicated
// table needed.
const integrationSettingsKey = "integrations"

// IntegrationSettings holds admin-configurable credentials for external
// metadata/subtitle providers, as a DB-backed alternative to the
// VORN_TMDB_API_KEY / VORN_OPENSUBTITLES_* env vars. Like ServerSettings
// (custom domain/SSL), changes here only take effect after a restart -- the
// metadata and subtitles services are constructed once at startup in
// cmd/vornd.
type IntegrationSettings struct {
	TMDbAPIKey            string
	OpenSubtitlesAPIKey   string
	OpenSubtitlesUsername string
	OpenSubtitlesPassword string
	// MusicMetadataEnabled/AudiobookMetadataEnabled gate outbound calls to
	// MusicBrainz+Cover Art Archive / Open Library. Unlike TMDb/OpenSubtitles
	// these providers need no credentials at all, so a boolean opt-in (default
	// off) is the only thing standing between a fresh install and Vorn
	// silently calling out to third-party APIs -- admins should choose that,
	// not have it happen by default.
	MusicMetadataEnabled     bool
	AudiobookMetadataEnabled bool
	UpdatedAt                time.Time
}

type integrationSettingsValue struct {
	TMDbAPIKey               string `json:"tmdbApiKey"`
	OpenSubtitlesAPIKey      string `json:"openSubtitlesApiKey"`
	OpenSubtitlesUsername    string `json:"openSubtitlesUsername"`
	OpenSubtitlesPassword    string `json:"openSubtitlesPassword"`
	MusicMetadataEnabled     bool   `json:"musicMetadataEnabled"`
	AudiobookMetadataEnabled bool   `json:"audiobookMetadataEnabled"`
}

// GetIntegrationSettings returns the current settings, or their zero value
// (nothing configured) if they've never been set.
func (s *Store) GetIntegrationSettings() (*IntegrationSettings, error) {
	var v integrationSettingsValue
	found, err := s.GetSetting(integrationSettingsKey, &v)
	if err != nil {
		return nil, err
	}
	if !found {
		return &IntegrationSettings{}, nil
	}

	is := &IntegrationSettings{
		TMDbAPIKey:               v.TMDbAPIKey,
		OpenSubtitlesAPIKey:      v.OpenSubtitlesAPIKey,
		OpenSubtitlesUsername:    v.OpenSubtitlesUsername,
		OpenSubtitlesPassword:    v.OpenSubtitlesPassword,
		MusicMetadataEnabled:     v.MusicMetadataEnabled,
		AudiobookMetadataEnabled: v.AudiobookMetadataEnabled,
	}
	// SetSetting's ON CONFLICT upsert always stamps updated_at, so this
	// extra lookup is just to surface it -- GetSetting itself doesn't.
	_ = s.db.QueryRow(`SELECT updated_at FROM server_settings WHERE key = $1`, integrationSettingsKey).Scan(&is.UpdatedAt)
	return is, nil
}

// UpdateIntegrationSettingsInput fields are pointers so nil means "leave
// this credential unchanged" -- an admin rotating one key shouldn't have to
// resend every other secret, and the API never echoes secrets back for them
// to resend in the first place. A non-nil empty string explicitly clears
// the field.
type UpdateIntegrationSettingsInput struct {
	TMDbAPIKey               *string
	OpenSubtitlesAPIKey      *string
	OpenSubtitlesUsername    *string
	OpenSubtitlesPassword    *string
	MusicMetadataEnabled     *bool
	AudiobookMetadataEnabled *bool
}

func (s *Store) UpdateIntegrationSettings(in UpdateIntegrationSettingsInput) (*IntegrationSettings, error) {
	current, err := s.GetIntegrationSettings()
	if err != nil {
		return nil, err
	}

	v := integrationSettingsValue{
		TMDbAPIKey:               current.TMDbAPIKey,
		OpenSubtitlesAPIKey:      current.OpenSubtitlesAPIKey,
		OpenSubtitlesUsername:    current.OpenSubtitlesUsername,
		OpenSubtitlesPassword:    current.OpenSubtitlesPassword,
		MusicMetadataEnabled:     current.MusicMetadataEnabled,
		AudiobookMetadataEnabled: current.AudiobookMetadataEnabled,
	}
	if in.TMDbAPIKey != nil {
		v.TMDbAPIKey = *in.TMDbAPIKey
	}
	if in.OpenSubtitlesAPIKey != nil {
		v.OpenSubtitlesAPIKey = *in.OpenSubtitlesAPIKey
	}
	if in.OpenSubtitlesUsername != nil {
		v.OpenSubtitlesUsername = *in.OpenSubtitlesUsername
	}
	if in.OpenSubtitlesPassword != nil {
		v.OpenSubtitlesPassword = *in.OpenSubtitlesPassword
	}
	if in.MusicMetadataEnabled != nil {
		v.MusicMetadataEnabled = *in.MusicMetadataEnabled
	}
	if in.AudiobookMetadataEnabled != nil {
		v.AudiobookMetadataEnabled = *in.AudiobookMetadataEnabled
	}

	if err := s.SetSetting(integrationSettingsKey, v); err != nil {
		return nil, err
	}
	return s.GetIntegrationSettings()
}
