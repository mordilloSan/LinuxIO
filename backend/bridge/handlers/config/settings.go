package config

// DefaultAppSettings returns sane UI defaults.
func DefaultAppSettings() AppSettings {
	return AppSettings{
		Theme:            ThemeDark,
		PrimaryColor:     "#2196f3",
		SidebarCollapsed: false,
		ShowHiddenFiles:  true,
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

func cssColor(s string) *CSSColor { c := CSSColor(s); return &c }

// ExampleDefaults returns a stable, user-agnostic config for docs/examples.
// Keep this in sync with your real defaults (used at runtime).
func ExampleDefaults() Settings {
	return Settings{
		AppSettings: AppSettings{
			Theme:            ThemeDark,
			PrimaryColor:     "#2196f3",
			SidebarCollapsed: false,
			ShowHiddenFiles:  true,
			DashboardOrder:   []string{"overview", "system", "cpu", "memory", "docker", "nic", "fs", "mb", "gpu", "drive"},
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
			ViewModes: map[string]string{
				"accounts.users":    "card",
				"accounts.groups":   "card",
				"docker.containers": "card",
				"docker.stacks":     "card",
				"docker.networks":   "card",
				"docker.volumes":    "card",
				"docker.images":     "card",
				"services.list":     "card",
				"timers.list":       "card",
				"sockets.list":      "card",
				"shares":            "card",
				"shares.mounts":     "card",
			},
			ChunkSizeMB: 1,
			ThemeColors: &ThemeColorsByMode{
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
			},
		},
		Docker: Docker{
			// Use a neutral path that makes sense in docs. Avoid per-user paths.
			Folders: []AbsolutePath{"/var/lib/linuxio/docker"},
		},
		Jobs: DefaultJobSettings(),
	}
}
