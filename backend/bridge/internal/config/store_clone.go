package config

import "maps"

func cloneSettings(in *Settings) *Settings {
	if in == nil {
		return nil
	}
	out := *in
	out.AppSettings = cloneAppSettings(in.AppSettings)
	out.Docker = cloneDocker(in.Docker)
	if in.Dismissals != nil {
		dismissals := *in.Dismissals
		out.Dismissals = &dismissals
	}
	return &out
}

func cloneAppSettings(in PersistedAppSettings) PersistedAppSettings {
	out := in
	out.ThemeColors = cloneThemeColorsByMode(in.ThemeColors)
	out.DashboardOrder = cloneStringSlice(in.DashboardOrder)
	out.HiddenCards = cloneStringSlice(in.HiddenCards)
	out.ContainerOrder = cloneStringSlice(in.ContainerOrder)
	if in.DockerDashboardSections != nil {
		sections := *in.DockerDashboardSections
		out.DockerDashboardSections = &sections
	}
	if in.HardwareSections != nil {
		sections := *in.HardwareSections
		out.HardwareSections = &sections
	}
	out.ViewModes = cloneStringMap(in.ViewModes)
	return out
}

func cloneDocker(in Docker) Docker {
	out := in
	out.Folders = cloneAbsolutePathSlice(in.Folders)
	out.AutoUpdateStacks = cloneStringSlice(in.AutoUpdateStacks)
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

func cloneStringSlice(in []string) []string {
	if in == nil {
		return nil
	}
	return append([]string(nil), in...)
}

func cloneAbsolutePathSlice(in []AbsolutePath) []AbsolutePath {
	if in == nil {
		return nil
	}
	return append([]AbsolutePath(nil), in...)
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
