package config

// DefaultThemeSettings returns sane UI defaults.
func DefaultThemeSettings() ThemeSettings {
	return ThemeSettings{
		Theme:            "DARK",
		PrimaryColor:     "#2196f3",
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
		ThemeSettings: DefaultThemeSettings(),
		Docker:        DefaultDocker(base),
	}
}
