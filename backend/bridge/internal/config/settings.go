package config

// DefaultAppSettings returns sane UI defaults.
func DefaultAppSettings() AppSettings {
	return AppSettings{
		Theme:            ThemeDark,
		PrimaryColor:     "#2196f3",
		ThemeColors:      defaultThemeColors(),
		SidebarCollapsed: false,
		ShowHiddenFiles:  true,
		DashboardOrder:   defaultDashboardOrder(),
		HiddenCards:      []string{},
		ContainerOrder:   []string{},
		DockerDashboardSections: &DockerDashboardSections{
			Overview:  true,
			Daemon:    true,
			Resources: true,
		},
		HardwareSections: &HardwareSections{
			Overview:      true,
			Hardware:      true,
			Sensors:       true,
			SystemInfo:    true,
			GPU:           true,
			PCIDevices:    true,
			MemoryModules: true,
		},
		ViewModes:   defaultViewModes(),
		ChunkSizeMB: 1,
	}
}

func defaultDashboardOrder() []string {
	return []string{"overview", "system", "cpu", "memory", "docker", "nic", "fs", "mb", "gpu", "drive"}
}

func defaultViewModes() map[string]string {
	return map[string]string{
		"accounts.groups":   "card",
		"accounts.users":    "card",
		"docker.containers": "card",
		"docker.images":     "card",
		"docker.networks":   "card",
		"docker.stacks":     "card",
		"docker.volumes":    "card",
		"services.list":     "card",
		"shares":            "card",
		"shares.mounts":     "card",
		"sockets.list":      "card",
		"timers.list":       "card",
	}
}

func defaultThemeColors() *ThemeColorsByMode {
	return &ThemeColorsByMode{
		Light: &ThemeColors{
			BackgroundDefault:               cssColor("#F7F9FC"),
			BackgroundPaper:                 cssColor("#FFFFFF"),
			HeaderBackground:                cssColor("#F7F9FC"),
			FooterBackground:                cssColor("#F7F9FC"),
			SidebarBackground:               cssColor("#F7F9FC"),
			CardBackground:                  cssColor("#FFFFFF"),
			DialogBorder:                    cssColor("#FFFFFF"),
			DialogGlow:                      cssColor("#FFFFFF"),
			DialogBackdrop:                  cssColor("#000000"),
			CodeBackground:                  cssColor("#F5F5F5"),
			CodeText:                        cssColor("#333333"),
			ChartRx:                         cssColor("#8884D8"),
			ChartTx:                         cssColor("#82CA9D"),
			ChartNeutral:                    cssColor("#808080"),
			FileBrowserSurface:              cssColor("#FFFFFF"),
			FileBrowserChrome:               cssColor("#253137"),
			FileBrowserBreadcrumbBackground: cssColor("#D0D4D8"),
			FileBrowserBreadcrumbText:       cssColor("#5A5A5A"),
		},
		Dark: &ThemeColors{
			BackgroundDefault:               cssColor("#1B2635"),
			BackgroundPaper:                 cssColor("#233044"),
			HeaderBackground:                cssColor("#1B2635"),
			FooterBackground:                cssColor("#1B2635"),
			SidebarBackground:               cssColor("#1B2635"),
			CardBackground:                  cssColor("#11192A"),
			DialogBorder:                    cssColor("#FFFFFF"),
			DialogGlow:                      cssColor("#FFFFFF"),
			DialogBackdrop:                  cssColor("#000000"),
			CodeBackground:                  cssColor("#1E1E1E"),
			CodeText:                        cssColor("#D4D4D4"),
			ChartRx:                         cssColor("#8884D8"),
			ChartTx:                         cssColor("#82CA9D"),
			ChartNeutral:                    cssColor("#808080"),
			FileBrowserSurface:              cssColor("#20292F"),
			FileBrowserChrome:               cssColor("#253137"),
			FileBrowserBreadcrumbBackground: cssColor("#283136"),
			FileBrowserBreadcrumbText:       cssColor("#FFFFFF"),
		},
	}
}

// DefaultDocker returns Docker defaults based on the chosen base directory.
func DefaultDocker(base string) Docker {
	return Docker{
		Folders: []AbsolutePath{AbsolutePath(filepathJoinClean(base, "docker"))},
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

func EffectiveJobSettings(jobSettings JobSettings) JobSettings {
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

// DefaultSettings composes full defaults for later expansion.
func DefaultSettings(base string) *Settings {
	return &Settings{
		AppSettings: DefaultAppSettings(),
		Docker:      DefaultDocker(base),
		Jobs:        DefaultJobSettings(),
	}
}

func cssColor(s string) *CSSColor { c := CSSColor(s); return &c }

// ExampleDefaults returns stable, user-agnostic config for docs/examples.
func ExampleDefaults() Settings {
	return *DefaultSettings("/var/lib/linuxio")
}
