package store

const requestSettingsKey = "requests"

// RequestSettings holds admin-configurable options for the content-request
// workflow.
type RequestSettings struct {
	AutoApprove bool
}

type requestSettingsValue struct {
	AutoApprove bool `json:"autoApprove"`
}

// GetRequestSettings returns the current settings, or their zero value
// (auto-approve off, i.e. requests need manual admin review) if they've
// never been configured.
func (s *Store) GetRequestSettings() (*RequestSettings, error) {
	var v requestSettingsValue
	found, err := s.GetSetting(requestSettingsKey, &v)
	if err != nil {
		return nil, err
	}
	if !found {
		return &RequestSettings{}, nil
	}
	return &RequestSettings{AutoApprove: v.AutoApprove}, nil
}

func (s *Store) SetRequestAutoApprove(autoApprove bool) error {
	return s.SetSetting(requestSettingsKey, requestSettingsValue{AutoApprove: autoApprove})
}
