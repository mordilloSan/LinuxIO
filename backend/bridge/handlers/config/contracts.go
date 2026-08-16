package config

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

// appConfigToAPI is deliberately explicit: persisted configuration has
// validation-specific types, while apischema owns the public response shape.
func appConfigToAPI(value bridgeconfig.Settings) apischema.AppConfig {
	viewModes := make(map[string]apischema.TableCardViewMode, len(value.AppSettings.ViewModes))
	for key, mode := range value.AppSettings.ViewModes {
		viewModes[key] = apischema.TableCardViewMode(mode)
	}
	result := apischema.AppConfig{
		AppSettings: apischema.AppSettings{
			Theme: apischema.Theme(value.AppSettings.Theme), PrimaryColor: value.AppSettings.PrimaryColor.String(),
			ThemeColors: themeColorsToAPI(value.AppSettings.ThemeColors), SidebarCollapsed: value.AppSettings.SidebarCollapsed,
			NavigationMode:  apischema.NavigationMode(value.AppSettings.NavigationMode),
			DockTileColors:  apischema.DockTileColors(value.AppSettings.DockTileColors),
			ShowHiddenFiles: value.AppSettings.ShowHiddenFiles, HiddenCards: value.AppSettings.HiddenCards,
			DockerDashboardSections: dockerDashboardSectionsToAPI(value.AppSettings.DockerDashboardSections),
			HardwareSections:        hardwareSectionsToAPI(value.AppSettings.HardwareSections), ViewModes: viewModes,
			LayoutOrders: value.AppSettings.LayoutOrders,
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
	if value.AppSettings.TerminalFontSize != 0 {
		result.AppSettings.TerminalFontSize = &value.AppSettings.TerminalFontSize
	}
	if value.Dismissals != nil {
		result.Dismissals = &apischema.Dismissals{UncleanShutdownBootID: optionalString(value.Dismissals.UncleanShutdownBootID), FailedLoginAlertID: optionalString(value.Dismissals.FailedLoginAlertID)}
	}
	return result
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
	return &apischema.ConfigDockerDashboardSections{Overview: value.Overview, Daemon: value.Daemon, Resources: value.Resources}
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
