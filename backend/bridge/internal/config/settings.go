package config

import "path/filepath"

// DefaultAppSettings returns defaults for the important configuration file.
func DefaultAppSettings() PersistedAppSettings {
	return PersistedAppSettings{ShowHiddenFiles: true, ChunkSizeMB: 1}
}

const DefaultViewMode = "card"

// DefaultUIPreferences returns the effective backend-owned UI defaults. The
// empty UI file is represented separately by writeEmptyUIConfigOwned; it must
// not be confused with this populated runtime value.
func DefaultUIPreferences() UIPreferences {
	return UIPreferences{
		Theme:              ThemeDark,
		PrimaryColor:       CSSColor("#2196f3"),
		NavigationMode:     NavigationModeSidebar,
		DockTileColors:     DockTileColorsAccent,
		DockAccentGradient: &DockAccentGradient{RangeStart: 0, RangeEnd: 100},
		HiddenCards:        []string{},
		DockerDashboardSections: &DockerDashboardSections{
			Overview: true, Monitoring: true, Daemon: true, Resources: true,
		},
		HardwareSections: &HardwareSections{
			Overview: true, Hardware: true, Sensors: true, SystemInfo: true,
			GPU: true, PCIDevices: true, MemoryModules: true,
		},
		ViewModes:        map[string]string{},
		LayoutOrders:     map[string][]string{},
		TerminalFontSize: 16,
	}
}

// DefaultDocker returns Docker defaults based on the chosen base directory.
func DefaultDocker(base string) Docker {
	return Docker{
		Folders: []AbsolutePath{AbsolutePath(filepath.Join(base, "docker"))},
	}
}

func DefaultJobSettings() PersistedJobSettings {
	return PersistedJobSettings{
		ProgressMinIntervalMs:     250,
		NotificationMinIntervalMs: 1000,
		ProgressMinBytesMB:        16,
		HeavyArchiveConcurrency:   1,
		ArchiveCompressionWorkers: 0,
		ArchiveExtractWorkers:     0,
	}
}

func EffectiveJobSettings(jobSettings PersistedJobSettings) PersistedJobSettings {
	defaults := DefaultJobSettings()
	if jobSettings.ProgressMinIntervalMs <= 0 {
		jobSettings.ProgressMinIntervalMs = defaults.ProgressMinIntervalMs
	}
	if jobSettings.NotificationMinIntervalMs <= 0 {
		jobSettings.NotificationMinIntervalMs = defaults.NotificationMinIntervalMs
	}
	if jobSettings.ProgressMinBytesMB <= 0 {
		jobSettings.ProgressMinBytesMB = defaults.ProgressMinBytesMB
	}
	if jobSettings.HeavyArchiveConcurrency <= 0 {
		jobSettings.HeavyArchiveConcurrency = defaults.HeavyArchiveConcurrency
	}
	if jobSettings.ArchiveCompressionWorkers < 0 {
		jobSettings.ArchiveCompressionWorkers = defaults.ArchiveCompressionWorkers
	}
	if jobSettings.ArchiveExtractWorkers < 0 {
		jobSettings.ArchiveExtractWorkers = defaults.ArchiveExtractWorkers
	}
	return jobSettings
}

// DefaultSettings returns defaults for the important configuration file.
func DefaultSettings(base string) *Settings {
	return &Settings{
		AppSettings: DefaultAppSettings(),
		Docker:      DefaultDocker(base),
		Jobs:        DefaultJobSettings(),
	}
}
