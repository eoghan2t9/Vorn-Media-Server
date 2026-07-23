package store

import "time"

// serverSettingsKey is this feature's key in the generic server_settings
// key/value table (from migration 000002_core_schema; see GetSetting/
// SetSetting in settings.go) -- no dedicated table needed.
const serverSettingsKey = "network"

// ServerSettings holds admin-configurable options that don't fit anywhere
// else: custom domain/SSL and Cloudflare-aware real-IP trust.
type ServerSettings struct {
	CustomDomain    string
	ACMEEmail       string
	SSLEnabled      bool
	TrustCloudflare bool
	UpdatedAt       time.Time
}

type serverSettingsValue struct {
	CustomDomain    string `json:"customDomain"`
	ACMEEmail       string `json:"acmeEmail"`
	SSLEnabled      bool   `json:"sslEnabled"`
	TrustCloudflare bool   `json:"trustCloudflare"`
}

// GetServerSettings returns the current settings, or their zero value
// (SSL/trust both off) if they've never been configured.
func (s *Store) GetServerSettings() (*ServerSettings, error) {
	var v serverSettingsValue
	found, err := s.GetSetting(serverSettingsKey, &v)
	if err != nil {
		return nil, err
	}
	if !found {
		return &ServerSettings{}, nil
	}

	ss := &ServerSettings{
		CustomDomain:    v.CustomDomain,
		ACMEEmail:       v.ACMEEmail,
		SSLEnabled:      v.SSLEnabled,
		TrustCloudflare: v.TrustCloudflare,
	}
	// SetSetting's ON CONFLICT upsert always stamps updated_at, so this
	// extra lookup is just to surface it -- GetSetting itself doesn't.
	_ = s.db.QueryRow(`SELECT updated_at FROM server_settings WHERE key = $1`, serverSettingsKey).Scan(&ss.UpdatedAt)
	return ss, nil
}

type UpdateServerSettingsInput struct {
	CustomDomain    string
	ACMEEmail       string
	SSLEnabled      bool
	TrustCloudflare bool
}

func (s *Store) UpdateServerSettings(in UpdateServerSettingsInput) (*ServerSettings, error) {
	v := serverSettingsValue{
		CustomDomain:    in.CustomDomain,
		ACMEEmail:       in.ACMEEmail,
		SSLEnabled:      in.SSLEnabled,
		TrustCloudflare: in.TrustCloudflare,
	}
	if err := s.SetSetting(serverSettingsKey, v); err != nil {
		return nil, err
	}
	return s.GetServerSettings()
}
