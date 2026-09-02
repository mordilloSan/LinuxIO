package config

import (
	"maps"
	"slices"
)

func cloneSettings(in *Settings) *Settings {
	if in == nil {
		return nil
	}
	out := *in
	out.AppSettings = in.AppSettings
	out.Docker = cloneDocker(in.Docker)
	if in.Dismissals != nil {
		dismissals := *in.Dismissals
		out.Dismissals = &dismissals
	}
	return &out
}

func cloneUIPreferences(in *UIPreferences) *UIPreferences {
	if in == nil {
		return nil
	}
	out := *in
	out.HiddenCards = slices.Clone(in.HiddenCards)
	out.ViewModes = maps.Clone(in.ViewModes)
	out.LayoutOrders = cloneLayoutOrders(in.LayoutOrders)
	out.ThemeColors = cloneThemeColorsByMode(in.ThemeColors)
	if in.DockAccentGradient != nil {
		gradient := *in.DockAccentGradient
		out.DockAccentGradient = &gradient
	}
	if in.DockerDashboardSections != nil {
		sections := *in.DockerDashboardSections
		out.DockerDashboardSections = &sections
	}
	if in.HardwareSections != nil {
		sections := *in.HardwareSections
		out.HardwareSections = &sections
	}
	return &out
}

func cloneLayoutOrders(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for surface, order := range in {
		out[surface] = slices.Clone(order)
	}
	return out
}

func cloneDocker(in Docker) Docker {
	out := in
	out.Folders = slices.Clone(in.Folders)
	return out
}

func cloneThemeColorsByMode(in *ThemeColorsByMode) *ThemeColorsByMode {
	if in == nil {
		return nil
	}
	return &ThemeColorsByMode{
		Light: cloneThemeColors(in.Light),
		Dark:  cloneThemeColors(in.Dark),
	}
}

func cloneThemeColors(in *ThemeColors) *ThemeColors {
	if in == nil {
		return nil
	}
	out := *in
	out.BackgroundDefault = cloneCSSColor(in.BackgroundDefault)
	out.BackgroundPaper = cloneCSSColor(in.BackgroundPaper)
	out.HeaderBackground = cloneCSSColor(in.HeaderBackground)
	out.FooterBackground = cloneCSSColor(in.FooterBackground)
	out.SidebarBackground = cloneCSSColor(in.SidebarBackground)
	out.CardBackground = cloneCSSColor(in.CardBackground)
	out.DialogBorder = cloneCSSColor(in.DialogBorder)
	out.DialogGlow = cloneCSSColor(in.DialogGlow)
	out.DialogBackdrop = cloneCSSColor(in.DialogBackdrop)
	out.CodeBackground = cloneCSSColor(in.CodeBackground)
	out.CodeText = cloneCSSColor(in.CodeText)
	out.ChartRx = cloneCSSColor(in.ChartRx)
	out.ChartTx = cloneCSSColor(in.ChartTx)
	out.ChartNeutral = cloneCSSColor(in.ChartNeutral)
	out.FileBrowserSurface = cloneCSSColor(in.FileBrowserSurface)
	out.FileBrowserChrome = cloneCSSColor(in.FileBrowserChrome)
	out.FileBrowserBreadcrumbBackground = cloneCSSColor(in.FileBrowserBreadcrumbBackground)
	out.FileBrowserBreadcrumbText = cloneCSSColor(in.FileBrowserBreadcrumbText)
	return &out
}

func cloneCSSColor(in *CSSColor) *CSSColor {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
