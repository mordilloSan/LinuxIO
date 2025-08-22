package config

// DefaultAppSettings returns sane UI defaults.
func DefaultAppSettings() AppSettings {
	return AppSettings{
		Theme:            "DARK",
		PrimaryColor:     "blue",
		SidebarCollapsed: false,
	}
}

// DefaultDocker returns Docker defaults based on the chosen base directory.
func DefaultDocker(base string) Docker {
	return Docker{
		Folder: filepathJoinClean(base, "docker"),
	}
}

// DefaultSettings composes full defaults for later expansion.
func DefaultSettings(base string) *Settings {
	return &Settings{
		AppSettings: DefaultAppSettings(),
		Docker:      DefaultDocker(base),
	}
}
