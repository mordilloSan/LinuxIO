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
	Dismissals  *Dismissals       `json:"dismissals,omitempty"`
	Assistant   AssistantSettings `json:"assistant"`
	Docker      DockerSettings    `json:"docker"`
	Jobs        JobSettings       `json:"jobs"`
	StorageMode ConfigStorageMode `json:"storageMode"`
}

type AssistantSettings struct {
	Agent string `json:"agent"`
}

type AppSettings struct {
	ChunkSizeMB     *int `json:"chunkSizeMB,omitempty"`
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
	ThemeColors             *ConfigThemeColorsByModePayload `json:"themeColors,omitempty"`
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
	FailedLoginAlertID    *string `json:"failedLoginAlertId,omitempty"`
	UncleanShutdownBootID *string `json:"uncleanShutdownBootId,omitempty"`
}

type ConfigSetPayload struct {
	AppSettings *ConfigAppSettingsPayload `json:"appSettings,omitempty"`
	Docker      *ConfigDockerPayload      `json:"docker,omitempty"`
	Jobs        *ConfigJobSettingsPayload `json:"jobs,omitempty"`
	Dismissals  *ConfigDismissalsPayload  `json:"dismissals,omitempty"`
	Assistant   *ConfigAssistantPayload   `json:"assistant,omitempty"`
}

type ConfigAssistantPayload struct {
	Agent *string `json:"agent,omitempty"`
}

type ConfigAppSettingsPayload struct {
	ShowHiddenFiles *bool `json:"showHiddenFiles,omitempty"`
	ChunkSizeMB     *int  `json:"chunkSizeMB,omitempty"`
}

// ConfigUISetPayload replaces the user's UI preferences. A missing field
// resets that preference to the backend default.
type ConfigUISetPayload struct {
	Theme                   *string                         `json:"theme,omitempty"`
	PrimaryColor            *string                         `json:"primaryColor,omitempty"`
	ThemeColors             *ConfigThemeColorsByModePayload `json:"themeColors,omitempty"`
	SidebarCollapsed        *bool                           `json:"sidebarCollapsed,omitempty"`
	NavigationMode          *string                         `json:"navigationMode,omitempty"`
	DockTileColors          *string                         `json:"dockTileColors,omitempty"`
	DockAccentGradient      *ConfigDockAccentGradient       `json:"dockAccentGradient,omitempty"`
	HiddenCards             []string                        `json:"hiddenCards,omitempty"`
	DockerDashboardSections *ConfigDockerDashboardSections  `json:"dockerDashboardSections,omitempty"`
	HardwareSections        *ConfigHardwareSections         `json:"hardwareSections,omitempty"`
	ViewModes               map[string]string               `json:"viewModes,omitempty"`
	LayoutOrders            map[string][]string             `json:"layoutOrders,omitempty"`
	TerminalFontSize        *int                            `json:"terminalFontSize,omitempty"`
}

type ConfigDockAccentGradient struct {
	StartColor *string `json:"startColor,omitempty"`
	EndColor   *string `json:"endColor,omitempty"`
	RangeStart int     `json:"rangeStart"`
	RangeEnd   int     `json:"rangeEnd"`
}

type ConfigThemeColorsByModePayload struct {
	Light *ConfigThemeColorsPayload `json:"light,omitempty"`
	Dark  *ConfigThemeColorsPayload `json:"dark,omitempty"`
}

type ConfigThemeColorsPayload struct {
	BackgroundDefault               *string `json:"backgroundDefault,omitempty"`
	BackgroundPaper                 *string `json:"backgroundPaper,omitempty"`
	HeaderBackground                *string `json:"headerBackground,omitempty"`
	FooterBackground                *string `json:"footerBackground,omitempty"`
	SidebarBackground               *string `json:"sidebarBackground,omitempty"`
	CardBackground                  *string `json:"cardBackground,omitempty"`
	DialogBorder                    *string `json:"dialogBorder,omitempty"`
	DialogGlow                      *string `json:"dialogGlow,omitempty"`
	DialogBackdrop                  *string `json:"dialogBackdrop,omitempty"`
	CodeBackground                  *string `json:"codeBackground,omitempty"`
	CodeText                        *string `json:"codeText,omitempty"`
	ChartRx                         *string `json:"chartRx,omitempty"`
	ChartTx                         *string `json:"chartTx,omitempty"`
	ChartNeutral                    *string `json:"chartNeutral,omitempty"`
	FileBrowserSurface              *string `json:"fileBrowserSurface,omitempty"`
	FileBrowserChrome               *string `json:"fileBrowserChrome,omitempty"`
	FileBrowserBreadcrumbBackground *string `json:"fileBrowserBreadcrumbBackground,omitempty"`
	FileBrowserBreadcrumbText       *string `json:"fileBrowserBreadcrumbText,omitempty"`
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
	RequireMountsForFolders *bool    `json:"requireMountsForFolders,omitempty"`
}

type ConfigJobSettingsPayload struct {
	ProgressMinIntervalMs     *int `json:"progressMinIntervalMs,omitempty"`
	NotificationMinIntervalMs *int `json:"notificationMinIntervalMs,omitempty"`
	ProgressMinBytesMB        *int `json:"progressMinBytesMB,omitempty"`
	HeavyArchiveConcurrency   *int `json:"heavyArchiveConcurrency,omitempty"`
	ArchiveCompressionWorkers *int `json:"archiveCompressionWorkers,omitempty"`
	ArchiveExtractWorkers     *int `json:"archiveExtractWorkers,omitempty"`
}

type ConfigDismissalsPayload struct {
	UncleanShutdownBootID *string `json:"uncleanShutdownBootId,omitempty"`
	FailedLoginAlertID    *string `json:"failedLoginAlertId,omitempty"`
}
