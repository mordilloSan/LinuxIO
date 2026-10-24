package apischema

type ConfigStorageMode string
type TableCardViewMode string
type Theme string
type NavigationMode string
type DockTileColors string

type ConfigSetResult struct {
	Message string `json:"message"`
	Path    string `json:"path"`
}

type AppConfig struct {
	AppSettings AppSettings       `json:"appSettings"`
	Dismissals  *Dismissals       `json:"dismissals,omitzero"`
	Assistant   AssistantSettings `json:"assistant"`
	Docker      DockerSettings    `json:"docker"`
	Jobs        JobSettings       `json:"jobs"`
	StorageMode ConfigStorageMode `json:"storageMode"`
}

type AssistantSettings struct {
	Agent string `json:"agent"`
}

type AppSettings struct {
	ChunkSizeMB     *int `json:"chunkSizeMB,omitzero"`
	ShowHiddenFiles bool `json:"showHiddenFiles"`
}

// UIConfig is the effective presentation-only configuration. Backend defaults
// are included so the frontend has one authoritative default source.
type UIConfig struct {
	DockAccentGradient      ConfigDockAccentGradient        `json:"dockAccentGradient"`
	DockTileColors          DockTileColors                  `json:"dockTileColors"`
	DockerDashboardSections ConfigDockerDashboardSections   `json:"dockerDashboardSections"`
	HardwareSections        ConfigHardwareSections          `json:"hardwareSections"`
	HiddenCards             []string                        `json:"hiddenCards"`
	LayoutOrders            map[string][]string             `json:"layoutOrders"`
	NavigationMode          NavigationMode                  `json:"navigationMode"`
	PrimaryColor            string                          `json:"primaryColor"`
	SidebarCollapsed        bool                            `json:"sidebarCollapsed"`
	TerminalFontSize        int                             `json:"terminalFontSize"`
	Theme                   Theme                           `json:"theme"`
	ThemeColors             *ConfigThemeColorsByModePayload `json:"themeColors,omitzero"`
	ViewModes               map[string]TableCardViewMode    `json:"viewModes"`
	ViewModeDefault         TableCardViewMode               `json:"viewModeDefault"`
}

type JobSettings struct {
	ArchiveCompressionWorkers int `json:"archiveCompressionWorkers"`
	ArchiveExtractWorkers     int `json:"archiveExtractWorkers"`
	HeavyArchiveConcurrency   int `json:"heavyArchiveConcurrency"`
	NotificationMinIntervalMs int `json:"notificationMinIntervalMs"`
	ProgressMinBytesMB        int `json:"progressMinBytesMB"`
	ProgressMinIntervalMs     int `json:"progressMinIntervalMs"`
}

type Dismissals struct {
	FailedLoginAlertID    *string `json:"failedLoginAlertId,omitzero"`
	UncleanShutdownBootID *string `json:"uncleanShutdownBootId,omitzero"`
}

type ConfigSetPayload struct {
	AppSettings *ConfigAppSettingsPayload `json:"appSettings,omitzero"`
	Docker      *ConfigDockerPayload      `json:"docker,omitzero"`
	Jobs        *ConfigJobSettingsPayload `json:"jobs,omitzero"`
	Dismissals  *ConfigDismissalsPayload  `json:"dismissals,omitzero"`
	Assistant   *ConfigAssistantPayload   `json:"assistant,omitzero"`
}

type ConfigAssistantPayload struct {
	Agent *string `json:"agent,omitzero"`
}

type ConfigAppSettingsPayload struct {
	ShowHiddenFiles *bool `json:"showHiddenFiles,omitzero"`
	ChunkSizeMB     *int  `json:"chunkSizeMB,omitzero"`
}

// ConfigUISetPayload replaces the user's UI preferences. A missing field
// resets that preference to the backend default.
type ConfigUISetPayload struct {
	Theme                   *string                         `json:"theme,omitzero"`
	PrimaryColor            *string                         `json:"primaryColor,omitzero"`
	ThemeColors             *ConfigThemeColorsByModePayload `json:"themeColors,omitzero"`
	SidebarCollapsed        *bool                           `json:"sidebarCollapsed,omitzero"`
	NavigationMode          *string                         `json:"navigationMode,omitzero"`
	DockTileColors          *string                         `json:"dockTileColors,omitzero"`
	DockAccentGradient      *ConfigDockAccentGradient       `json:"dockAccentGradient,omitzero"`
	HiddenCards             []string                        `json:"hiddenCards,omitempty"`
	DockerDashboardSections *ConfigDockerDashboardSections  `json:"dockerDashboardSections,omitzero"`
	HardwareSections        *ConfigHardwareSections         `json:"hardwareSections,omitzero"`
	ViewModes               map[string]string               `json:"viewModes,omitempty"`
	LayoutOrders            map[string][]string             `json:"layoutOrders,omitempty"`
	TerminalFontSize        *int                            `json:"terminalFontSize,omitzero"`
}

type ConfigDockAccentGradient struct {
	StartColor *string `json:"startColor,omitzero"`
	EndColor   *string `json:"endColor,omitzero"`
	RangeStart int     `json:"rangeStart"`
	RangeEnd   int     `json:"rangeEnd"`
}

type ConfigThemeColorsByModePayload struct {
	Light *ConfigThemeColorsPayload `json:"light,omitzero"`
	Dark  *ConfigThemeColorsPayload `json:"dark,omitzero"`
}

type ConfigThemeColorsPayload struct {
	BackgroundDefault               *string `json:"backgroundDefault,omitzero"`
	BackgroundPaper                 *string `json:"backgroundPaper,omitzero"`
	HeaderBackground                *string `json:"headerBackground,omitzero"`
	FooterBackground                *string `json:"footerBackground,omitzero"`
	SidebarBackground               *string `json:"sidebarBackground,omitzero"`
	CardBackground                  *string `json:"cardBackground,omitzero"`
	DialogBorder                    *string `json:"dialogBorder,omitzero"`
	DialogGlow                      *string `json:"dialogGlow,omitzero"`
	DialogBackdrop                  *string `json:"dialogBackdrop,omitzero"`
	CodeBackground                  *string `json:"codeBackground,omitzero"`
	CodeText                        *string `json:"codeText,omitzero"`
	ChartRx                         *string `json:"chartRx,omitzero"`
	ChartTx                         *string `json:"chartTx,omitzero"`
	ChartNeutral                    *string `json:"chartNeutral,omitzero"`
	FileBrowserSurface              *string `json:"fileBrowserSurface,omitzero"`
	FileBrowserChrome               *string `json:"fileBrowserChrome,omitzero"`
	FileBrowserBreadcrumbBackground *string `json:"fileBrowserBreadcrumbBackground,omitzero"`
	FileBrowserBreadcrumbText       *string `json:"fileBrowserBreadcrumbText,omitzero"`
}

type ConfigDockerDashboardSections struct {
	Overview   bool `json:"overview"`
	Monitoring bool `json:"monitoring"`
	Daemon     bool `json:"daemon"`
	Resources  bool `json:"resources"`
}

type ConfigHardwareSections struct {
	Overview      bool `json:"overview"`
	Hardware      bool `json:"hardware"`
	Sensors       bool `json:"sensors"`
	SystemInfo    bool `json:"systemInfo"`
	GPU           bool `json:"gpu"`
	PCIDevices    bool `json:"pciDevices"`
	MemoryModules bool `json:"memoryModules"`
}

type ConfigDockerPayload struct {
	Folders                 []string `json:"folders,omitempty"`
	RequireMountsForFolders *bool    `json:"requireMountsForFolders,omitzero"`
}

type ConfigJobSettingsPayload struct {
	ProgressMinIntervalMs     *int `json:"progressMinIntervalMs,omitzero"`
	NotificationMinIntervalMs *int `json:"notificationMinIntervalMs,omitzero"`
	ProgressMinBytesMB        *int `json:"progressMinBytesMB,omitzero"`
	HeavyArchiveConcurrency   *int `json:"heavyArchiveConcurrency,omitzero"`
	ArchiveCompressionWorkers *int `json:"archiveCompressionWorkers,omitzero"`
	ArchiveExtractWorkers     *int `json:"archiveExtractWorkers,omitzero"`
}

type ConfigDismissalsPayload struct {
	UncleanShutdownBootID *string `json:"uncleanShutdownBootId,omitzero"`
	FailedLoginAlertID    *string `json:"failedLoginAlertId,omitzero"`
}
