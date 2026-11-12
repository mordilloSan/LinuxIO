package userconfig

import (
	"maps"
	"path/filepath"
)

// defaultAppSettings returns defaults for the important configuration file.
func defaultAppSettings() PersistedAppSettings {
	return PersistedAppSettings{ShowHiddenFiles: true, ChunkSizeMB: 1}
}

const DefaultViewMode = ViewModeCard

// DefaultUIPreferences returns the effective backend-owned UI defaults. The
// empty UI file is represented separately by writeEmptyUIConfigOwned; it must
// not be confused with this populated runtime value.
func DefaultUIPreferences() UIPreferences {
	return UIPreferences{
		Theme:              ThemeDark,
		PrimaryColor:       CSSColor("#2196f3"),
		ThemeColors:        defaultThemeColors(),
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

// defaultDocker returns Docker defaults based on the chosen base directory.
func defaultDocker(base string) Docker {
	return Docker{
		Folders: []AbsolutePath{AbsolutePath(filepath.Join(base, "docker"))},
	}
}

func DefaultJobSettings() PersistedJobSettings {
	return PersistedJobSettings{
		ProgressMinIntervalMs:     250,
		NotificationMinIntervalMs: 1000,
		ProgressMinBytesMB:        16,
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
	if jobSettings.ArchiveCompressionWorkers < 0 {
		jobSettings.ArchiveCompressionWorkers = defaults.ArchiveCompressionWorkers
	}
	if jobSettings.ArchiveExtractWorkers < 0 {
		jobSettings.ArchiveExtractWorkers = defaults.ArchiveExtractWorkers
	}
	return jobSettings
}

// EffectiveAssistantDefaults adds LinuxIO's starting point for an agent the
// user has not configured: Claude starts on Opus at high effort in Auto mode.
// Other agents keep their own defaults until the user picks some.
func EffectiveAssistantDefaults(stored map[string]AssistantDefaults) map[string]AssistantDefaults {
	out := maps.Clone(stored)
	if out == nil {
		out = map[string]AssistantDefaults{}
	}
	if _, ok := out["claude"]; !ok {
		out["claude"] = AssistantDefaults{Model: "opus", Effort: "high", Mode: "auto"}
	}
	return out
}

// defaultSettings returns defaults for the important configuration file.
func defaultSettings(base string) *Settings {
	return &Settings{
		AppSettings: defaultAppSettings(),
		Docker:      defaultDocker(base),
		Jobs:        DefaultJobSettings(),
	}
}
