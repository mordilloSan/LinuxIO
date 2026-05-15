package config

import (
	"fmt"
	"strings"

	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func applyAppSettingsUpdate(app *bridgeconfig.AppSettings, payload *configAppSettingsPayload) error {
	if err := applyThemeSetting(app, payload.Theme); err != nil {
		return err
	}
	if err := applyPrimaryColorSetting(app, payload.PrimaryColor); err != nil {
		return err
	}
	if err := applyThemeColorOverrides(app, payload.ThemeColors); err != nil {
		return err
	}
	applyOptionalBool(&app.SidebarCollapsed, payload.SidebarCollapsed)
	applyOptionalBool(&app.ShowHiddenFiles, payload.ShowHiddenFiles)
	applyOptionalStringSlice(&app.DashboardOrder, payload.DashboardOrder)
	applyOptionalStringSlice(&app.HiddenCards, payload.HiddenCards)
	applyOptionalStringSlice(&app.ContainerOrder, payload.ContainerOrder)
	applyOptionalDockerDashboardSections(app, payload.DockerDashboardSections)
	applyOptionalHardwareSections(app, payload.HardwareSections)
	applyViewModes(app, payload.ViewModes)
	return applyChunkSizeSetting(app, payload.ChunkSizeMB)
}

func applyThemeSetting(app *bridgeconfig.AppSettings, theme *string) error {
	if theme == nil {
		return nil
	}
	normalized := strings.ToUpper(strings.TrimSpace(*theme))
	if normalized != string(bridgeconfig.ThemeLight) && normalized != string(bridgeconfig.ThemeDark) {
		return fmt.Errorf("invalid theme value (LIGHT|DARK)")
	}
	app.Theme = bridgeconfig.Theme(normalized)
	return nil
}

func applyPrimaryColorSetting(app *bridgeconfig.AppSettings, primaryColor *string) error {
	if primaryColor == nil {
		return nil
	}
	if !bridgeconfig.IsValidCSSColor(*primaryColor) {
		return fmt.Errorf("invalid primaryColor")
	}
	app.PrimaryColor = bridgeconfig.CSSColor(*primaryColor)
	return nil
}

func applyThemeColorOverrides(app *bridgeconfig.AppSettings, payload *configThemeColorsByModePayload) error {
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

func buildThemeColors(payload *configThemeColorsPayload, modePrefix string) (*bridgeconfig.ThemeColors, error) {
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

func applyOptionalDockerDashboardSections(app *bridgeconfig.AppSettings, sections *bridgeconfig.DockerDashboardSections) {
	if sections != nil {
		app.DockerDashboardSections = sections
	}
}

func applyOptionalHardwareSections(app *bridgeconfig.AppSettings, sections *bridgeconfig.HardwareSections) {
	if sections != nil {
		app.HardwareSections = sections
	}
}

func applyViewModes(app *bridgeconfig.AppSettings, viewModes map[string]string) {
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

func applyChunkSizeSetting(app *bridgeconfig.AppSettings, chunkSize *int) error {
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
