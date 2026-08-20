package config

import (
	"fmt"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func applyAppSettingsUpdate(app *bridgeconfig.PersistedAppSettings, payload *apischema.ConfigAppSettingsPayload) error {
	if err := applyThemeSetting(app, payload.Theme); err != nil {
		return err
	}
	if err := applyPrimaryColorSetting(app, payload.PrimaryColor); err != nil {
		return err
	}
	if err := applyThemeColorOverrides(app, payload.ThemeColors); err != nil {
		return err
	}
	if err := applyNavigationModeSetting(app, payload.NavigationMode); err != nil {
		return err
	}
	if err := applyDockTileColorsSetting(app, payload.DockTileColors); err != nil {
		return err
	}
	if err := applyDockAccentGradientSetting(app, payload.DockAccentGradient); err != nil {
		return err
	}
	applyOptionalBool(&app.SidebarCollapsed, payload.SidebarCollapsed)
	applyOptionalBool(&app.ShowHiddenFiles, payload.ShowHiddenFiles)
	applyOptionalStringSlice(&app.HiddenCards, payload.HiddenCards)
	applyOptionalDockerDashboardSections(app, payload.DockerDashboardSections)
	applyOptionalHardwareSections(app, payload.HardwareSections)
	applyViewModes(app, payload.ViewModes)
	applyLayoutOrders(app, payload.LayoutOrders)
	if err := applyChunkSizeSetting(app, payload.ChunkSizeMB); err != nil {
		return err
	}
	return applyTerminalFontSizeSetting(app, payload.TerminalFontSize)
}

func applyDockAccentGradientSetting(app *bridgeconfig.PersistedAppSettings, payload *apischema.ConfigDockAccentGradient) error {
	if payload == nil {
		return nil
	}
	gradient := bridgeconfig.DockAccentGradient{
		StartColor: bridgeconfig.CSSColor(trimmedOptionalString(payload.StartColor)),
		EndColor:   bridgeconfig.CSSColor(trimmedOptionalString(payload.EndColor)),
		RangeStart: payload.RangeStart,
		RangeEnd:   payload.RangeEnd,
	}
	if errs := bridgeconfig.ValidateDockAccentGradient(gradient); len(errs) > 0 {
		return fmt.Errorf("invalid dockAccentGradient: %s", strings.Join(errs, "; "))
	}
	app.DockAccentGradient = gradient
	return nil
}

func trimmedOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func applyThemeSetting(app *bridgeconfig.PersistedAppSettings, theme *string) error {
	if theme == nil {
		return nil
	}
	normalized := strings.ToUpper(strings.TrimSpace(*theme))
	if normalized != string(bridgeconfig.ThemeLight) && normalized != string(bridgeconfig.ThemeDark) {
		return fmt.Errorf("invalid theme value (LIGHT|DARK)")
	}
	app.Theme = bridgeconfig.PersistedTheme(normalized)
	return nil
}

func applyNavigationModeSetting(app *bridgeconfig.PersistedAppSettings, mode *string) error {
	if mode == nil {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(*mode))
	if normalized != bridgeconfig.NavigationModeSidebar && normalized != bridgeconfig.NavigationModeDock {
		return fmt.Errorf("invalid navigationMode value (sidebar|dock)")
	}
	app.NavigationMode = normalized
	return nil
}

func applyDockTileColorsSetting(app *bridgeconfig.PersistedAppSettings, mode *string) error {
	if mode == nil {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(*mode))
	if !bridgeconfig.IsValidDockTileColors(normalized) {
		return fmt.Errorf("invalid dockTileColors value (accent|mono|neutral|vibrant)")
	}
	app.DockTileColors = normalized
	return nil
}

func applyPrimaryColorSetting(app *bridgeconfig.PersistedAppSettings, primaryColor *string) error {
	if primaryColor == nil {
		return nil
	}
	if !bridgeconfig.IsValidCSSColor(*primaryColor) {
		return fmt.Errorf("invalid primaryColor")
	}
	app.PrimaryColor = bridgeconfig.CSSColor(*primaryColor)
	return nil
}

func applyThemeColorOverrides(app *bridgeconfig.PersistedAppSettings, payload *apischema.ConfigThemeColorsByModePayload) error {
	if payload == nil {
		return nil
	}
	light, err := buildThemeColors(payload.Light, "light")
	if err != nil {
		return err
	}
	dark, err := buildThemeColors(payload.Dark, "dark")
	if err != nil {
		return err
	}
	if light == nil && dark == nil {
		app.ThemeColors = nil
	} else {
		app.ThemeColors = &bridgeconfig.ThemeColorsByMode{Light: light, Dark: dark}
	}
	return nil
}

func buildThemeColors(payload *apischema.ConfigThemeColorsPayload, modePrefix string) (*bridgeconfig.ThemeColors, error) {
	if payload == nil {
		return nil, nil
	}
	colors := &bridgeconfig.ThemeColors{}
	hasAny := false
	fields := []struct {
		src *string
		dst **bridgeconfig.CSSColor
		key string
	}{
		{src: payload.BackgroundDefault, dst: &colors.BackgroundDefault, key: "backgroundDefault"},
		{src: payload.BackgroundPaper, dst: &colors.BackgroundPaper, key: "backgroundPaper"},
		{src: payload.HeaderBackground, dst: &colors.HeaderBackground, key: "headerBackground"},
		{src: payload.FooterBackground, dst: &colors.FooterBackground, key: "footerBackground"},
		{src: payload.SidebarBackground, dst: &colors.SidebarBackground, key: "sidebarBackground"},
		{src: payload.CardBackground, dst: &colors.CardBackground, key: "cardBackground"},
		{src: payload.DialogBorder, dst: &colors.DialogBorder, key: "dialogBorder"},
		{src: payload.DialogGlow, dst: &colors.DialogGlow, key: "dialogGlow"},
		{src: payload.DialogBackdrop, dst: &colors.DialogBackdrop, key: "dialogBackdrop"},
		{src: payload.CodeBackground, dst: &colors.CodeBackground, key: "codeBackground"},
		{src: payload.CodeText, dst: &colors.CodeText, key: "codeText"},
		{src: payload.ChartRx, dst: &colors.ChartRx, key: "chartRx"},
		{src: payload.ChartTx, dst: &colors.ChartTx, key: "chartTx"},
		{src: payload.ChartNeutral, dst: &colors.ChartNeutral, key: "chartNeutral"},
		{src: payload.FileBrowserSurface, dst: &colors.FileBrowserSurface, key: "fileBrowserSurface"},
		{src: payload.FileBrowserChrome, dst: &colors.FileBrowserChrome, key: "fileBrowserChrome"},
		{src: payload.FileBrowserBreadcrumbBackground, dst: &colors.FileBrowserBreadcrumbBackground, key: "fileBrowserBreadcrumbBackground"},
		{src: payload.FileBrowserBreadcrumbText, dst: &colors.FileBrowserBreadcrumbText, key: "fileBrowserBreadcrumbText"},
	}
	for _, field := range fields {
		if field.src == nil {
			continue
		}
		if !bridgeconfig.IsValidCSSColor(*field.src) {
			return nil, fmt.Errorf("invalid themeColors.%s.%s", modePrefix, field.key)
		}
		value := bridgeconfig.CSSColor(*field.src)
		*field.dst = &value
		hasAny = true
	}
	if !hasAny {
		return nil, nil
	}
	return colors, nil
}

func applyOptionalDockerDashboardSections(app *bridgeconfig.PersistedAppSettings, sections *apischema.ConfigDockerDashboardSections) {
	if sections != nil {
		app.DockerDashboardSections = &bridgeconfig.DockerDashboardSections{Overview: sections.Overview, Monitoring: sections.Monitoring, Daemon: sections.Daemon, Resources: sections.Resources}
	}
}

func applyOptionalHardwareSections(app *bridgeconfig.PersistedAppSettings, sections *apischema.ConfigHardwareSections) {
	if sections != nil {
		app.HardwareSections = &bridgeconfig.HardwareSections{Overview: sections.Overview, Hardware: sections.Hardware, Sensors: sections.Sensors, SystemInfo: sections.SystemInfo, GPU: sections.GPU, PCIDevices: sections.PCIDevices, MemoryModules: sections.MemoryModules}
	}
}

func applyViewModes(app *bridgeconfig.PersistedAppSettings, viewModes map[string]string) {
	if viewModes == nil {
		return
	}
	normalized := make(map[string]string, len(viewModes))
	for key, mode := range viewModes {
		normalizedKey := strings.TrimSpace(key)
		normalizedMode := strings.ToLower(strings.TrimSpace(mode))
		if normalizedKey == "" {
			continue
		}
		if normalizedMode != "card" && normalizedMode != "table" {
			continue
		}
		normalized[normalizedKey] = normalizedMode
	}
	app.ViewModes = normalized
}

// applyLayoutOrders replaces the whole per-surface order map, dropping surfaces
// whose order is empty: an absent surface already means "natural order", so
// storing the empty case would only grow the config file.
func applyLayoutOrders(app *bridgeconfig.PersistedAppSettings, layoutOrders map[string][]string) {
	if layoutOrders == nil {
		return
	}
	normalized := make(map[string][]string, len(layoutOrders))
	for surface, order := range layoutOrders {
		normalizedSurface := strings.TrimSpace(surface)
		if normalizedSurface == "" {
			continue
		}
		items := make([]string, 0, len(order))
		for _, item := range order {
			normalizedItem := strings.TrimSpace(item)
			if normalizedItem == "" {
				continue
			}
			items = append(items, normalizedItem)
		}
		if len(items) == 0 {
			continue
		}
		normalized[normalizedSurface] = items
	}
	app.LayoutOrders = normalized
}

func applyChunkSizeSetting(app *bridgeconfig.PersistedAppSettings, chunkSize *int) error {
	if chunkSize == nil {
		return nil
	}
	value := *chunkSize
	if value != 0 && (value < 1 || value > 32) {
		return fmt.Errorf("chunkSizeMB must be 0 (default) or between 1 and 32")
	}
	app.ChunkSizeMB = value
	return nil
}

func applyTerminalFontSizeSetting(app *bridgeconfig.PersistedAppSettings, fontSize *int) error {
	if fontSize == nil {
		return nil
	}
	value := *fontSize
	if value != 0 && (value < 10 || value > 28) {
		return fmt.Errorf("terminalFontSize must be 0 (default) or between 10 and 28")
	}
	app.TerminalFontSize = value
	return nil
}
