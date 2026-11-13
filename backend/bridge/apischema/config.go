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

type LoggingDebugRequest struct {
	Enabled bool `json:"enabled"`
}

type LoggingDebugState struct {
	Enabled bool `json:"enabled"`
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
	// Defaults is keyed by agent id: what a new chat with that agent starts with.
	Defaults map[string]AssistantChatDefaults `json:"defaults,omitempty"`
}

// AssistantChatDefaults are the agent's own option values for the model, the
// effort and the permission mode. An empty value keeps the agent's own choice.
type AssistantChatDefaults struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
	Mode   string `json:"mode"`
}

type AppSettings struct {
	ChunkSizeMB     *int `json:"chunkSizeMB,omitzero"`
	ShowHiddenFiles bool `json:"showHiddenFiles"`
}

// UIConfig is the effective presentation-only configuration. Backend defaults
// are included so the frontend has one authoritative default source.
type UIConfig struct {
	// CollapsedSections lists the collapsed section names by page; the
	// frontend owns both, and an absent page or section is expanded.
	CollapsedSections  map[string][]string             `json:"collapsedSections"`
	DockAccentGradient ConfigDockAccentGradient        `json:"dockAccentGradient"`
	DockTileColors     DockTileColors                  `json:"dockTileColors"`
	HiddenCards        []string                        `json:"hiddenCards"`
	LayoutOrders       map[string][]string             `json:"layoutOrders"`
	NavigationMode     NavigationMode                  `json:"navigationMode"`
	PrimaryColor       string                          `json:"primaryColor"`
	SidebarCollapsed   bool                            `json:"sidebarCollapsed"`
	TerminalFontSize   int                             `json:"terminalFontSize"`
	Theme              Theme                           `json:"theme"`
	ThemeColors        *ConfigThemeColorsByModePayload `json:"themeColors,omitzero"`
	ViewModes          map[string]TableCardViewMode    `json:"viewModes"`
	ViewModeDefault    TableCardViewMode               `json:"viewModeDefault"`
}

type JobSettings struct {
	ArchiveCompressionWorkers int `json:"archiveCompressionWorkers"`
	ArchiveExtractWorkers     int `json:"archiveExtractWorkers"`
	NotificationMinIntervalMs int `json:"notificationMinIntervalMs"`
	ProgressMinBytesMB        int `json:"progressMinBytesMB"`
	ProgressMinIntervalMs     int `json:"progressMinIntervalMs"`
}

type Dismissals struct {
	FailedLoginAlertID    *string `json:"failedLoginAlertId,omitzero"`
	UncleanShutdownBootID *string `json:"uncleanShutdownBootId,omitzero"`
	// HistorySeenAt is when the user last opened the job history (RFC 3339).
	HistorySeenAt *string `json:"historySeenAt,omitzero"`
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
	// Defaults, when present, replaces every agent's chat defaults.
	Defaults map[string]AssistantChatDefaults `json:"defaults,omitzero"`
}

type ConfigAppSettingsPayload struct {
	ShowHiddenFiles *bool `json:"showHiddenFiles,omitzero"`
	ChunkSizeMB     *int  `json:"chunkSizeMB,omitzero"`
}

// ConfigUISetPayload replaces the user's UI preferences. A missing field
// resets that preference to the backend default.
type ConfigUISetPayload struct {
	Theme              *string                         `json:"theme,omitzero"`
	PrimaryColor       *string                         `json:"primaryColor,omitzero"`
	ThemeColors        *ConfigThemeColorsByModePayload `json:"themeColors,omitzero"`
	SidebarCollapsed   *bool                           `json:"sidebarCollapsed,omitzero"`
	NavigationMode     *string                         `json:"navigationMode,omitzero"`
	DockTileColors     *string                         `json:"dockTileColors,omitzero"`
	DockAccentGradient *ConfigDockAccentGradient       `json:"dockAccentGradient,omitzero"`
	HiddenCards        []string                        `json:"hiddenCards,omitempty"`
	CollapsedSections  map[string][]string             `json:"collapsedSections,omitempty"`
	ViewModes          map[string]string               `json:"viewModes,omitempty"`
	LayoutOrders       map[string][]string             `json:"layoutOrders,omitempty"`
	TerminalFontSize   *int                            `json:"terminalFontSize,omitzero"`
}

type ConfigDockAccentGradient struct {
	StartColor *string `json:"startColor,omitzero"`
	EndColor   *string `json:"endColor,omitzero"`
	RangeStart int     `json:"rangeStart"`
	RangeEnd   int     `json:"rangeEnd"`
}

type ConfigThemeColorsByModePayload struct {
	Light ConfigThemeColorsPayload `json:"light,omitzero"`
	Dark  ConfigThemeColorsPayload `json:"dark,omitzero"`
}

// ThemeColorKey names one theme colour. The keys are userconfig's list, which
// StringEnums publishes as the literal union.
type ThemeColorKey string

// ConfigThemeColorsPayload holds one scheme's colour overrides by key. A key
// that is absent or null is unset.
type ConfigThemeColorsPayload map[ThemeColorKey]*string

type ConfigDockerPayload struct {
	Folders                 []string `json:"folders,omitempty"`
	RequireMountsForFolders *bool    `json:"requireMountsForFolders,omitzero"`
}

type ConfigJobSettingsPayload struct {
	ProgressMinIntervalMs     *int `json:"progressMinIntervalMs,omitzero"`
	NotificationMinIntervalMs *int `json:"notificationMinIntervalMs,omitzero"`
	ProgressMinBytesMB        *int `json:"progressMinBytesMB,omitzero"`
	ArchiveCompressionWorkers *int `json:"archiveCompressionWorkers,omitzero"`
	ArchiveExtractWorkers     *int `json:"archiveExtractWorkers,omitzero"`
}

type ConfigDismissalsPayload struct {
	UncleanShutdownBootID *string `json:"uncleanShutdownBootId,omitzero"`
	FailedLoginAlertID    *string `json:"failedLoginAlertId,omitzero"`
	// HistorySeenAt must be an RFC 3339 time; an empty value clears it.
	HistorySeenAt *string `json:"historySeenAt,omitzero"`
}
