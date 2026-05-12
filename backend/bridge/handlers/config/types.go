package config

import "github.com/mordilloSan/LinuxIO/backend/bridge/settings"

type configSetPayload struct {
	AppSettings *configAppSettingsPayload `json:"appSettings"`
	Docker      *configDockerPayload      `json:"docker"`
	Jobs        *configJobSettingsPayload `json:"jobs"`
	Dismissals  *configDismissalsPayload  `json:"dismissals"`
}

type configAppSettingsPayload struct {
	Theme                   *string                           `json:"theme"`
	PrimaryColor            *string                           `json:"primaryColor"`
	ThemeColors             *configThemeColorsByModePayload   `json:"themeColors"`
	SidebarCollapsed        *bool                             `json:"sidebarCollapsed"`
	ShowHiddenFiles         *bool                             `json:"showHiddenFiles"`
	DashboardOrder          []string                          `json:"dashboardOrder"`
	HiddenCards             []string                          `json:"hiddenCards"`
	ContainerOrder          []string                          `json:"containerOrder"`
	DockerDashboardSections *settings.DockerDashboardSections `json:"dockerDashboardSections"`
	HardwareSections        *settings.HardwareSections        `json:"hardwareSections"`
	ViewModes               map[string]string                 `json:"viewModes"`
	ChunkSizeMB             *int                              `json:"chunkSizeMB"`
}

type configThemeColorsByModePayload struct {
	Light *configThemeColorsPayload `json:"light"`
	Dark  *configThemeColorsPayload `json:"dark"`
}

type configThemeColorsPayload struct {
	BackgroundDefault               *string `json:"backgroundDefault"`
	BackgroundPaper                 *string `json:"backgroundPaper"`
	HeaderBackground                *string `json:"headerBackground"`
	FooterBackground                *string `json:"footerBackground"`
	SidebarBackground               *string `json:"sidebarBackground"`
	CardBackground                  *string `json:"cardBackground"`
	DialogBorder                    *string `json:"dialogBorder"`
	DialogGlow                      *string `json:"dialogGlow"`
	DialogBackdrop                  *string `json:"dialogBackdrop"`
	CodeBackground                  *string `json:"codeBackground"`
	CodeText                        *string `json:"codeText"`
	ChartRx                         *string `json:"chartRx"`
	ChartTx                         *string `json:"chartTx"`
	ChartNeutral                    *string `json:"chartNeutral"`
	FileBrowserSurface              *string `json:"fileBrowserSurface"`
	FileBrowserChrome               *string `json:"fileBrowserChrome"`
	FileBrowserBreadcrumbBackground *string `json:"fileBrowserBreadcrumbBackground"`
	FileBrowserBreadcrumbText       *string `json:"fileBrowserBreadcrumbText"`
}

type configDockerPayload struct {
	Folders          []string                  `json:"folders"`
	AutoUpdateStacks []string                  `json:"autoUpdateStacks"`
	Proxy            *configDockerProxyPayload `json:"proxy"`
}

type configDockerProxyPayload struct {
	CaddyEnabled *bool   `json:"caddyEnabled"`
	BaseDomain   *string `json:"baseDomain"`
	TLSEmail     *string `json:"tlsEmail"`
}

type configJobSettingsPayload struct {
	ProgressMinIntervalMs     *int `json:"progressMinIntervalMs"`
	NotificationMinIntervalMs *int `json:"notificationMinIntervalMs"`
	ProgressMinBytesMB        *int `json:"progressMinBytesMB"`
	HeavyArchiveConcurrency   *int `json:"heavyArchiveConcurrency"`
	ArchiveCompressionWorkers *int `json:"archiveCompressionWorkers"`
	ArchiveExtractWorkers     *int `json:"archiveExtractWorkers"`
}

type configDismissalsPayload struct {
	UncleanShutdownBootID *string `json:"uncleanShutdownBootId"`
	FailedLoginAlertID    *string `json:"failedLoginAlertId"`
}
