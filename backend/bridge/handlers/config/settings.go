package config

// DefaultAppSettings returns sane UI defaults.
func DefaultAppSettings() AppSettings {
	return AppSettings{
		Theme:            ThemeDark,
		PrimaryColor:     "#2196f3",
		SidebarCollapsed: false,
		ShowHiddenFiles:  false,
	}
}

// DefaultDocker returns Docker defaults based on the chosen base directory.
func DefaultDocker(base string) Docker {
	return Docker{
		Folder: AbsolutePath(filepathJoinClean(base, "docker")),
	}
}

func DefaultJobSettings() JobSettings {
	return JobSettings{
		ProgressMinIntervalMs:     250,
		NotificationMinIntervalMs: 1000,
		ProgressMinBytesMB:        16,
		HeavyArchiveConcurrency:   1,
		ArchiveCompressionWorkers: 0,
		ArchiveExtractWorkers:     0,
	}
}

func EffectiveJobSettings(settings JobSettings) JobSettings {
	defaults := DefaultJobSettings()
	if settings.ProgressMinIntervalMs <= 0 {
		settings.ProgressMinIntervalMs = defaults.ProgressMinIntervalMs
	}
	if settings.NotificationMinIntervalMs <= 0 {
		settings.NotificationMinIntervalMs = defaults.NotificationMinIntervalMs
	}
	if settings.ProgressMinBytesMB <= 0 {
		settings.ProgressMinBytesMB = defaults.ProgressMinBytesMB
	}
	if settings.HeavyArchiveConcurrency <= 0 {
		settings.HeavyArchiveConcurrency = defaults.HeavyArchiveConcurrency
	}
	if settings.ArchiveCompressionWorkers < 0 {
		settings.ArchiveCompressionWorkers = defaults.ArchiveCompressionWorkers
	}
	if settings.ArchiveExtractWorkers < 0 {
		settings.ArchiveExtractWorkers = defaults.ArchiveExtractWorkers
	}
	return settings
}

// DefaultSettings composes full defaults for later expansion.
func DefaultSettings(base string) *Settings {
	return &Settings{
		AppSettings: DefaultAppSettings(),
		Docker:      DefaultDocker(base),
		Jobs:        DefaultJobSettings(),
	}
}

// ExampleDefaults returns a stable, user-agnostic config for docs/examples.
// Keep this in sync with your real defaults (used at runtime).
func ExampleDefaults() Settings {
	return Settings{
		AppSettings: AppSettings{
			Theme:            ThemeDark,
			PrimaryColor:     "#2196f3",
			SidebarCollapsed: false,
			ShowHiddenFiles:  false,
		},
		Docker: Docker{
			// Use a neutral path that makes sense in docs. Avoid per-user paths.
			Folder: "/var/lib/linuxio/docker",
		},
		Jobs: DefaultJobSettings(),
	}
}
