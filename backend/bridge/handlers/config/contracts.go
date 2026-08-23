package config

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

// appConfigToAPI is deliberately explicit: core persisted configuration has
// validation-specific types, while apischema owns the public response shape.
func appConfigToAPI(value bridgeconfig.Settings) apischema.AppConfig {
	result := apischema.AppConfig{
		AppSettings: apischema.AppSettings{
			ShowHiddenFiles: value.AppSettings.ShowHiddenFiles,
		},
		Docker: apischema.DockerSettings{
			Folders: absolutePathsToStrings(value.Docker.Folders), RequireMountsForFolders: value.Docker.RequireMountsForFolders,
			Proxy: apischema.DockerProxySettings{CaddyEnabled: value.Docker.Proxy.CaddyEnabled,
				BaseDomain: optionalString(value.Docker.Proxy.BaseDomain), TLSEmail: optionalString(value.Docker.Proxy.TLSEmail)},
		},
		Jobs: apischema.JobSettings{
			ProgressMinIntervalMs: value.Jobs.ProgressMinIntervalMs, NotificationMinIntervalMs: value.Jobs.NotificationMinIntervalMs,
			ProgressMinBytesMB: value.Jobs.ProgressMinBytesMB, HeavyArchiveConcurrency: value.Jobs.HeavyArchiveConcurrency,
			ArchiveCompressionWorkers: value.Jobs.ArchiveCompressionWorkers, ArchiveExtractWorkers: value.Jobs.ArchiveExtractWorkers,
		},
	}
	if value.AppSettings.ChunkSizeMB != 0 {
		result.AppSettings.ChunkSizeMB = &value.AppSettings.ChunkSizeMB
	}
	if value.Dismissals != nil {
		result.Dismissals = &apischema.Dismissals{UncleanShutdownBootID: optionalString(value.Dismissals.UncleanShutdownBootID), FailedLoginAlertID: optionalString(value.Dismissals.FailedLoginAlertID)}
	}
	return result
}

func uiConfigToAPI(value bridgeconfig.UIPreferences) apischema.UIConfig {
	// Store reads normally return a validated complete snapshot, but keeping
	// this boundary total prevents a malformed in-memory value (or a direct
	// unit-test call) from turning an API response into a panic.
	defaults := bridgeconfig.DefaultUIPreferences()
	if value.DockAccentGradient == nil {
		value.DockAccentGradient = defaults.DockAccentGradient
	}
	if value.DockerDashboardSections == nil {
		value.DockerDashboardSections = defaults.DockerDashboardSections
	}
	if value.HardwareSections == nil {
		value.HardwareSections = defaults.HardwareSections
	}
	if value.HiddenCards == nil {
		value.HiddenCards = defaults.HiddenCards
	}
	if value.ViewModes == nil {
		value.ViewModes = defaults.ViewModes
	}
	if value.LayoutOrders == nil {
		value.LayoutOrders = defaults.LayoutOrders
	}
	if value.NavigationMode == "" {
		value.NavigationMode = defaults.NavigationMode
	}
	if value.DockTileColors == "" {
		value.DockTileColors = defaults.DockTileColors
	}
	if value.Theme == "" {
		value.Theme = defaults.Theme
	}
	if value.PrimaryColor == "" {
		value.PrimaryColor = defaults.PrimaryColor
	}
	if value.TerminalFontSize == 0 {
		value.TerminalFontSize = defaults.TerminalFontSize
	}
	result := apischema.UIConfig{
		Theme:                   apischema.Theme(value.Theme),
		PrimaryColor:            value.PrimaryColor.String(),
		ThemeColors:             themeColorsToAPI(value.ThemeColors),
		SidebarCollapsed:        value.SidebarCollapsed,
		NavigationMode:          apischema.NavigationMode(value.NavigationMode),
		DockTileColors:          apischema.DockTileColors(value.DockTileColors),
		HiddenCards:             value.HiddenCards,
		DockerDashboardSections: *dockerDashboardSectionsToAPI(value.DockerDashboardSections),
		HardwareSections:        *hardwareSectionsToAPI(value.HardwareSections),
		LayoutOrders:            value.LayoutOrders,
		ViewModeDefault:         apischema.TableCardViewMode(bridgeconfig.DefaultViewMode),
		TerminalFontSize:        value.TerminalFontSize,
	}
	result.ViewModes = make(map[string]apischema.TableCardViewMode, len(value.ViewModes))
	for key, mode := range value.ViewModes {
		result.ViewModes[key] = apischema.TableCardViewMode(mode)
	}
	result.DockAccentGradient = *dockAccentGradientToAPI(*value.DockAccentGradient)
	return result
}

func dockAccentGradientToAPI(value bridgeconfig.DockAccentGradient) *apischema.ConfigDockAccentGradient {
	return &apischema.ConfigDockAccentGradient{StartColor: optionalString(value.StartColor.String()), EndColor: optionalString(value.EndColor.String()), RangeStart: value.RangeStart, RangeEnd: value.RangeEnd}
}

func absolutePathsToStrings(values []bridgeconfig.AbsolutePath) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}

func dockerDashboardSectionsToAPI(value *bridgeconfig.DockerDashboardSections) *apischema.ConfigDockerDashboardSections {
	if value == nil {
		return nil
	}
	return &apischema.ConfigDockerDashboardSections{Overview: value.Overview, Monitoring: value.Monitoring, Daemon: value.Daemon, Resources: value.Resources}
}

func hardwareSectionsToAPI(value *bridgeconfig.HardwareSections) *apischema.ConfigHardwareSections {
	if value == nil {
		return nil
	}
	return &apischema.ConfigHardwareSections{Overview: value.Overview, Hardware: value.Hardware, Sensors: value.Sensors, SystemInfo: value.SystemInfo, GPU: value.GPU, PCIDevices: value.PCIDevices, MemoryModules: value.MemoryModules}
}

func themeColorsToAPI(value *bridgeconfig.ThemeColorsByMode) *apischema.ConfigThemeColorsByModePayload {
	if value == nil {
		return nil
	}
	return &apischema.ConfigThemeColorsByModePayload{Light: themeColorToAPI(value.Light), Dark: themeColorToAPI(value.Dark)}
}

func themeColorToAPI(value *bridgeconfig.ThemeColors) *apischema.ConfigThemeColorsPayload {
	if value == nil {
		return nil
	}
	return &apischema.ConfigThemeColorsPayload{
		BackgroundDefault: cssColorToString(value.BackgroundDefault), BackgroundPaper: cssColorToString(value.BackgroundPaper), HeaderBackground: cssColorToString(value.HeaderBackground), FooterBackground: cssColorToString(value.FooterBackground), SidebarBackground: cssColorToString(value.SidebarBackground), CardBackground: cssColorToString(value.CardBackground), DialogBorder: cssColorToString(value.DialogBorder), DialogGlow: cssColorToString(value.DialogGlow), DialogBackdrop: cssColorToString(value.DialogBackdrop), CodeBackground: cssColorToString(value.CodeBackground), CodeText: cssColorToString(value.CodeText), ChartRx: cssColorToString(value.ChartRx), ChartTx: cssColorToString(value.ChartTx), ChartNeutral: cssColorToString(value.ChartNeutral), FileBrowserSurface: cssColorToString(value.FileBrowserSurface), FileBrowserChrome: cssColorToString(value.FileBrowserChrome), FileBrowserBreadcrumbBackground: cssColorToString(value.FileBrowserBreadcrumbBackground), FileBrowserBreadcrumbText: cssColorToString(value.FileBrowserBreadcrumbText),
	}
}

func cssColorToString(value *bridgeconfig.CSSColor) *string {
	if value == nil {
		return nil
	}
	result := value.String()
	return &result
}
func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
