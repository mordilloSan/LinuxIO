package apischema

type TunedProfile struct {
	Active      bool    `json:"active"`
	Description *string `json:"description,omitzero"`
	Name        string  `json:"name"`
	Recommended bool    `json:"recommended"`
}

type PowerStatus struct {
	ActiveProfile             string         `json:"active_profile"`
	Backend                   string         `json:"backend"`
	Error                     *string        `json:"error,omitzero"`
	InstallCommand            string         `json:"install_command"`
	Notes                     []string       `json:"notes,omitempty"`
	PackageName               string         `json:"package_name"`
	PowerProfilesDaemonActive bool           `json:"power_profiles_daemon_active"`
	Profiles                  []TunedProfile `json:"profiles"`
	RecommendedProfile        string         `json:"recommended_profile"`
	TunedActivatable          bool           `json:"tuned_activatable"`
	TunedActive               bool           `json:"tuned_active"`
	TunedAvailable            bool           `json:"tuned_available"`
	TunedStartable            bool           `json:"tuned_startable"`
	TunedUnitAvailable        bool           `json:"tuned_unit_available"`
	TunedUnitFileState        string         `json:"tuned_unit_file_state"`
}

type ProfileRequest struct {
	Profile string `json:"profile"`
}
