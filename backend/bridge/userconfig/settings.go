package userconfig

// DefaultAppSettings returns sane UI defaults.
func DefaultAppSettings() AppSettings {
	return AppSettings{
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
		AppSettings: DefaultAppSettings(),
		Docker:      DefaultDocker(base),
	}
}

// ExampleDefaults returns a stable, user-agnostic config for docs/examples.
// Keep this in sync with your real defaults (used at runtime).
func ExampleDefaults() Settings {
	return Settings{
		AppSettings: AppSettings{
			Theme:            "DARK",
			PrimaryColor:     "#2196f3",
			SidebarCollapsed: false,
		},
		Docker: Docker{
			// Use a neutral path that makes sense in docs. Avoid per-user paths.
			Folder: "/var/lib/linuxio/docker",
		},
	}
}
