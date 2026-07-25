package store

import "time"

// integrationSettingsKey is this feature's key in the generic server_settings
// key/value table (see GetSetting/SetSetting in settings.go) -- no dedicated
// table needed.
const integrationSettingsKey = "integrations"

// IntegrationSettings holds admin-configurable credentials for external
// metadata/subtitle providers, as a DB-backed alternative to the
// VORN_TMDB_API_KEY / VORN_OPENSUBTITLES_* env vars, plus the torrent/NZB
// acquisition-source toggles. Every field here takes effect immediately --
// httpapi.Server.reconfigure re-reads this on every save and hot-swaps
// whichever credentialed clients/services changed, no restart needed.
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
	// FanartAPIKey and OMDbAPIKey are both optional enrichment layered onto
	// an existing TMDb/TheTVDB match (higher-res art, ratings) -- empty
	// means that enrichment step is simply skipped, same as TMDb being
	// empty skips matching entirely.
	FanartAPIKey string
	OMDbAPIKey   string
	// TVDbAPIKey/TVDbPin are an optional fallback series matcher, tried
	// only when TMDb has no match for a series. TVDbPin is only needed for
	// a "user-support" API key tied to an individual paid subscriber
	// account -- a standard project key leaves it empty.
	TVDbAPIKey string
	TVDbPin    string
	// TorrentEnabled/NZBEnabled are tri-state, unlike MusicMetadataEnabled
	// above: nil means "no admin has touched this toggle yet, fall back to
	// VORN_TORRENT_ENABLED/VORN_NZB_ENABLED" (so an existing env-var-only
	// deployment doesn't silently lose acquisition on upgrade), while a
	// non-nil value is authoritative from then on. See
	// httpapi.Server.reconfigure for where the fallback is resolved.
	TorrentEnabled *bool
	NZBEnabled     *bool
	UpdatedAt      time.Time
}

type integrationSettingsValue struct {
	TMDbAPIKey               string `json:"tmdbApiKey"`
	OpenSubtitlesAPIKey      string `json:"openSubtitlesApiKey"`
	OpenSubtitlesUsername    string `json:"openSubtitlesUsername"`
	OpenSubtitlesPassword    string `json:"openSubtitlesPassword"`
	MusicMetadataEnabled     bool   `json:"musicMetadataEnabled"`
	AudiobookMetadataEnabled bool   `json:"audiobookMetadataEnabled"`
	FanartAPIKey             string `json:"fanartApiKey"`
	OMDbAPIKey               string `json:"omdbApiKey"`
	TVDbAPIKey               string `json:"tvdbApiKey"`
	TVDbPin                  string `json:"tvdbPin"`
	TorrentEnabled           *bool  `json:"torrentEnabled,omitempty"`
	NZBEnabled               *bool  `json:"nzbEnabled,omitempty"`
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
		FanartAPIKey:             v.FanartAPIKey,
		OMDbAPIKey:               v.OMDbAPIKey,
		TVDbAPIKey:               v.TVDbAPIKey,
		TVDbPin:                  v.TVDbPin,
		TorrentEnabled:           v.TorrentEnabled,
		NZBEnabled:               v.NZBEnabled,
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
	FanartAPIKey             *string
	OMDbAPIKey               *string
	TVDbAPIKey               *string
	TVDbPin                  *string
	// TorrentEnabled/NZBEnabled are themselves already tri-state
	// (*bool) on IntegrationSettings, so unlike the other fields here
	// there's no separate "leave unchanged" pointer-of-pointer -- passing
	// non-nil always overwrites the stored tri-state value (including
	// explicitly setting it back to nil isn't supported via this API;
	// once toggled, a value is always sent back on every future save from
	// the admin UI, which always knows the current effective state).
	TorrentEnabled *bool
	NZBEnabled     *bool
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
		FanartAPIKey:             current.FanartAPIKey,
		OMDbAPIKey:               current.OMDbAPIKey,
		TVDbAPIKey:               current.TVDbAPIKey,
		TVDbPin:                  current.TVDbPin,
		TorrentEnabled:           current.TorrentEnabled,
		NZBEnabled:               current.NZBEnabled,
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
	if in.FanartAPIKey != nil {
		v.FanartAPIKey = *in.FanartAPIKey
	}
	if in.OMDbAPIKey != nil {
		v.OMDbAPIKey = *in.OMDbAPIKey
	}
	if in.TVDbAPIKey != nil {
		v.TVDbAPIKey = *in.TVDbAPIKey
	}
	if in.TVDbPin != nil {
		v.TVDbPin = *in.TVDbPin
	}
	if in.TorrentEnabled != nil {
		v.TorrentEnabled = in.TorrentEnabled
	}
	if in.NZBEnabled != nil {
		v.NZBEnabled = in.NZBEnabled
	}

	if err := s.SetSetting(integrationSettingsKey, v); err != nil {
		return nil, err
	}
	return s.GetIntegrationSettings()
}
