package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

// applyUISettingsUpdate maps a UI replacement payload onto ui. Value rules
// come from internal/userconfig; only request-shape normalization lives here.
func applyUISettingsUpdate(ui *userconfig.UIPreferences, payload *apischema.ConfigUISetPayload) error {
	if ui == nil {
		return errors.New("UI preferences are nil")
	}
	if payload.Theme != nil {
		theme, ok := userconfig.ParseTheme(*payload.Theme)
		if !ok {
			return errors.New("invalid theme value (LIGHT|DARK)")
		}
		ui.Theme = theme
	}
	if payload.PrimaryColor != nil {
		color, ok := userconfig.ParseCSSColor(*payload.PrimaryColor)
		if !ok {
			return errors.New("invalid primaryColor")
		}
		ui.PrimaryColor = color
	}
	if err := applyThemeColorOverrides(ui, payload.ThemeColors); err != nil {
		return err
	}
	if payload.NavigationMode != nil {
		mode, ok := userconfig.ParseNavigationMode(*payload.NavigationMode)
		if !ok {
			return errors.New("invalid navigationMode value (sidebar|dock)")
		}
		ui.NavigationMode = mode
	}
	if payload.DockTileColors != nil {
		mode, ok := userconfig.ParseDockTileColors(*payload.DockTileColors)
		if !ok {
			return errors.New("invalid dockTileColors value (accent|mono|neutral|vibrant)")
		}
		ui.DockTileColors = mode
	}
	if err := applyDockAccentGradientSetting(ui, payload.DockAccentGradient); err != nil {
		return err
	}
	applyOptionalBool(&ui.SidebarCollapsed, payload.SidebarCollapsed)
	applyOptionalStringSlice(&ui.HiddenCards, payload.HiddenCards)
	applyOptionalDockerDashboardSections(ui, payload.DockerDashboardSections)
	applyOptionalHardwareSections(ui, payload.HardwareSections)
	applyViewModes(ui, payload.ViewModes)
	applyLayoutOrders(ui, payload.LayoutOrders)
	return applyTerminalFontSizeSetting(ui, payload.TerminalFontSize)
}

func applyDockAccentGradientSetting(ui *userconfig.UIPreferences, payload *apischema.ConfigDockAccentGradient) error {
	if payload == nil {
		return nil
	}
	gradient := userconfig.DockAccentGradient{
		StartColor: userconfig.CSSColor(trimmedOptionalString(payload.StartColor)),
		EndColor:   userconfig.CSSColor(trimmedOptionalString(payload.EndColor)),
		RangeStart: payload.RangeStart,
		RangeEnd:   payload.RangeEnd,
	}
	if errs := userconfig.ValidateDockAccentGradient(gradient); len(errs) > 0 {
		return fmt.Errorf("invalid dockAccentGradient: %s", strings.Join(errs, "; "))
	}
	ui.DockAccentGradient = &gradient
	return nil
}

func trimmedOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func applyThemeColorOverrides(ui *userconfig.UIPreferences, payload *apischema.ConfigThemeColorsByModePayload) error {
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
		ui.ThemeColors = nil
	} else {
		ui.ThemeColors = &userconfig.ThemeColorsByMode{Light: light, Dark: dark}
	}
	return nil
}

// buildThemeColors maps the API payload fields to theme colour keys; the key
// table, validation and field assignment live in internal/userconfig.
func buildThemeColors(payload *apischema.ConfigThemeColorsPayload, mode string) (*userconfig.ThemeColors, error) {
	if payload == nil {
		return nil, nil
	}
	return userconfig.NewThemeColors(map[string]*string{
		"backgroundDefault":               payload.BackgroundDefault,
		"backgroundPaper":                 payload.BackgroundPaper,
		"headerBackground":                payload.HeaderBackground,
		"footerBackground":                payload.FooterBackground,
		"sidebarBackground":               payload.SidebarBackground,
		"cardBackground":                  payload.CardBackground,
		"dialogBorder":                    payload.DialogBorder,
		"dialogGlow":                      payload.DialogGlow,
		"dialogBackdrop":                  payload.DialogBackdrop,
		"codeBackground":                  payload.CodeBackground,
		"codeText":                        payload.CodeText,
		"chartRx":                         payload.ChartRx,
		"chartTx":                         payload.ChartTx,
		"chartNeutral":                    payload.ChartNeutral,
		"fileBrowserSurface":              payload.FileBrowserSurface,
		"fileBrowserChrome":               payload.FileBrowserChrome,
		"fileBrowserBreadcrumbBackground": payload.FileBrowserBreadcrumbBackground,
		"fileBrowserBreadcrumbText":       payload.FileBrowserBreadcrumbText,
	}, mode)
}

func applyOptionalDockerDashboardSections(ui *userconfig.UIPreferences, sections *apischema.ConfigDockerDashboardSections) {
	if sections != nil {
		ui.DockerDashboardSections = &userconfig.DockerDashboardSections{Overview: sections.Overview, Monitoring: sections.Monitoring, Daemon: sections.Daemon, Resources: sections.Resources}
	}
}

func applyOptionalHardwareSections(ui *userconfig.UIPreferences, sections *apischema.ConfigHardwareSections) {
	if sections != nil {
		ui.HardwareSections = &userconfig.HardwareSections{Overview: sections.Overview, Hardware: sections.Hardware, Sensors: sections.Sensors, SystemInfo: sections.SystemInfo, GPU: sections.GPU, PCIDevices: sections.PCIDevices, MemoryModules: sections.MemoryModules}
	}
}

// applyViewModes replaces the whole map, silently dropping blank keys and
// unknown modes.
func applyViewModes(ui *userconfig.UIPreferences, viewModes map[string]string) {
	if viewModes == nil {
		return
	}
	normalized := make(map[string]string, len(viewModes))
	for key, mode := range viewModes {
		normalizedKey := strings.TrimSpace(key)
		normalizedMode, ok := userconfig.ParseViewMode(mode)
		if normalizedKey == "" || !ok {
			continue
		}
		normalized[normalizedKey] = normalizedMode
	}
	ui.ViewModes = normalized
}

// applyLayoutOrders replaces the whole per-surface order map, dropping surfaces
// whose order is empty: an absent surface already means "natural order", so
// storing the empty case would only grow the config file.
func applyLayoutOrders(ui *userconfig.UIPreferences, layoutOrders map[string][]string) {
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
	ui.LayoutOrders = normalized
}

// applyTerminalFontSizeSetting treats 0 as "keep the current size".
func applyTerminalFontSizeSetting(ui *userconfig.UIPreferences, fontSize *int) error {
	if fontSize == nil || *fontSize == 0 {
		return nil
	}
	if !userconfig.ValidTerminalFontSize(*fontSize) {
		return errors.New("terminalFontSize must be between 10 and 28")
	}
	ui.TerminalFontSize = *fontSize
	return nil
}
