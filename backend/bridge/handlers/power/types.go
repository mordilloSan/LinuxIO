package power

type TunedProfile struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Active      bool   `json:"active"`
	Recommended bool   `json:"recommended"`
}

type PowerStatus struct {
	Backend                   string         `json:"backend"`
	TunedAvailable            bool           `json:"tuned_available"`
	TunedActive               bool           `json:"tuned_active"`
	TunedActivatable          bool           `json:"tuned_activatable"`
	TunedStartable            bool           `json:"tuned_startable"`
	TunedUnitAvailable        bool           `json:"tuned_unit_available"`
	TunedUnitFileState        string         `json:"tuned_unit_file_state"`
	PowerProfilesDaemonActive bool           `json:"power_profiles_daemon_active"`
	PackageName               string         `json:"package_name"`
	InstallCommand            string         `json:"install_command"`
	ActiveProfile             string         `json:"active_profile"`
	RecommendedProfile        string         `json:"recommended_profile"`
	Profiles                  []TunedProfile `json:"profiles"`
	Notes                     []string       `json:"notes,omitempty"`
	Error                     string         `json:"error,omitempty"`
}
