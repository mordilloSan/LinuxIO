package userconfig

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
	out.Assistant.Defaults = maps.Clone(in.Assistant.Defaults)
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
	out.CollapsedSections = cloneNameLists(in.CollapsedSections)
	out.LayoutOrders = cloneNameLists(in.LayoutOrders)
	out.ThemeColors = cloneThemeColorsByMode(in.ThemeColors)
	if in.DockAccentGradient != nil {
		gradient := *in.DockAccentGradient
		out.DockAccentGradient = &gradient
	}
	return &out
}

func cloneNameLists(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for key, names := range in {
		out[key] = slices.Clone(names)
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
		Light: maps.Clone(in.Light),
		Dark:  maps.Clone(in.Dark),
	}
}
