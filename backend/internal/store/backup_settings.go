package store

// backupSettingsKey is this feature's key in the generic server_settings
// key/value table (see GetSetting/SetSetting in settings.go) -- a
// singleton config blob, same as ServerSettings/IntegrationSettings.
const backupSettingsKey = "backup"

// BackupSettings controls the automated-backup scheduler (see
// backend/internal/backup). Disabled with no interval set is the zero
// value, so a fresh install doesn't silently start writing to disk until
// an admin explicitly opts in.
type BackupSettings struct {
	Enabled       bool
	IntervalHours int
}

type backupSettingsValue struct {
	Enabled       bool `json:"enabled"`
	IntervalHours int  `json:"intervalHours"`
}

// GetBackupSettings returns the current settings, defaulting to disabled
// with a 24h interval (a sensible default an admin only needs to flip a
// switch on, if they've never touched this before).
func (s *Store) GetBackupSettings() (*BackupSettings, error) {
	var v backupSettingsValue
	found, err := s.GetSetting(backupSettingsKey, &v)
	if err != nil {
		return nil, err
	}
	if !found {
		return &BackupSettings{Enabled: false, IntervalHours: 24}, nil
	}
	return &BackupSettings{Enabled: v.Enabled, IntervalHours: v.IntervalHours}, nil
}

func (s *Store) SetBackupSettings(in BackupSettings) error {
	return s.SetSetting(backupSettingsKey, backupSettingsValue{Enabled: in.Enabled, IntervalHours: in.IntervalHours})
}
